package session

// Independent reviewer probes. Only present in the review's disposable copy.
import (
	"fmt"
	"runtime"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func TestReviewReentrantCloseFromCancelledCallback(t *testing.T) {
	m := NewManager("/bin/sh")
	o := NewOwner()
	defer o.Close()
	active := submitLifecycle(m, o, "review-reentrant", "printf ready; read -r gate", false)
	waitReady(t, active)
	var cancelled atomic.Int32
	closed := make(chan bool, 1)
	m.ExecuteOwned(o, "review-reentrant", "exit 1", false, "", func([]byte) {}, func(code int) {
		if code == ExitCancelled {
			cancelled.Add(1)
		}
		closed <- m.Close("review-reentrant")
	})
	drained := make(chan int, 1)
	go func() { drained <- m.Drain("review-reentrant") }()
	select {
	case ok := <-closed:
		if !ok {
			t.Fatal("reentrant close failed")
		}
	case <-time.After(3 * time.Second):
		t.Fatal("Drain -> queued callback -> Close deadlocked")
	}
	select {
	case n := <-drained:
		if n != 1 {
			t.Fatalf("Drain=%d", n)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("Drain never returned")
	}
	if jobCode(t, active) == 0 {
		t.Fatal("active survived")
	}
	if cancelled.Load() != 1 {
		t.Fatalf("cancelled callbacks=%d", cancelled.Load())
	}
	assertOwnerEmpty(t, o)
}

func TestReviewCompletionReentrantDrainAndOwnerClose(t *testing.T) {
	m := NewManager("/bin/sh")
	o := NewOwner()
	defer m.Close("review-completion")
	first := submitLifecycle(m, o, "review-completion", "printf ready; read -r gate", false)
	waitReady(t, first)
	callbacks := make(chan int, 8)
	// FIFO: second's completion drains third/fourth and reenters Owner.Close.
	m.ExecuteOwned(o, "review-completion", "exit 7", false, "", func([]byte) {}, func(code int) {
		if code != 7 {
			callbacks <- -100
			return
		}
		n := m.Drain("review-completion")
		o.Close()
		callbacks <- n
	})
	var cancelled atomic.Int32
	for i := 0; i < 2; i++ {
		m.ExecuteOwned(o, "review-completion", "exit 19", false, "", func([]byte) {}, func(code int) {
			if code == ExitCancelled {
				cancelled.Add(1)
			}
			_ = m.List()
		})
	}
	if err := m.WriteInput("review-completion", []byte("release\n")); err != nil {
		t.Fatal(err)
	}
	assertCode(t, first, 0)
	select {
	case n := <-callbacks:
		if n != 2 {
			t.Fatalf("drain=%d", n)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("completion reentrancy stuck")
	}
	if cancelled.Load() != 2 {
		t.Fatalf("cancelled=%d", cancelled.Load())
	}
	assertOwnerEmpty(t, o)
}

func TestReviewOutputCallbackCloseLiveness(t *testing.T) {
	m := NewManager("/bin/sh")
	returned := make(chan struct{})
	callbackStarted := make(chan struct{})
	callbackRelease := make(chan struct{})
	closeStarted := make(chan struct{})
	completed := make(chan int, 1)
	// Large, finite output: os/exec must still be copying after the first callback.
	// Exit cleanup uses a release latch, so the review doesn't leave stuck workers.
	m.Execute("review-output-close", "head -c 262144 /dev/zero", false, "", func([]byte) {
		select {
		case <-callbackStarted:
			return
		default:
			close(callbackStarted)
		}
		go func() { close(closeStarted); m.Close("review-output-close"); close(returned) }()
		// Hold the output callback until its reentrant Close returns, or test cleanup.
		select {
		case <-returned:
		case <-callbackRelease:
		}
	}, func(code int) { completed <- code })
	select {
	case <-callbackStarted:
	case <-time.After(3 * time.Second):
		t.Fatal("no output")
	}
	<-closeStarted
	var stalled bool
	select {
	case <-returned:
	case <-time.After(500 * time.Millisecond):
		stalled = true
	}
	if stalled {
		buf := make([]byte, 32768)
		n := runtime.Stack(buf, true)
		t.Logf("blocked Close/output wait stacks:\n%s", buf[:n])
	}
	close(callbackRelease)
	select {
	case <-returned:
	case <-time.After(8 * time.Second):
		t.Fatal("cleanup Close did not return")
	}
	select {
	case <-completed:
	case <-time.After(3 * time.Second):
		t.Fatal("cleanup missing completion")
	}
	if stalled {
		t.Fatal("Close waits for worker; worker Wait waits for the output callback that is waiting for Close (not reentrant-safe)")
	}
}

func TestReviewCloseConcurrentSubmissionCompletionCounts(t *testing.T) {
	m := NewManager("/bin/sh")
	for iteration := 0; iteration < 40; iteration++ {
		name := fmt.Sprintf("review-close-%d", iteration)
		active := submitLifecycle(m, nil, name, "printf ready; read -r gate", false)
		waitReady(t, active)
		start := make(chan struct{})
		done := make(chan struct{}, 9)
		var submitted, callbacks atomic.Int32
		for j := 0; j < 8; j++ {
			go func() {
				<-start
				submitted.Add(1)
				m.Execute(name, "exit 0", false, "", func([]byte) {}, func(int) { callbacks.Add(1) })
				done <- struct{}{}
			}()
		}
		go func() { <-start; m.Close(name); done <- struct{}{} }()
		close(start)
		for j := 0; j < 9; j++ {
			select {
			case <-done:
			case <-time.After(3 * time.Second):
				t.Fatal("close/submission stuck")
			}
		}
		// Some submissions can legitimately create a new session after old Close.
		m.Drain(name)
		m.Close(name)
		if jobCode(t, active) == 0 {
			t.Fatal("active survived close")
		}
		deadline := time.Now().Add(3 * time.Second)
		for callbacks.Load() < submitted.Load() && time.Now().Before(deadline) {
			runtime.Gosched()
		}
		if callbacks.Load() != submitted.Load() {
			t.Fatalf("submitted=%d completions=%d", submitted.Load(), callbacks.Load())
		}
	}
}

func TestReviewFIFOResourceGrowth(t *testing.T) {
	m := NewManager("/bin/sh")
	o := NewOwner()
	defer m.Close("review-queue")
	defer o.Close()
	active := submitLifecycle(m, o, "review-queue", "printf ready; read -r gate", false)
	waitReady(t, active)
	runtime.GC()
	var before, after runtime.MemStats
	runtime.ReadMemStats(&before)
	var completions atomic.Int32
	const queued = 256
	const payload = 256 * 1024
	for i := 0; i < queued; i++ {
		line := fmt.Sprintf("# %04d ", i) + strings.Repeat("x", payload)
		m.ExecuteOwned(o, "review-queue", line, false, "", func([]byte) {}, func(int) { completions.Add(1) })
	}
	runtime.GC()
	runtime.ReadMemStats(&after)
	s := m.Get("review-queue")
	s.mu.Lock()
	n := len(s.queue)
	var bytes int
	for _, j := range s.queue {
		bytes += len(j.line)
	}
	s.mu.Unlock()
	growth := int64(after.HeapAlloc) - int64(before.HeapAlloc)
	t.Logf("single owner/session: pending=%d retained command bytes=%d signed heap delta=%d bytes (%.2f MiB)", n, bytes, growth, float64(growth)/(1024*1024))
	// Follow-up adaptation: the original unbounded expectation is now a bound.
	// The verbatim reviewer file remains in .followup-evidence/.
	if n >= queued || n > m.Limits().SessionJobs-1 || bytes > m.Limits().SessionBytes {
		t.Fatalf("admission did not bound pending=%d bytes=%d", n, bytes)
	}
	o.Close()
	if jobCode(t, active) == 0 {
		t.Fatal("active survived")
	}
	if completions.Load() != queued {
		t.Fatalf("completions=%d", completions.Load())
	}
	assertOwnerEmpty(t, o)
}
