package session

import (
	"errors"
	"fmt"
	"sync/atomic"
	"testing"
	"time"
)

func TestExternalCancellationCompletionsReserveNameUntilReturn(t *testing.T) {
	for _, mode := range []string{"drain", "owner-end"} {
		t.Run(mode, func(t *testing.T) {
			m := NewManager("/bin/sh")
			defer reviewCloseAll(t, m)
			o, foreign := NewOwner(), NewOwner()
			defer o.Close()
			defer foreign.Close()
			head := submitLifecycle(m, o, "reserved-external", "printf ready; read -r gate", false)
			waitReady(t, head)
			s := m.Get("reserved-external")
			entered, release, returned := make(chan struct{}), make(chan struct{}), make(chan struct{})
			cancelReturned := make(chan struct{})
			defer func() {
				select {
				case <-release:
				default:
					close(release)
				}
			}()
			var calls atomic.Int32
			m.Submit(o, s.Name, "exit 19", false, "", func([]byte) { t.Error("cancelled queue ran") }, func(r Result) {
				calls.Add(1)
				if r.ExitCode != ExitCancelled {
					t.Errorf("result=%+v", r)
				}
				if !m.Close(s.Name) {
					t.Error("external cancellation callback Close rejected")
				}
				close(entered)
				<-release
				close(returned)
			})
			go func() {
				defer close(cancelReturned)
				if mode == "drain" {
					if n := m.Drain(s.Name); n != 1 {
						t.Errorf("Drain=%d", n)
					}
				} else {
					o.Close()
				}
			}()
			reviewAwait(t, entered, "external callback did not enter")
			if jobCode(t, head) == 0 {
				t.Fatal("head survived cancellation")
			}
			// Head completion has returned independently. The single remaining
			// accepted-job lifetime is precisely the callback held outside worker.
			s.mu.Lock()
			pending := s.uncompleted
			s.mu.Unlock()
			// The helper's head channel can be received just before its callback
			// returns; both 1 (held callback only) and 2 are legitimate here.
			if pending < 1 || pending > 2 {
				t.Fatalf("uncompleted=%d, held callback must remain reserved", pending)
			}
			assertBudgetEmpty(t, m)
			assertOwnerEmpty(t, o)
			for i := 0; i < 20; i++ {
				var got int
				err := m.Submit(foreign, s.Name, "exit 23", false, "", func([]byte) { t.Error("same-name process bypassed held callback") }, func(r Result) {
					got++
					if r.ExitCode != ExitRejected || !errors.Is(r.Err, ErrSessionClosing) {
						t.Errorf("closing result=%+v", r)
					}
				})
				if !errors.Is(err, ErrSessionClosing) || got != 1 {
					t.Fatalf("err=%v calls=%d", err, got)
				}
			}
			select {
			case <-s.Done():
				t.Fatal("Done before external completion returned")
			default:
			}
			if m.Get(s.Name) != s || s.Info().Status != string(StatusClosing) {
				t.Fatal("closing reservation disappeared")
			}
			// Independent names and freed credits remain available.
			assertCode(t, submitLifecycle(m, foreign, "parallel", "exit 7", false), 7)
			close(release)
			reviewAwait(t, returned, "callback failed to return")
			reviewAwait(t, cancelReturned, "cancellation API failed to return")
			waitClosed(t, s)
			if calls.Load() != 1 || m.Get(s.Name) != nil {
				t.Fatalf("calls=%d name=%v", calls.Load(), m.Get(s.Name))
			}
			assertCode(t, submitLifecycle(m, foreign, s.Name, "exit 23", false), 23)
			assertBudgetEmpty(t, m)
			assertOwnerEmpty(t, foreign)
		})
	}
}

