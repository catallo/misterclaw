package session

import (
	"errors"
	"sync"
	"testing"
	"time"
)

func waitClosed(t *testing.T, s *Session) {
	t.Helper()
	select {
	case <-s.Done():
	case <-time.After(5 * time.Second):
		t.Fatal("shutdown did not complete")
	}
}

func TestAsyncCloseOutputReentrancyReservationAndRecreate(t *testing.T) {
	m := NewManager("/bin/sh")
	o, fresh := NewOwner(), NewOwner()
	defer o.Close()
	defer fresh.Close()
	s := m.GetOrCreate("reserved")
	requested := make(chan bool, 1)
	release := make(chan struct{})
	completed := make(chan int, 1)
	var once sync.Once
	defer func() {
		select {
		case <-release:
		default:
			close(release)
		}
		s.Close()
		waitClosed(t, s)
		m.Close("reserved")
		m.Close("parallel")
	}()
	m.ExecuteOwned(o, "reserved", "head -c 262144 /dev/zero", false, "", func([]byte) {
		once.Do(func() { requested <- m.Close("reserved"); <-release })
	}, func(code int) { completed <- code })
	select {
	case accepted := <-requested:
		if !accepted {
			t.Fatal("output close rejected")
		}
	case <-time.After(500 * time.Millisecond):
		t.Fatal("direct output callback Close stalled")
	}
	if m.Close("reserved") {
		t.Fatal("duplicate close accepted")
	}
	select {
	case <-s.Done():
		t.Fatal("Done closed before blocked output returned")
	default:
	}
	if m.Get("reserved") != s || s.Info().Status != string(StatusClosing) {
		t.Fatal("closing name was released too early")
	}
	var results int
	err := m.Submit(fresh, "reserved", "exit 7", false, "", func([]byte) { t.Error("new same-name process started before old output returned") }, func(r Result) {
		results++
		if r.ExitCode != ExitRejected || !errors.Is(r.Err, ErrSessionClosing) {
			t.Errorf("closing result=%+v", r)
		}
	})
	if !errors.Is(err, ErrSessionClosing) || results != 1 {
		t.Fatalf("err=%v calls=%d", err, results)
	}
	assertOwnerEmpty(t, fresh)
	// A different name must still run while the old callback is deliberately
	// held; neither Close nor admission holds a manager-wide execution lock.
	assertCode(t, submitLifecycle(m, fresh, "parallel", "exit 23", false), 23)
	m.budget.mu.Lock()
	u := m.budget.total
	m.budget.mu.Unlock()
	if u.jobs != 1 {
		t.Fatalf("old active budget prematurely freed: %+v", u)
	}
	close(release)
	select {
	case <-completed:
	case <-time.After(5 * time.Second):
		t.Fatal("missing old completion")
	}
	waitClosed(t, s)
	if m.Get("reserved") != nil {
		t.Fatal("reserved name not released at Done")
	}
	assertBudgetEmpty(t, m)
	assertOwnerEmpty(t, o)
	newSession := m.GetOrCreate("reserved")
	if newSession == s || newSession == nil {
		t.Fatal("session wasn't recreated after quiescence")
	}
	assertCode(t, submitLifecycle(m, fresh, "reserved", "exit 7", false), 7)
	m.Close("reserved")
	waitClosed(t, newSession)
	assertBudgetEmpty(t, m)
}

func TestAsyncCloseReleasesCancelledOwnerBudgetsAndWorkerSlot(t *testing.T) {
	l := DefaultLimits()
	l.Sessions = 1
	m, _ := NewManagerWithLimits("/bin/sh", l)
	a, b := NewOwner(), NewOwner()
	defer a.Close()
	defer b.Close()
	active := submitLifecycle(m, a, "one", "printf ready; read -r gate", false)
	waitReady(t, active)
	qa := submitLifecycle(m, a, "one", "exit 7", false)
	qb := submitLifecycle(m, b, "one", "exit 23", false)
	s := m.Get("one")
	if !m.Close("one") {
		t.Fatal("Close refused")
	}
	_ = jobCode(t, active)
	assertCode(t, qa, ExitCancelled)
	assertCode(t, qb, ExitCancelled)
	waitClosed(t, s)
	assertBudgetEmpty(t, m)
	assertOwnerEmpty(t, a)
	assertOwnerEmpty(t, b)
	// Named-worker capacity also becomes reusable; changing names cannot
	// exceed the cap while old workers are still in the closing state.
	newSession, err := m.GetOrCreateChecked("two")
	if err != nil || newSession == nil {
		t.Fatalf("released worker slot err=%v", err)
	}
	m.Close("two")
	waitClosed(t, newSession)
}
