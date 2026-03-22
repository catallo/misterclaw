package session

import (
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func TestSequentialExecution(t *testing.T) {
	m := NewManager("/bin/sh")

	// Run two commands in the same session — they must execute sequentially
	var order []int
	var mu sync.Mutex
	done := make(chan struct{}, 2)

	m.Execute("seq-test", "echo first", false, "", func(data []byte) {}, func(code int) {
		mu.Lock()
		order = append(order, 1)
		mu.Unlock()
		done <- struct{}{}
	})

	m.Execute("seq-test", "echo second", false, "", func(data []byte) {}, func(code int) {
		mu.Lock()
		order = append(order, 2)
		mu.Unlock()
		done <- struct{}{}
	})

	for i := 0; i < 2; i++ {
		select {
		case <-done:
		case <-time.After(5 * time.Second):
			t.Fatal("timeout")
		}
	}

	mu.Lock()
	defer mu.Unlock()
	if len(order) != 2 || order[0] != 1 || order[1] != 2 {
		t.Errorf("execution order = %v, want [1, 2]", order)
	}
}

func TestParallelSessions(t *testing.T) {
	m := NewManager("/bin/sh")

	var count int64
	done := make(chan struct{}, 2)

	// Two different sessions should run in parallel
	m.Execute("session-a", "echo a", false, "", func(data []byte) {}, func(code int) {
		atomic.AddInt64(&count, 1)
		done <- struct{}{}
	})

	m.Execute("session-b", "echo b", false, "", func(data []byte) {}, func(code int) {
		atomic.AddInt64(&count, 1)
		done <- struct{}{}
	})

	for i := 0; i < 2; i++ {
		select {
		case <-done:
		case <-time.After(5 * time.Second):
			t.Fatal("timeout")
		}
	}

	if atomic.LoadInt64(&count) != 2 {
		t.Errorf("count = %d, want 2", count)
	}
}

func TestList(t *testing.T) {
	m := NewManager("/bin/sh")
	m.GetOrCreate("alpha")
	m.GetOrCreate("beta")

	list := m.List()
	if len(list) != 2 {
		t.Errorf("len(list) = %d, want 2", len(list))
	}
}

func TestKillAndClose(t *testing.T) {
	m := NewManager("/bin/sh")
	m.GetOrCreate("to-close")

	if !m.Kill("to-close") {
		// Kill on idle session is fine
	}
	if !m.Close("to-close") {
		t.Error("Close returned false for existing session")
	}
	if m.Close("to-close") {
		t.Error("Close returned true for already-closed session")
	}
	if len(m.List()) != 0 {
		t.Error("session still in list after close")
	}
}
