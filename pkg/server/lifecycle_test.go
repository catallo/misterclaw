package server

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/catallo/misterclaw/pkg/session"
)

// eofBarrierConn delays only the old connection's EOF, not the new command.
// It reproduces the exact reconnect interleaving without timing sleeps/hooks
// in production code.
type eofBarrierConn struct {
	net.Conn
	eof     chan struct{}
	release chan struct{}
	once    sync.Once
}

func (c *eofBarrierConn) Read(p []byte) (int, error) {
	n, err := c.Conn.Read(p)
	if err == io.EOF {
		c.once.Do(func() { close(c.eof) })
		<-c.release
	}
	return n, err
}

type protocolPeer struct {
	conn   net.Conn
	events chan map[string]interface{}
	ended  chan struct{}
}

func pipePeer(t *testing.T, srv *Server, gate *eofBarrierConn) *protocolPeer {
	t.Helper()
	client, server := net.Pipe()
	var conn net.Conn = server
	if gate != nil {
		gate.Conn = server
		conn = gate
	}
	p := &protocolPeer{client, make(chan map[string]interface{}, 256), make(chan struct{})}
	go func() { defer close(p.ended); srv.handleConn(conn) }()
	go func() {
		defer close(p.events)
		scanner := bufio.NewScanner(client)
		for scanner.Scan() {
			var event map[string]interface{}
			if json.Unmarshal(scanner.Bytes(), &event) == nil {
				p.events <- event
			}
		}
	}()
	t.Cleanup(func() {
		client.Close()
		if gate != nil {
			select {
			case <-gate.release:
			default:
				close(gate.release)
			}
		}
		awaitSignal(t, p.ended, "connection cleanup")
	})
	return p
}

func awaitSignal(t *testing.T, ch <-chan struct{}, what string) {
	t.Helper()
	select {
	case <-ch:
	case <-time.After(5 * time.Second):
		t.Fatalf("timeout waiting for %s", what)
	}
}

func (p *protocolPeer) send(t *testing.T, req map[string]interface{}) {
	t.Helper()
	data, err := json.Marshal(req)
	if err != nil {
		t.Fatal(err)
	}
	p.conn.SetWriteDeadline(time.Now().Add(5 * time.Second))
	if _, err = p.conn.Write(append(data, '\n')); err != nil {
		t.Fatal(err)
	}
}

func (p *protocolPeer) await(t *testing.T, match func(map[string]interface{}) bool) map[string]interface{} {
	t.Helper()
	timer := time.NewTimer(5 * time.Second)
	defer timer.Stop()
	for {
		select {
		case event, ok := <-p.events:
			if !ok {
				t.Fatal("connection ended before expected response")
			}
			if match(event) {
				return event
			}
		case <-timer.C:
			t.Fatal("timeout waiting for protocol event")
		}
	}
}

func (p *protocolPeer) cmd(t *testing.T, id, command string) {
	p.send(t, map[string]interface{}{"id": id, "cmd": command, "session": "reconnect", "pty": false})
}

func (p *protocolPeer) output(t *testing.T, text string) {
	p.await(t, func(e map[string]interface{}) bool {
		data, _ := e["data"].(string)
		return strings.Contains(data, text)
	})
}

func (p *protocolPeer) done(t *testing.T, id string, want int) {
	e := p.await(t, func(e map[string]interface{}) bool { return e["id"] == id && e["done"] == true })
	if e["exit_code"] != float64(want) {
		t.Fatalf("%s completion = %v, want %d", id, e, want)
	}
}

func TestReconnectOldDisconnectPreservesNewActive(t *testing.T) {
	mgr := session.NewManager("/bin/sh")
	srv := New(mgr)
	t.Cleanup(func() { mgr.Close("reconnect") })
	gate := &eofBarrierConn{eof: make(chan struct{}), release: make(chan struct{})}
	old := pipePeer(t, srv, gate)
	old.cmd(t, "old", "printf old")
	old.done(t, "old", 0)
	old.conn.Close()
	awaitSignal(t, gate.eof, "old EOF barrier")

	fresh := pipePeer(t, srv, nil)
	fresh.cmd(t, "new", "printf ready; read -r line; printf survived")
	fresh.output(t, "ready") // new process is definitely running before old cleanup
	close(gate.release)
	awaitSignal(t, old.ended, "old disconnect cleanup")
	fresh.send(t, map[string]interface{}{"session": "reconnect", "input": "release\n"})
	fresh.done(t, "new", 0)
}

func TestReconnectOldDisconnectPreservesNewQueued(t *testing.T) {
	mgr := session.NewManager("/bin/sh")
	srv := New(mgr)
	t.Cleanup(func() { mgr.Close("reconnect") })
	gate := &eofBarrierConn{eof: make(chan struct{}), release: make(chan struct{})}
	old := pipePeer(t, srv, gate)
	old.cmd(t, "old", "printf ready; read -r line")
	old.output(t, "ready")
	old.conn.Close()
	awaitSignal(t, gate.eof, "old EOF barrier")

	fresh := pipePeer(t, srv, nil)
	fresh.cmd(t, "queued", "printf survived")
	fresh.send(t, map[string]interface{}{"list": true})
	fresh.await(t, func(e map[string]interface{}) bool { return e["list"] == true })
	if n := mgr.Get("reconnect").Info().Pending; n != 1 {
		t.Fatalf("Pending = %d, want 1", n)
	}
	close(gate.release)
	awaitSignal(t, old.ended, "old disconnect cleanup")
	fresh.done(t, "queued", 0)
}

