package session

import (
	"errors"
	"fmt"
	"strings"
	"sync/atomic"
	"testing"
)

func assertBudgetEmpty(t *testing.T, m *Manager) {
	t.Helper()
	m.budget.mu.Lock()
	defer m.budget.mu.Unlock()
	if m.budget.total != (usage{}) || len(m.budget.sessions) != 0 || len(m.budget.owners) != 0 {
		t.Fatalf("retained budgets: total=%+v sessions=%d owners=%d", m.budget.total, len(m.budget.sessions), len(m.budget.owners))
	}
}

func TestAdmissionScopesCountAndBytes(t *testing.T) {
	for _, scope := range []string{"session", "owner", "manager"} {
		for _, byBytes := range []bool{false, true} {
			t.Run(fmt.Sprintf("%s/bytes=%v", scope, byBytes), func(t *testing.T) {
				limits := DefaultLimits()
				line := "printf ready; read -r gate"
				cost := len(line) + len("/bin/sh") + len("a")
				switch scope {
				case "session":
					if byBytes {
						limits.SessionBytes = cost
					} else {
						limits.SessionJobs = 1
					}
				case "owner":
					if byBytes {
						limits.OwnerBytes = cost
					} else {
						limits.OwnerJobs = 1
					}
				case "manager":
					if byBytes {
						limits.ManagerBytes = cost
					} else {
						limits.ManagerJobs = 1
					}
				}
				m, err := NewManagerWithLimits("/bin/sh", limits)
				if err != nil {
					t.Fatal(err)
				}
				o, other := NewOwner(), NewOwner()
				defer o.Close()
				defer other.Close()
				active := submitLifecycle(m, o, "a", line, false)
				waitReady(t, active)
				name, owner := "a", other
				if scope == "owner" {
					name, owner = "b", o
				}
				if scope == "manager" {
					name = "b"
				}
				var calls int
				err = m.Submit(owner, name, "exit 7", false, "", func([]byte) {}, func(r Result) {
					calls++
					if r.ExitCode != ExitRejected || !errors.Is(r.Err, ErrAdmission) || !strings.Contains(r.Err.Error(), scope) {
						t.Errorf("result=%+v", r)
					}
				})
				if !errors.Is(err, ErrAdmission) || calls != 1 {
					t.Fatalf("err=%v callbacks=%d", err, calls)
				}
				assertOwnerEmpty(t, other)
				o.Close()
				_ = jobCode(t, active)
				assertOwnerEmpty(t, o)
				assertBudgetEmpty(t, m)
				// Rejection must not consume a budget reservation.
				assertCode(t, submitLifecycle(m, other, "a", "exit 7", false), 7)
				assertBudgetEmpty(t, m)
				m.Close("a")
				m.Close("b")
			})
		}
	}
}

func TestAdmissionSingleCommandMetadataAndSessionWorkerLimit(t *testing.T) {
	l := DefaultLimits()
	l.CommandBytes = 8
	l.NameBytes = 4
	l.AgentBytes = 3
	l.Sessions = 2
	m, err := NewManagerWithLimits("/bin/sh", l)
	if err != nil {
		t.Fatal(err)
	}
	for _, c := range []struct{ name, line, agent string }{{"a", "123456789", ""}, {"abcde", ":", ""}, {"a", ":", "xxxx"}} {
		var calls int
		err := m.Submit(nil, c.name, c.line, false, c.agent, func([]byte) {}, func(r Result) {
			calls++
			if r.ExitCode != ExitRejected || r.Err == nil {
				t.Errorf("result=%+v", r)
			}
		})
		if err == nil || calls != 1 {
			t.Fatalf("validation err=%v callbacks=%d", err, calls)
		}
	}
	if len(m.List()) != 0 {
		t.Fatal("invalid metadata created workers")
	}
	m.GetOrCreate("a")
	m.GetOrCreate("b")
	if s, err := m.GetOrCreateChecked("c"); s != nil || !errors.Is(err, ErrAdmission) {
		t.Fatalf("session limit s=%v err=%v", s, err)
	}
	var calls int
	m.Execute("c", ":", false, "", func([]byte) {}, func(code int) {
		calls++
		if code != ExitRejected {
			t.Errorf("code=%d", code)
		}
	})
	if calls != 1 || len(m.List()) != 2 {
		t.Fatal("worker limit bypassed")
	}
	m.Close("a")
	m.Close("b")
	assertBudgetEmpty(t, m)
}

func TestAdmissionReleaseOnDrainOwnerCloseStartFailure(t *testing.T) {
	m := NewManager("/bin/sh")
	o, other := NewOwner(), NewOwner()
	defer o.Close()
	defer other.Close()
	active := submitLifecycle(m, other, "a", "printf ready; read -r gate", false)
	waitReady(t, active)
	queued := submitLifecycle(m, o, "a", "exit 7", false)
	o.Close()
	assertCode(t, queued, ExitCancelled)
	assertOwnerEmpty(t, o)
	m.budget.mu.Lock()
	u := m.budget.total
	m.budget.mu.Unlock()
	if u.jobs != 1 {
		t.Fatalf("foreign active budget changed: %+v", u)
	}
	m.Drain("a")
	_ = jobCode(t, active)
	assertBudgetEmpty(t, m)
	m.Close("a")
	bad := NewManager("/no-admission-test-shell")
	assertCode(t, submitLifecycle(bad, other, "bad", ":", false), -1)
	assertBudgetEmpty(t, bad)
	assertOwnerEmpty(t, other)
	bad.Close("bad")
}

func TestAdmissionConcurrentRejectionCallbacksAndReuse(t *testing.T) {
	l := DefaultLimits()
	l.SessionJobs = 3
	m, _ := NewManagerWithLimits("/bin/sh", l)
	o := NewOwner()
	defer o.Close()
	defer m.Close("a")
	active := submitLifecycle(m, o, "a", "printf ready; read -r gate", false)
	waitReady(t, active)
	var calls, rejects atomic.Int32
	finished := make(chan struct{}, 100)
	for i := 0; i < 100; i++ {
		go func() {
			m.ExecuteOwned(o, "a", "exit 7", false, "", func([]byte) {}, func(code int) {
				calls.Add(1)
				if code == ExitRejected {
					rejects.Add(1)
				}
			})
			finished <- struct{}{}
		}()
	}
	for i := 0; i < 100; i++ {
		<-finished
	}
	if rejects.Load() != 98 {
		t.Fatalf("rejected=%d want 98", rejects.Load())
	}
	o.Close()
	_ = jobCode(t, active)
	if calls.Load() != 100 {
		t.Fatalf("callbacks=%d want 100", calls.Load())
	}
	assertBudgetEmpty(t, m)
	assertOwnerEmpty(t, o)
}

func TestAdmissionInvalidLimitConfiguration(t *testing.T) {
	l := DefaultLimits()
	l.OwnerBytes = 0
	if m, err := NewManagerWithLimits("/bin/sh", l); m != nil || err == nil {
		t.Fatalf("m=%v err=%v", m, err)
	}
}
