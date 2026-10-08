package session

import (
	"errors"
	"strings"
	"testing"
)

func TestNeverAdmittedNamesHaveNoArtifactsForEveryBudgetRejection(t *testing.T) {
	for _, scope := range []string{"session", "owner", "manager"} {
		t.Run(scope, func(t *testing.T) {
			l := DefaultLimits()
			switch scope {
			case "session":
				l.SessionBytes = 1
			case "owner":
				l.OwnerBytes = 1
			case "manager":
				l.ManagerBytes = 1
			}
			m, _ := NewManagerWithLimits("/bin/sh", l)
			o := NewOwner()
			defer o.Close()
			var calls int
			err := m.Submit(o, "never-published", "exit 7", false, "", func([]byte) { t.Error("rejected process started") }, func(r Result) {
				calls++
				_ = m.List()
				o.Close()
				if r.ExitCode != ExitRejected || !errors.Is(r.Err, ErrAdmission) || !strings.Contains(r.Err.Error(), scope+" outstanding") {
					t.Errorf("result=%+v", r)
				}
			})
			if err == nil || calls != 1 || len(m.List()) != 0 {
				t.Fatalf("err=%v callbacks=%d names=%d", err, calls, len(m.List()))
			}
			assertBudgetEmpty(t, m)
			assertOwnerEmpty(t, o)
		})
	}
}

func TestExplicitIdleReservationsAndAdmittedStartFailureKeepTheirOwnSlots(t *testing.T) {
	l := DefaultLimits()
	l.Sessions = 1
	l.SessionBytes = 1
	m, _ := NewManagerWithLimits("/bin/sh", l)
	explicit, err := m.GetOrCreateChecked("explicit")
	if err != nil {
		t.Fatal(err)
	}
	assertCode(t, submitLifecycle(m, NewOwner(), "explicit", "exit 7", false), ExitRejected)
	if m.Get("explicit") != explicit {
		t.Fatal("rejection removed explicit idle reservation")
	}
	assertCode(t, submitLifecycle(m, NewOwner(), "new", "exit 7", false), ExitRejected)
	if m.Get("new") != nil || len(m.List()) != 1 {
		t.Fatal("named-limit rejection changed registry")
	}
	m.Close("explicit")
	waitClosed(t, explicit)
	assertBudgetEmpty(t, m)
	bad := NewManager("/no-transaction-start-shell")
	o := NewOwner()
	defer o.Close()
	assertCode(t, submitLifecycle(bad, o, "admitted-failed-start", "exit 7", false), -1)
	s := bad.Get("admitted-failed-start")
	if s == nil || s.Info().Status != string(StatusIdle) {
		t.Fatal("admitted start failure lost its deliberate idle metadata")
	}
	assertBudgetEmpty(t, bad)
	assertOwnerEmpty(t, o)
	bad.Close(s.Name)
	waitClosed(t, s)
}
