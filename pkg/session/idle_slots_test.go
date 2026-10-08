package session

import (
	"errors"
	"fmt"
	"strings"
	"testing"
)

func TestDefaultIdleWorkerSlotsExplicitRejectionExistingWorkAndCloseRecovery(t *testing.T) {
	m := NewManager("/bin/sh")
	defer func() {
		for _, i := range m.List() {
			s := m.Get(i.Name)
			m.Close(i.Name)
			if s != nil {
				waitClosed(t, s)
			}
		}
	}()
	for i := 0; i < m.Limits().Sessions; i++ {
		o := NewOwner()
		name := fmt.Sprintf("idle-%03d", i)
		assertCode(t, submitLifecycle(m, o, name, "exit 7", false), 7)
		o.Close()
		assertOwnerEmpty(t, o)
	}
	assertBudgetEmpty(t, m)
	if len(m.List()) != 128 {
		t.Fatalf("slots=%d", len(m.List()))
	}
	var calls int
	err := m.Submit(NewOwner(), "new-name", "exit 7", false, "", func([]byte) {}, func(r Result) {
		calls++
		if r.ExitCode != ExitRejected || !errors.Is(r.Err, ErrAdmission) || !strings.Contains(r.Err.Error(), "named-session") {
			t.Errorf("result=%+v", r)
		}
	})
	if err == nil || calls != 1 {
		t.Fatalf("limit err=%v completions=%d", err, calls)
	}
	// Existing named work is not locked out when new names hit the idle cap.
	assertCode(t, submitLifecycle(m, nil, "idle-000", "exit 23", false), 23)
	s := m.Get("idle-001")
	if !m.Close("idle-001") {
		t.Fatal("operator close rejected")
	}
	waitClosed(t, s)
	assertCode(t, submitLifecycle(m, nil, "new-name", "exit 7", false), 7)
	if len(m.List()) != 128 {
		t.Fatalf("slots after recovery=%d", len(m.List()))
	}
	assertBudgetEmpty(t, m)
}

func TestStartFailureKeepsDiscoverableIdleSlotUntilExplicitClose(t *testing.T) {
	l := DefaultLimits()
	l.Sessions = 1
	m, _ := NewManagerWithLimits("/no-idle-slot-test-shell", l)
	o := NewOwner()
	assertCode(t, submitLifecycle(m, o, "failed", "exit 7", false), -1)
	o.Close()
	assertOwnerEmpty(t, o)
	assertBudgetEmpty(t, m)
	if list := m.List(); len(list) != 1 || list[0].Status != string(StatusIdle) {
		t.Fatalf("idle failure metadata=%v", list)
	}
	assertCode(t, submitLifecycle(m, nil, "different", "exit 7", false), ExitRejected)
	s := m.Get("failed")
	m.Close("failed")
	waitClosed(t, s)
	assertCode(t, submitLifecycle(m, nil, "different", "exit 7", false), -1)
	s = m.Get("different")
	m.Close("different")
	waitClosed(t, s)
	assertBudgetEmpty(t, m)
}