func TestExplicitCloseCompletesActiveAndQueuedWithoutManagerDeadlock(t *testing.T) {
	mgr := session.NewManager("/bin/sh")
	srv := New(mgr)
	peer := pipePeer(t, srv, nil)
	peer.cmd(t, "active", "printf ready; read -r line")
	peer.output(t, "ready")
	peer.cmd(t, "queued", "printf must-not-run")
	peer.send(t, map[string]interface{}{"close": true, "session": "reconnect"})
	codes := map[string]float64{}
	closed := false
	for len(codes) != 2 || !closed {
		event := peer.await(t, func(map[string]interface{}) bool { return true })
		if event["done"] == true {
			codes[event["id"].(string)] = event["exit_code"].(float64)
		}
		if event["close"] == true {
			closed = event["success"] == true
		}
	}
	if codes["active"] != -1 || codes["queued"] != -2 {
		t.Fatalf("close completions: %v", codes)
	}
	if mgr.Get("reconnect") != nil {
		t.Fatal("closed session remains registered")
	}
	peer.cmd(t, "recreated", "exit 7")
	peer.done(t, "recreated", 7)
	mgr.Close("reconnect")
}

func TestDisconnectWithMoreThanChannelCapacityQueued(t *testing.T) {
	mgr := session.NewManager("/bin/sh")
	srv := New(mgr)
	t.Cleanup(func() { mgr.Close("reconnect") })
	old := pipePeer(t, srv, nil)
	old.cmd(t, "active", "printf ready; read -r line")
	old.output(t, "ready")
	for i := 0; i < 100; i++ {
		old.cmd(t, fmt.Sprint(i), "exit 19")
	}
	old.send(t, map[string]interface{}{"list": true})
	old.await(t, func(e map[string]interface{}) bool { return e["list"] == true })
	if got := mgr.Get("reconnect").Info().Pending; got != 100 {
		t.Fatalf("Pending = %d, want 100", got)
	}
	old.conn.Close()
	awaitSignal(t, old.ended, "backlogged disconnect cleanup")
	fresh := pipePeer(t, srv, nil)
	fresh.cmd(t, "new", "exit 23")
	fresh.done(t, "new", 23)
	if got := mgr.Get("reconnect").Info().Pending; got != 0 {
		t.Fatalf("abandoned Pending = %d", got)
	}
}

// Real TCP, deliberately identical session, request ID and agent. A read
// barrier ensures the known independent output-before-done bug does not
// masquerade as a lifecycle regression; no reconnect-delay is inserted.
func TestRapidTCPReconnectIdenticalSession(t *testing.T) {
	addr, srv := startTestServer(t)
	t.Cleanup(func() { srv.manager.Close("rapid-reused") })
	for i := 0; i < 1000; i++ {
		conn, err := net.DialTimeout("tcp", addr, 2*time.Second)
		if err != nil {
			t.Fatal(err)
		}
		conn.SetDeadline(time.Now().Add(5 * time.Second))
		scanner := bufio.NewScanner(conn)
		token := fmt.Sprintf("ready-%04d", i)
		data, _ := json.Marshal(map[string]interface{}{"session": "rapid-reused", "id": "same-id", "agent": "same-agent", "owner": "ignored-client-owner", "pty": false, "cmd": "printf '" + token + "'; read -r gate; exit 7"})
		if _, err := conn.Write(append(data, '\n')); err != nil {
			conn.Close()
			t.Fatal(err)
		}
		output := ""
		for !strings.Contains(output, token) {
			if !scanner.Scan() {
				conn.Close()
				t.Fatalf("iteration %d output: %v", i, scanner.Err())
			}
			var e map[string]interface{}
			if err := json.Unmarshal(scanner.Bytes(), &e); err != nil {
				conn.Close()
				t.Fatal(err)
			}
			if e["done"] == true {
				conn.Close()
				t.Fatalf("iteration %d unexpectedly completed at barrier: %v", i, e)
			}
			if text, ok := e["data"].(string); ok {
				output += text
			}
		}
		fmt.Fprintln(conn, `{"session":"rapid-reused","input":"release\n"}`)
		for {
			if !scanner.Scan() {
				conn.Close()
				t.Fatalf("iteration %d completion: %v", i, scanner.Err())
			}
			var e map[string]interface{}
			if err := json.Unmarshal(scanner.Bytes(), &e); err != nil {
				conn.Close()
				t.Fatal(err)
			}
			if e["done"] == true {
				if e["exit_code"] != float64(7) {
					conn.Close()
					t.Fatalf("iteration %d: %v", i, e)
				}
				break
			}
		}
		conn.Close() // next dial races this connection's server-side cleanup
	}
	if list := srv.manager.List(); len(list) != 1 {
		t.Fatalf("identical reconnects created %d sessions", len(list))
	}
}
