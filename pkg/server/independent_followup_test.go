package server

// Independent real-TCP availability regression, only in the disposable copy.
import (
	"bufio"
	"encoding/json"
	"github.com/catallo/misterclaw/pkg/session"
	"net"
	"strings"
	"testing"
	"time"
)

func TestIndependentTCPRejectedNamesReserveSlotsAndOperatorRecovery(t *testing.T) {
	limits := session.DefaultLimits()
	limits.OwnerJobs = 1
	limits.Sessions = 3
	mgr, err := session.NewManagerWithLimits("/bin/sh", limits)
	if err != nil {
		t.Fatal(err)
	}
	addr, _ := startTestServerWithManager(t, mgr)
	defer func() {
		for _, info := range mgr.List() {
			s := mgr.Get(info.Name)
			mgr.Close(info.Name)
			if s != nil {
				select {
				case <-s.Done():
				case <-time.After(5 * time.Second):
					t.Error("cleanup timed out")
				}
			}
		}
	}()
	dial := func() (net.Conn, *json.Encoder, *bufio.Scanner) {
		t.Helper()
		c, err := net.DialTimeout("tcp", addr, 2*time.Second)
		if err != nil {
			t.Fatal(err)
		}
		c.SetDeadline(time.Now().Add(10 * time.Second))
		return c, json.NewEncoder(c), bufio.NewScanner(c)
	}
	send := func(enc *json.Encoder, value interface{}) {
		t.Helper()
		if err := enc.Encode(value); err != nil {
			t.Fatal(err)
		}
	}
	read := func(scan *bufio.Scanner) map[string]interface{} {
		t.Helper()
		if !scan.Scan() {
			t.Fatalf("protocol ended: %v", scan.Err())
		}
		var event map[string]interface{}
		if err := json.Unmarshal(scan.Bytes(), &event); err != nil {
			t.Fatal(err)
		}
		return event
	}
	old, enc, scan := dial()
	defer old.Close()
	send(enc, map[string]interface{}{"id": "head", "session": "held", "pty": false, "cmd": "printf ready; read -r gate"})
	for {
		e := read(scan)
		if text, _ := e["data"].(string); strings.Contains(text, "ready") {
			break
		}
	}
	for _, name := range []string{"never-admitted-a", "never-admitted-b"} {
		send(enc, map[string]interface{}{"id": name, "session": name, "pty": false, "cmd": "exit 7"})
		e := read(scan)
		if e["id"] != name || e["done"] != true || e["exit_code"] != float64(-3) || !strings.Contains(e["error"].(string), "owner outstanding") {
			t.Fatalf("unexpected rejection=%v", e)
		}
	}
	// Follow-up expectation: rejected-only names do not consume slots.
	if len(mgr.List()) != 1 || mgr.Get("never-admitted-a") != nil || mgr.Get("never-admitted-b") != nil {
		t.Fatalf("rejected names retained; names=%d", len(mgr.List()))
	}
	old.Close()
	fresh, fe, fs := dial()
	defer fresh.Close()
	// Existing-name reuse succeeds after prior owner cleanup, so no active process
	// or prior owner's admission reservation explains the subsequent rejection.
	send(fe, map[string]interface{}{"id": "reuse", "session": "held", "pty": false, "cmd": "exit 7"})
	for {
		e := read(fs)
		if e["id"] == "reuse" && e["done"] == true {
			if e["exit_code"] != float64(7) {
				t.Fatal(e)
			}
			break
		}
	}
	send(fe, map[string]interface{}{"id": "fresh-name", "session": "legitimate", "pty": false, "cmd": "exit 23"})
	e := read(fs)
	t.Logf("foreign client after old disconnect, previous commands rejected before admission: slots=%d new-name response=%v", len(mgr.List()), e)
	if e["id"] != "fresh-name" || e["done"] != true || e["exit_code"] != float64(23) || e["error"] != nil {
		t.Fatalf("foreign new name still excluded=%v", e)
	}
	// OperatorClose remains valid for actually used idle names. No cleanup
	// of rejected-only names was needed to admit the foreign connection.
	used := mgr.Get("held")
	send(fe, map[string]interface{}{"close": true, "session": "held"})
	reply := read(fs)
	if reply["close"] != true || reply["success"] != true || reply["closing"] != true {
		t.Fatalf("operator close=%v", reply)
	}
	select {
	case <-used.Done():
	case <-time.After(3 * time.Second):
		t.Fatal("operator Close did not complete")
	}
	if mgr.Get("held") != nil {
		t.Fatal("operator Close did not release used slot")
	}
	send(fe, map[string]interface{}{"id": "recovered", "session": "operator-new", "pty": false, "cmd": "exit 23"})
	for {
		e := read(fs)
		if e["id"] == "recovered" && e["done"] == true {
			if e["exit_code"] != float64(23) {
				t.Fatal(e)
			}
			break
		}
	}
	t.Log("foreign new-name admission needs no rejected-slot cleanup; OperatorClose/Done still releases actually used idle names")
}
