package server

import (
	"bufio"
	"encoding/json"
	"fmt"
	"net"
	"strings"
	"testing"
	"time"

	"github.com/catallo/misterclaw/pkg/session"
)

func TestTCPAdmissionByteBudgetExactRejectionsAndRelease(t *testing.T) {
	name, head := "byte-tcp", "printf ready; read -r gate"
	payload := strings.Repeat(" ", 256*1024-1) + ":"
	limits := session.DefaultLimits()
	// Two large commands fit by bytes; the job-count limit would allow 64.
	limits.SessionBytes = len(head) + len("/bin/sh") + len(name) + 2*(len(payload)+len("/bin/sh")+len(name))
	mgr, err := session.NewManagerWithLimits("/bin/sh", limits)
	if err != nil {
		t.Fatal(err)
	}
	srv := New(mgr)
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()
	defer mgr.Close(name)
	go func() {
		c, e := ln.Accept()
		if e == nil {
			srv.handleConn(c)
		}
	}()
	conn, err := net.DialTimeout("tcp", ln.Addr().String(), 2*time.Second)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	conn.SetDeadline(time.Now().Add(15 * time.Second))
	enc := json.NewEncoder(conn)
	send := func(v interface{}) {
		t.Helper()
		if err := enc.Encode(v); err != nil {
			t.Fatal(err)
		}
	}
	events := make(chan map[string]interface{}, 512)
	go func() {
		defer close(events)
		scan := bufio.NewScanner(conn)
		for scan.Scan() {
			var e map[string]interface{}
			if json.Unmarshal(scan.Bytes(), &e) == nil {
				events <- e
			}
		}
	}()
	read := func() map[string]interface{} {
		t.Helper()
		select {
		case e, ok := <-events:
			if !ok {
				t.Fatal("connection ended")
			}
			return e
		case <-time.After(10 * time.Second):
			t.Fatal("protocol stalled")
			return nil
		}
	}
	send(map[string]interface{}{"id": "head", "session": name, "cmd": head, "pty": false})
	for {
		e := read()
		if data, _ := e["data"].(string); strings.Contains(data, "ready") {
			break
		}
	}
	for i := 0; i < 256; i++ {
		send(map[string]interface{}{"id": fmt.Sprintf("q%d", i), "session": name, "cmd": payload, "pty": false})
	}
	send(map[string]interface{}{"list": true})
	seen := map[string]bool{}
	rejected := 0
	for {
		e := read()
		if e["list"] == true {
			break
		}
		if e["done"] == true {
			id := e["id"].(string)
			if seen[id] {
				t.Fatalf("duplicate %s", id)
			}
			seen[id] = true
			if e["exit_code"] != float64(session.ExitRejected) || e["error"] == nil {
				t.Fatalf("rejection=%v", e)
			}
			rejected++
		}
	}
	if pending := mgr.Get(name).Info().Pending; pending != 2 || rejected != 254 {
		t.Fatalf("pending=%d rejected=%d", pending, rejected)
	}
	send(map[string]interface{}{"drain": true, "session": name})
	drained, active, cancelled := false, false, 0
	for !drained || !active || cancelled != 2 {
		e := read()
		if e["drain"] == true {
			drained = e["success"] == true && e["cancelled"] == float64(2)
		}
		if e["done"] == true {
			id := e["id"].(string)
			if seen[id] {
				t.Fatalf("duplicate %s", id)
			}
			seen[id] = true
			if id == "head" {
				active = e["exit_code"] == float64(-1)
			} else {
				if e["exit_code"] != float64(session.ExitCancelled) {
					t.Fatalf("cancel=%v", e)
				}
				cancelled++
			}
		}
	}
	if len(seen) != 257 {
		t.Fatalf("completions=%d", len(seen))
	}
	// Credits become reusable without changing the session or owner.
	send(map[string]interface{}{"id": "reuse", "session": name, "cmd": "exit 7", "pty": false})
	for {
		e := read()
		if e["id"] == "reuse" && e["done"] == true {
			if e["exit_code"] != float64(7) {
				t.Fatal(e)
			}
			break
		}
	}
}