func TestMultipleConcurrentExternalCancelCallbacksAllJoinAtDone(t *testing.T) {
	m := NewManager("/bin/sh")
	defer reviewCloseAll(t, m)
	a, b := NewOwner(), NewOwner()
	defer a.Close()
	defer b.Close()
	head := submitLifecycle(m, nil, "multi-external", "printf ready; read -r gate", false)
	waitReady(t, head)
	s := m.Get("multi-external")
	enterA, enterB := make(chan struct{}), make(chan struct{})
	releaseA, releaseB := make(chan struct{}), make(chan struct{})
	returnA, returnB := make(chan struct{}), make(chan struct{})
	defer func() {
		select {
		case <-releaseA:
		default:
			close(releaseA)
		}
		select {
		case <-releaseB:
		default:
			close(releaseB)
		}
	}()
	var calls atomic.Int32
	for i, item := range []struct {
		o                *Owner
		entered, release chan struct{}
	}{{a, enterA, releaseA}, {b, enterB, releaseB}} {
		item := item
		m.Submit(item.o, s.Name, fmt.Sprintf("exit %d", 19+i), false, "", func([]byte) { t.Error("cancelled queue ran") }, func(r Result) {
			calls.Add(1)
			if r.ExitCode != ExitCancelled {
				t.Errorf("result=%+v", r)
			}
			close(item.entered)
			<-item.release
		})
	}
	go func() { a.Close(); close(returnA) }()
	go func() { b.Close(); close(returnB) }()
	reviewAwait(t, enterA, "first callback missing")
	reviewAwait(t, enterB, "second callback missing")
	if !m.Close(s.Name) {
		t.Fatal("Close refused")
	}
	if jobCode(t, head) == 0 {
		t.Fatal("head survived")
	}
	close(releaseA)
	reviewAwait(t, returnA, "first external callback missing return")
	select {
	case <-s.Done():
		t.Fatal("Done waited for only one external callback")
	default:
	}
	s.mu.Lock()
	pending := s.uncompleted
	s.mu.Unlock()
	if pending < 1 || pending > 2 {
		t.Fatalf("remaining=%d", pending)
	}
	close(releaseB)
	reviewAwait(t, returnB, "second callback missing return")
	waitClosed(t, s)
	if calls.Load() != 2 {
		t.Fatalf("calls=%d", calls.Load())
	}
	assertBudgetEmpty(t, m)
	assertOwnerEmpty(t, a)
	assertOwnerEmpty(t, b)
}

func TestAdmissionReservesCompletionBeforeQueueExtractionAndWorkerStart(t *testing.T) {
	for i := 0; i < 50; i++ {
		m := NewManager("/bin/sh")
		o := NewOwner()
		entered, release := make(chan struct{}), make(chan struct{})
		callbackDone := make(chan struct{})
		var code atomic.Int32
		err := m.Submit(o, "new-publication", "read -r gate", false, "", func([]byte) {}, func(r Result) {
			code.Store(int32(r.ExitCode))
			m.Close("new-publication")
			close(entered)
			<-release
			close(callbackDone)
		})
		if err != nil {
			t.Fatal(err)
		}
		s := m.Get("new-publication")
		ended := make(chan struct{})
		go func() { o.Close(); close(ended) }()
		reviewAwait(t, entered, "new job cancellation never entered callback")
		select {
		case <-s.Done():
			t.Fatal("new transaction did not reserve completion lifetime")
		default:
		}
		close(release)
		reviewAwait(t, ended, "Owner.Close stuck")
		reviewAwait(t, callbackDone, "callback stuck")
		waitClosed(t, s)
		if code.Load() == 0 {
			t.Fatal("cancelled barrier exited normally")
		}
		assertBudgetEmpty(t, m)
		assertOwnerEmpty(t, o)
	}
}

// A rejected request never became this session's job. Its synchronous callback
// may close and observe a deliberately reserved empty session without creating
// a completion token or a new self-join cycle.
func TestUnadmittedRejectionCallbackDoesNotReserveSessionLifetime(t *testing.T) {
	l := DefaultLimits()
	l.SessionBytes = 1
	m, _ := NewManagerWithLimits("/bin/sh", l)
	s := m.GetOrCreate("explicit-empty")
	finished := make(chan struct{})
	go func() {
		m.Submit(NewOwner(), s.Name, "exit 7", false, "", func([]byte) {}, func(r Result) {
			if r.ExitCode != ExitRejected {
				t.Errorf("result=%+v", r)
			}
			m.Close(s.Name)
			waitClosed(t, s)
		})
		close(finished)
	}()
	select {
	case <-finished:
	case <-time.After(3 * time.Second):
		t.Fatal("unadmitted callback self-joined")
	}
	assertBudgetEmpty(t, m)
}
