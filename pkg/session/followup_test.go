package session

import (
	"strings"
	"sync"
	"testing"
	"time"
)

// This fixture uses only the existing API, so it visibly fails on d949e67.
// The active shell is held at an input barrier while payloads fill its FIFO.
func TestRegressionBoundedAdmission(t *testing.T) {
	m := NewManager("/bin/sh")
	o := NewOwner()
	defer o.Close()
	defer m.Close("bounded")
	active := submitLifecycle(m, o, "bounded", "printf ready; read -r line", false)
	waitReady(t, active)
	var mu sync.Mutex
	rejected := 0
	for i := 0; i < 256; i++ {
		payload := strings.Clone(strings.Repeat(" ", 256*1024-1) + ":")
		m.ExecuteOwned(o, "bounded", payload, false, "", func([]byte) {}, func(code int) {
			mu.Lock()
			if code == -3 {
				rejected++
			}
			mu.Unlock()
		})
	}
	mu.Lock()
	n := rejected
	mu.Unlock()
	pending := m.Get("bounded").Info().Pending
	m.budget.mu.Lock()
	retained := m.budget.total
	m.budget.mu.Unlock()
	t.Logf("bounded exact-256KiB commands: pending=%d rejected=%d outstanding=%+v", pending, n, retained)
	if n == 0 {
		t.Fatalf("no admission rejection: queued=%d, command payloads=%d bytes", pending, 256*256*1024)
	}
	if pending+n != 256 || retained.bytes > m.Limits().SessionBytes || retained.jobs > m.Limits().SessionJobs {
		t.Fatalf("incorrect admission accounting pending=%d rejected=%d usage=%+v", pending, n, retained)
	}
}

// Pipe Wait cannot finish a large blocked output while the callback waits for
// its own worker. The small deadline reports the cycle; on failure a rescue
// callback channel makes the test process exit cleanly instead of hanging.
func TestRegressionOutputCallbackCloseReentrant(t *testing.T) {
	m := NewManager("/bin/sh")
	s := m.GetOrCreate("output-close")
	entered, returned, done := make(chan struct{}), make(chan bool, 1), make(chan int, 1)
	rescue := make(chan struct{})
	var once sync.Once
	m.Execute("output-close", "while :; do printf 'xxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxx'; done", false, "", func([]byte) {
		once.Do(func() {
			close(entered)
			result := make(chan bool, 1)
			go func() { result <- m.Close("output-close") }()
			select {
			case ok := <-result:
				returned <- ok
			case <-rescue:
			}
		})
	}, func(code int) { done <- code })
	defer close(rescue)
	select {
	case <-entered:
	case <-time.After(3 * time.Second):
		t.Fatal("output callback not entered")
	}
	select {
	case ok := <-returned:
		if !ok {
			t.Fatal("Close did not accept request")
		}
	case <-time.After(500 * time.Millisecond):
		t.Fatal("OutputCallback -> Close -> worker -> PipeWait -> OutputCallback cycle")
	}
	select {
	case <-done:
	case <-time.After(3 * time.Second):
		t.Fatal("completion did not follow callback close")
	}
	select {
	case <-s.done:
	case <-time.After(3 * time.Second):
		t.Fatal("worker did not stop")
	}
}
