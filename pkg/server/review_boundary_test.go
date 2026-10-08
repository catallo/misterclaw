package server

// Independent reviewer probes; never written into the original source/evidence.
import (
	"bufio"
	"encoding/json"
	"fmt"
	"net"
	"runtime"
	"strings"
	"testing"
	"time"
)

func TestReviewSingleTCPConnectionFIFOResourceGrowth(t *testing.T) {
	addr, srv := startTestServer(t)
	defer srv.manager.Close("review-network-queue")
	conn, err := net.DialTimeout("tcp", addr, 2*time.Second)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	conn.SetDeadline(time.Now().Add(10 * time.Second))
	encoder := json.NewEncoder(conn)
	scanner := bufio.NewScanner(conn)
	send := func(v interface{}) {
		t.Helper()
		if err := encoder.Encode(v); err != nil {
			t.Fatal(err)
		}
	}
	send(map[string]interface{}{"id": "head", "session": "review-network-queue", "pty": false, "cmd": "printf ready; read -r gate"})
	for {
		if !scanner.Scan() {
			t.Fatal("no head output")
		}
		var e map[string]interface{}
		if err := json.Unmarshal(scanner.Bytes(), &e); err != nil {
			t.Fatal(err)
		}
		if data, _ := e["data"].(string); strings.Contains(data, "ready") {
			break
		}
	}
	runtime.GC()
	var before, after runtime.MemStats
	runtime.ReadMemStats(&before)
	const count = 256
	const payload = 256 * 1024
	for i := 0; i < count; i++ {
		send(map[string]interface{}{"id": fmt.Sprintf("q%d", i), "session": "review-network-queue", "pty": false, "cmd": fmt.Sprintf("# %04d ", i) + strings.Repeat("x", payload)})
	}
	send(map[string]interface{}{"list": true})
	for {
		if !scanner.Scan() {
			t.Fatal("no list barrier")
		}
		var e map[string]interface{}
		if err := json.Unmarshal(scanner.Bytes(), &e); err != nil {
			t.Fatal(err)
		}
		if e["list"] == true {
			break
		}
	}
	runtime.GC()
	runtime.ReadMemStats(&after)
	pending := srv.manager.Get("review-network-queue").Info().Pending
	growth := int64(after.HeapAlloc) - int64(before.HeapAlloc)
	t.Logf("one real TCP connection + one session, all JSON lines below 1MiB scanner limit: pending=%d, signed heap delta=%d bytes (%.2f MiB)", pending, growth, float64(growth)/(1024*1024))
	// Follow-up adaptation: the reviewer expectation of all jobs being queued
	// is inverted. Its unchanged original remains in .followup-evidence/.
	if pending >= count || pending > srv.manager.Limits().SessionJobs-1 {
		t.Fatalf("admission did not bound pending=%d", pending)
	}
	// Close the TCP connection, so cleanup's synchronous sends cannot block.
	conn.Close()
	// The server closes connections before Owner.Close; wait for pending removal.
	deadline := time.Now().Add(5 * time.Second)
	for srv.manager.Get("review-network-queue").Info().Pending != 0 && time.Now().Before(deadline) {
		time.Sleep(time.Millisecond)
	}
	if pending := srv.manager.Get("review-network-queue").Info().Pending; pending != 0 {
		t.Fatalf("post-disconnect pending=%d", pending)
	}
}
