package session

import "testing"

func TestHeldOwnerCancellationAllowsForeignWorkUntilExplicitClose(t *testing.T) {
	m := NewManager("/bin/sh")
	defer reviewCloseAll(t, m)
	old, foreign := NewOwner(), NewOwner()
	defer old.Close()
	defer foreign.Close()
	head := submitLifecycle(m, foreign, "foreign-progress", "printf ready; read -r gate", false)
	waitReady(t, head)
	s := m.Get("foreign-progress")
	entered, release, ended := make(chan struct{}), make(chan struct{}), make(chan struct{})
	defer func() {
		select {
		case <-release:
		default:
			close(release)
		}
	}()
	m.Submit(old, s.Name, "exit 19", false, "", func([]byte) { t.Error("cancelled own queue ran") }, func(r Result) {
		if r.ExitCode != ExitCancelled {
			t.Errorf("result=%+v", r)
		}
		close(entered)
		<-release
	})
	kept := submitLifecycle(m, foreign, s.Name, "exit 23", false)
	go func() { old.Close(); close(ended) }()
	reviewAwait(t, entered, "owner callback missing")
	if s.Info().Status != string(StatusRunning) {
		t.Fatal("owner end stopped foreign head")
	}
	if err := m.WriteInput(s.Name, []byte("release\n")); err != nil {
		t.Fatal(err)
	}
	assertCode(t, head, 0)
	assertCode(t, kept, 23)
	// A held callback is not a global execution barrier; only explicit Close
	// must join its lifetime at Done and keep the name reserved.
	if !m.Close(s.Name) {
		t.Fatal("Close refused")
	}
	select {
	case <-s.Done():
		t.Fatal("Done before foreign-independent cancellation callback returned")
	default:
	}
	assertBudgetEmpty(t, m)
	assertOwnerEmpty(t, old)
	assertOwnerEmpty(t, foreign)
	close(release)
	reviewAwait(t, ended, "Owner.Close stuck")
	waitClosed(t, s)
	assertCode(t, submitLifecycle(m, foreign, s.Name, "exit 7", false), 7)
}
