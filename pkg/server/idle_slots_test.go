package server

import (
	"encoding/json"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/catallo/misterclaw/pkg/session"
)

func TestTCPOperatorCloseRecoversIdleWorkerCapacity(t *testing.T) {
	l := session.DefaultLimits()
	l.Sessions = 2
	mgr, _ := session.NewManagerWithLimits("/bin/sh", l)
	addr, _ := startTestServerWithManager(t, mgr)
	defer func() {
		for _, i := range mgr.List() {
			mgr.Close(i.Name)
		}
	}()
	conn, scan := dial(t, addr)
	conn.SetDeadline(time.Now().Add(5 * time.Second))
	command := func(id, name string) map[string]interface{} {
		t.Helper()
		fmt.Fprintf(conn, "{\"id\":%q,\"session\":%q,\"cmd\":\"exit 7\",\"pty\":false}\n", id, name)
		for scan.Scan() {
			var e map[string]interface{}
			if err := json.Unmarshal(scan.Bytes(), &e); err != nil {
				t.Fatal(err)
			}
			if e["id"] == id && e["done"] == true {
				return e
			}
		}
		t.Fatalf("missing completion %s", id)
		return nil
	}
	for _, name := range []string{"slot-a", "slot-b"} {
		if e := command(name, name); e["exit_code"] != float64(7) {
			t.Fatal(e)
		}
	}
	e := command("rejected", "slot-c")
	if e["exit_code"] != float64(session.ExitRejected) || !strings.Contains(fmt.Sprint(e["error"]), "named-session") {
		t.Fatalf("worker-limit rejection=%v", e)
	}
	old := mgr.Get("slot-a")
	e = sendAndRead(t, conn, scan, `{"close":true,"session":"slot-a"}`)
	if e["success"] != true || e["closing"] != true {
		t.Fatalf("close request ack=%v", e)
	}
	awaitSignal(t, old.Done(), "operator close completion")
	if e = command("recovered", "slot-c"); e["exit_code"] != float64(7) {
		t.Fatalf("slot recovery=%v", e)
	}
}
