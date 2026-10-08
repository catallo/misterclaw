package session

// Additional independent review fixtures; only in this disposable copy.
import (
	"errors"
	"runtime"
	"sync/atomic"
	"testing"
	"time"
)

func reviewAwait(t *testing.T, ch <-chan struct{}, what string) {
	t.Helper()
	select {
	case <-ch:
	case <-time.After(3 * time.Second):
		t.Fatal(what)
	}
}

func TestFinalReviewOwnerEndBeforeRegistryPublication(t *testing.T) {
	m := NewManager("/bin/sh")
	defer reviewCloseAll(t, m)
	o := NewOwner()
	m.mu.Lock()
	started := make(chan struct{})
	submitted := make(chan error, 1)
	completed := make(chan Result, 2)
	go func() {
		close(started)
		submitted <- m.Submit(o, "unpublished", "read -r gate", false, "", func([]byte) { t.Error("dead owner process started") }, func(r Result) { completed <- r })
	}()
	<-started
	o.Close()
	m.mu.Unlock()
	select {
	case err := <-submitted:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("submission stuck")
	}
	select {
	case r := <-completed:
		if r.ExitCode != ExitCancelled || r.Err != nil {
			t.Fatalf("result=%+v", r)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("missing cancellation")
	}
	if len(m.List()) != 0 {
		t.Fatal("ended owner published a name")
	}
	assertOwnerEmpty(t, o)
	assertBudgetEmpty(t, m)
	select {
	case extra := <-completed:
		t.Fatalf("duplicate=%+v", extra)
	default:
	}
}

func TestFinalReviewPublicationWinsOwnerEndWithoutLostRegistration(t *testing.T) {
	m := NewManager("/bin/sh")
	defer reviewCloseAll(t, m)
	o := NewOwner()
	defer o.Close()
	// Stop final admission at the actual Budget lock, while the transaction
	// holds Owner/Registry. Owner.Close must wait and then see the publication.
	m.budget.mu.Lock()
	locked := true
	defer func() {
		if locked {
			m.budget.mu.Unlock()
		}
	}()
	submitted := make(chan error, 1)
	completed := make(chan Result, 2)
	go func() {
		submitted <- m.Submit(o, "publication-wins", "printf ready; read -r gate", false, "", func([]byte) {}, func(r Result) { completed <- r })
	}()
	deadline := time.Now().Add(3 * time.Second)
	for {
		if !o.mu.TryLock() {
			break
		}
		o.mu.Unlock()
		if time.Now().After(deadline) {
			t.Fatal("transaction never reached Owner lock")
		}
		runtime.Gosched()
	}
	ended := make(chan struct{})
	go func() { o.Close(); close(ended) }()
	select {
	case <-ended:
		t.Fatal("Owner.Close bypassed admission transaction")
	default:
	}
	m.budget.mu.Unlock()
	locked = false
	select {
	case err := <-submitted:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("submission stuck")
	}
	reviewAwait(t, ended, "Owner.Close stuck")
	select {
	case r := <-completed:
		if r.ExitCode == 0 || r.ExitCode == ExitRejected {
			t.Fatalf("admitted then cancelled result=%+v", r)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("owner missed published job")
	}
	assertOwnerEmpty(t, o)
	assertBudgetEmpty(t, m)
	if s := m.Get("publication-wins"); s == nil {
		t.Fatal("actually admitted name was incorrectly rolled back")
	}
	select {
	case extra := <-completed:
		t.Fatalf("duplicate=%+v", extra)
	default:
	}
}

func TestFinalReviewRejectionCallbackReentersRegistryOwnerBudget(t *testing.T) {
	l := DefaultLimits()
	l.SessionBytes = 1
	m, _ := NewManagerWithLimits("/bin/sh", l)
	defer reviewCloseAll(t, m)
	o := NewOwner()
	finished := make(chan struct{})
	var calls atomic.Int32
	go func() {
		err := m.Submit(o, "rejected-new", "exit 7", false, "", func([]byte) { t.Error("rejected process started") }, func(r Result) {
			calls.Add(1)
			if r.ExitCode != ExitRejected || !errors.Is(r.Err, ErrAdmission) {
				t.Errorf("result=%+v", r)
			}
			assertBudgetEmpty(t, m)
			o.Close()
			_ = m.List()
			explicit, err := m.GetOrCreateChecked("callback-explicit")
			if err != nil {
				t.Error(err)
				return
			}
			if !m.Close("callback-explicit") {
				t.Error("nested Close rejected")
			}
			waitClosed(t, explicit)
			m.Submit(NewOwner(), "nested-rejected", "exit 7", false, "", func([]byte) {}, func(r Result) {
				calls.Add(1)
				if r.ExitCode != ExitRejected {
					t.Errorf("nested result=%+v", r)
				}
			})
		})
		if !errors.Is(err, ErrAdmission) {
			t.Errorf("error=%v", err)
		}
		close(finished)
	}()
	reviewAwait(t, finished, "rejection callback reentrancy deadlocked")
	if calls.Load() != 2 || len(m.List()) != 0 {
		t.Fatalf("calls=%d names=%d", calls.Load(), len(m.List()))
	}
	assertBudgetEmpty(t, m)
	assertOwnerEmpty(t, o)
}

func TestFinalReviewDoneWaitsForExternallyDrainedCompletionCallback(t *testing.T) {
	m := NewManager("/bin/sh")
	defer reviewCloseAll(t, m)
	head := submitLifecycle(m, nil, "callback-quiescence", "printf ready; read -r gate", false)
	waitReady(t, head)
	s := m.Get("callback-quiescence")
	entered := make(chan struct{})
	release := make(chan struct{})
	returned := make(chan struct{})
	drained := make(chan int, 1)
	defer func() {
		select {
		case <-release:
		default:
			close(release)
		}
	}()
	var calls atomic.Int32
	m.Submit(nil, "callback-quiescence", "exit 19", false, "", func([]byte) { t.Error("queued job ran") }, func(r Result) {
		calls.Add(1)
		if r.ExitCode != ExitCancelled {
			t.Errorf("cancelled result=%+v", r)
		}
		if !m.Close("callback-quiescence") {
			t.Error("callback Close rejected")
		}
		close(entered)
		<-release
		close(returned)
	})
	go func() { drained <- m.Drain("callback-quiescence") }()
	reviewAwait(t, entered, "cancelled callback did not enter")
	if jobCode(t, head) == 0 {
		t.Fatal("Drain left head alive")
	}
	var premature bool
	select {
	case <-s.Done():
		premature = true
	case <-time.After(500 * time.Millisecond):
	}
	if premature {
		t.Logf("Done closed while cancelled completion callback was still held; old registered pointer=%v", m.Get("callback-quiescence"))
		// A new same-name job now actually runs before that old callback returns.
		assertCode(t, submitLifecycle(m, nil, "callback-quiescence", "exit 23", false), 23)
	}
	close(release)
	reviewAwait(t, returned, "callback did not return")
	select {
	case n := <-drained:
		if n != 1 {
			t.Fatalf("Drain=%d", n)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("Drain never returned")
	}
	waitClosed(t, s)
	if calls.Load() != 1 {
		t.Fatalf("calls=%d", calls.Load())
	}
	if premature {
		t.Fatal("Done/name release violates documented completion-callback quiescence for callbacks delivered by Drain outside the worker")
	}
}
