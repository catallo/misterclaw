package server

import (
	"bufio"
	"encoding/json"
	"fmt"
	"net"
	"testing"
	"time"

	"github.com/catallo/clawexec-mister-fpga/pkg/session"
)

func startTestServer(t *testing.T) (string, *Server) {
	t.Helper()
	mgr := session.NewManager("/bin/sh")
	srv := New(mgr)

	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	srv.listener = ln
	addr := ln.Addr().String()

	go func() {
		for {
			conn, err := ln.Accept()
			if err != nil {
				return
			}
			srv.mu.Lock()
			srv.clients[conn] = struct{}{}
			srv.mu.Unlock()
			go srv.handleConn(conn)
		}
	}()

	t.Cleanup(func() { srv.Close() })
	return addr, srv
}

func dial(t *testing.T, addr string) (net.Conn, *bufio.Scanner) {
	t.Helper()
	conn, err := net.DialTimeout("tcp", addr, 2*time.Second)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	scanner := bufio.NewScanner(conn)
	scanner.Buffer(make([]byte, 64*1024), 1024*1024)
	t.Cleanup(func() { conn.Close() })
	return conn, scanner
}

func sendAndRead(t *testing.T, conn net.Conn, scanner *bufio.Scanner, req string) map[string]interface{} {
	t.Helper()
	fmt.Fprintf(conn, "%s\n", req)
	if !scanner.Scan() {
		t.Fatalf("no response (err: %v)", scanner.Err())
	}
	var resp map[string]interface{}
	if err := json.Unmarshal(scanner.Bytes(), &resp); err != nil {
		t.Fatalf("unmarshal response: %v (raw: %s)", err, scanner.Text())
	}
	return resp
}

func TestListCommand(t *testing.T) {
	addr, _ := startTestServer(t)
	conn, scanner := dial(t, addr)

	resp := sendAndRead(t, conn, scanner, `{"list": true}`)
	if resp["list"] != true {
		t.Errorf("expected list=true, got %v", resp["list"])
	}
	if _, ok := resp["sessions"]; !ok {
		t.Error("missing sessions field")
	}
	if _, ok := resp["total"]; !ok {
		t.Error("missing total field")
	}
}

func TestExecuteCommand(t *testing.T) {
	addr, _ := startTestServer(t)
	conn, scanner := dial(t, addr)

	// Send a simple echo command without PTY (pipe mode for test reliability)
	fmt.Fprintf(conn, `{"id":"test1","cmd":"echo hello","session":"test","pty":false}`+"\n")

	// Collect responses until we get the done message
	var gotOutput bool
	var gotDone bool
	for i := 0; i < 10; i++ {
		if !scanner.Scan() {
			break
		}
		var resp map[string]interface{}
		json.Unmarshal(scanner.Bytes(), &resp)

		if resp["stream"] == "stdout" {
			gotOutput = true
		}
		if resp["done"] == true {
			gotDone = true
			// Verify done message has sessions array
			if _, ok := resp["sessions"]; !ok {
				t.Error("done message missing sessions")
			}
			if _, ok := resp["exit_code"]; !ok {
				t.Error("done message missing exit_code")
			}
			break
		}
	}

	if !gotOutput {
		t.Error("never received stream output")
	}
	if !gotDone {
		t.Error("never received done message")
	}
}

func TestKillCommand(t *testing.T) {
	addr, _ := startTestServer(t)
	conn, scanner := dial(t, addr)

	resp := sendAndRead(t, conn, scanner, `{"kill": true, "session": "nonexistent"}`)
	if resp["kill"] != true {
		t.Errorf("expected kill=true")
	}
	if resp["session"] != "nonexistent" {
		t.Errorf("expected session=nonexistent")
	}
}

func TestCloseCommand(t *testing.T) {
	addr, _ := startTestServer(t)
	conn, scanner := dial(t, addr)

	resp := sendAndRead(t, conn, scanner, `{"close": true, "session": "nonexistent"}`)
	if resp["close"] != true {
		t.Errorf("expected close=true")
	}
}

func TestInvalidJSON(t *testing.T) {
	addr, _ := startTestServer(t)
	conn, scanner := dial(t, addr)

	resp := sendAndRead(t, conn, scanner, `not json`)
	if _, ok := resp["error"]; !ok {
		t.Error("expected error response for invalid JSON")
	}
}

func TestUnrecognizedCommand(t *testing.T) {
	addr, _ := startTestServer(t)
	conn, scanner := dial(t, addr)

	resp := sendAndRead(t, conn, scanner, `{"foo": "bar"}`)
	if _, ok := resp["error"]; !ok {
		t.Error("expected error response for unrecognized command")
	}
}
