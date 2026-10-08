package session

import (
	"fmt"
	"os"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"
)

type lifecycleJob struct {
	ready chan struct{}
	done  chan int
	once  sync.Once
	mu    sync.Mutex
	out   string
}

func submitLifecycle(m *Manager, owner *Owner, name, line string, pty bool) *lifecycleJob {
	j := &lifecycleJob{ready: make(chan struct{}), done: make(chan int, 2)}
	m.ExecuteOwned(owner, name, line, pty, "", func(data []byte) {
		j.mu.Lock()
		j.out += string(data)
		if strings.Contains(j.out, "ready") {
			j.once.Do(func() { close(j.ready) })
		}
		j.mu.Unlock()
	}, func(code int) { j.done <- code })
	return j
}

func waitReady(t *testing.T, j *lifecycleJob) {
	t.Helper()
	select {
	case <-j.ready:
	case <-time.After(5 * time.Second):
		t.Fatal("command did not reach barrier")
	}
}

func jobCode(t *testing.T, j *lifecycleJob) int {
	t.Helper()
	select {
	case c := <-j.done:
		return c
	case <-time.After(5 * time.Second):
		t.Fatal("missing completion")
		return 0
	}
}

func assertCode(t *testing.T, j *lifecycleJob, want int) {
	t.Helper()
	if got := jobCode(t, j); got != want {
		t.Fatalf("completion %d, want %d", got, want)
	}
}

func assertOwnerEmpty(t *testing.T, o *Owner) {
	t.Helper()
	o.mu.Lock()
	defer o.mu.Unlock()
	if len(o.sessions) != 0 {
		t.Fatalf("owner retained %d session registrations", len(o.sessions))
	}
}

func TestOwnerDisconnectKillsOwnActiveAndQueuedOnly(t *testing.T) {
	for _, pty := range []bool{false, true} {
		t.Run(fmt.Sprintf("pty=%v", pty), func(t *testing.T) {
			m := NewManager("/bin/sh")
			defer m.Close("shared")
			old, fresh := NewOwner(), NewOwner()
			defer old.Close()
			defer fresh.Close()
			active := submitLifecycle(m, old, "shared", "printf ready; read -r line", pty)
			waitReady(t, active)
			ownQueued := submitLifecycle(m, old, "shared", "exit 19", pty)
			otherQueued := submitLifecycle(m, fresh, "shared", "exit 23", pty)
			old.Close()
			if c := jobCode(t, active); c == 0 || c == ExitCancelled {
				t.Fatalf("active wasn't group-killed: %d", c)
			}
			assertCode(t, ownQueued, ExitCancelled)
			assertCode(t, otherQueued, 23)
			assertOwnerEmpty(t, old)
			assertOwnerEmpty(t, fresh)
		})
	}
}

func TestOwnerDisconnectPreservesOtherActiveAndCancelsOwnQueuedPromptly(t *testing.T) {
	m := NewManager("/bin/sh")
	defer m.Close("shared")
	old, fresh := NewOwner(), NewOwner()
	defer old.Close()
	defer fresh.Close()
	active := submitLifecycle(m, fresh, "shared", "printf ready; read -r line", false)
	waitReady(t, active)
	cancelled := submitLifecycle(m, old, "shared", "exit 19", false)
	kept := submitLifecycle(m, fresh, "shared", "exit 23", false)
	old.Close()
	assertCode(t, cancelled, ExitCancelled) // no need for somebody else's active command to exit
	if err := m.WriteInput("shared", []byte("release\n")); err != nil {
		t.Fatal(err)
	}
	assertCode(t, active, 0)
	assertCode(t, kept, 23)
	assertOwnerEmpty(t, old)
	assertOwnerEmpty(t, fresh)
}

func TestGlobalDrainStillCancelsAllOwners(t *testing.T) {
	m := NewManager("/bin/sh")
	defer m.Close("shared")
	a, b := NewOwner(), NewOwner()
	defer a.Close()
	defer b.Close()
	active := submitLifecycle(m, a, "shared", "printf ready; read -r line", false)
	waitReady(t, active)
	qa := submitLifecycle(m, a, "shared", "exit 19", false)
	qb := submitLifecycle(m, b, "shared", "exit 23", false)
	if n := m.Drain("shared"); n != 2 {
		t.Fatalf("Drain = %d, want 2", n)
	}
	assertCode(t, qa, ExitCancelled)
	assertCode(t, qb, ExitCancelled)
	if code := jobCode(t, active); code == 0 {
		t.Fatal("Drain left active process alive")
	}
	assertCode(t, submitLifecycle(m, b, "shared", "exit 7", false), 7)
	assertOwnerEmpty(t, a)
	assertOwnerEmpty(t, b)
}

func TestOwnedSameSessionSerialDifferentSessionsParallel(t *testing.T) {
	m := NewManager("/bin/sh")
	defer m.Close("a")
	defer m.Close("b")
	a, b := NewOwner(), NewOwner()
	defer a.Close()
	defer b.Close()
	first := submitLifecycle(m, a, "a", "printf ready; read -r line", false)
	waitReady(t, first)
	second := submitLifecycle(m, b, "a", "printf ready; read -r line", false)
	parallel := submitLifecycle(m, b, "b", "printf ready; read -r line", false)
	waitReady(t, parallel) // proves parallelism, not merely eventual completions
	select {
	case <-second.ready:
		t.Fatal("same session ran simultaneously")
	default:
	}
	if got := m.Get("a").Info().Pending; got != 1 {
		t.Fatalf("Pending = %d", got)
	}
	if err := m.WriteInput("a", []byte("go\n")); err != nil {
		t.Fatal(err)
	}
	assertCode(t, first, 0)
	waitReady(t, second)
	if err := m.WriteInput("a", []byte("go\n")); err != nil {
		t.Fatal(err)
	}
	if err := m.WriteInput("b", []byte("go\n")); err != nil {
		t.Fatal(err)
	}
	assertCode(t, second, 0)
	assertCode(t, parallel, 0)
}

func TestOwnerCloseRacesSubmissionAndIsIdempotent(t *testing.T) {
	m := NewManager("/bin/sh")
	defer m.Close("reused")
	for i := 0; i < 100; i++ {
		o := NewOwner()
		start := make(chan struct{})
		var wg sync.WaitGroup
		jobs := make(chan *lifecycleJob, 4)
		for j := 0; j < 4; j++ {
			wg.Add(1)
			go func() {
				defer wg.Done()
				<-start
				jobs <- submitLifecycle(m, o, "reused", "printf ready; read -r line", false)
			}()
		}
		wg.Add(1)
		go func() { defer wg.Done(); <-start; o.Close(); o.Close() }()
		close(start)
		wg.Wait()
		close(jobs)
		for job := range jobs {
			code := jobCode(t, job)
			if code == 0 {
				t.Fatal("cancelled command unexpectedly finished normally")
			}
			select {
			case extra := <-job.done:
				t.Fatalf("duplicate completion %d", extra)
			default:
			}
		}
		assertOwnerEmpty(t, o)
	}
}

func TestCloseRecreateAndOldOwnerCannotTouchNewSession(t *testing.T) {
	m := NewManager("/bin/sh")
	old, fresh := NewOwner(), NewOwner()
	defer old.Close()
	defer fresh.Close()
	defer m.Close("reused")
	active := submitLifecycle(m, old, "reused", "printf ready; read -r line", false)
	waitReady(t, active)
	queued := submitLifecycle(m, old, "reused", "exit 19", false)
	s := m.Get("reused")
	if !m.Close("reused") {
		t.Fatal("Close failed")
	}
	assertCode(t, queued, ExitCancelled)
	if jobCode(t, active) == 0 {
		t.Fatal("Close did not kill active")
	}
	select {
	case <-s.done:
	default:
		t.Fatal("closed session worker leaked")
	}
	active = submitLifecycle(m, fresh, "reused", "printf ready; read -r line", false)
	waitReady(t, active)
	old.Close()
	old.Close()
	s.Close()
	s.Close()
	if err := m.WriteInput("reused", []byte("go\n")); err != nil {
		t.Fatal(err)
	}
	assertCode(t, active, 0)
	assertOwnerEmpty(t, old)
	assertOwnerEmpty(t, fresh)
}

func TestOwnerRegistrationsReleasedOnStartFailureAndLateSubmission(t *testing.T) {
	m := NewManager("/nonexistent-lifecycle-shell")
	defer m.Close("bad-shell")
	o := NewOwner()
	assertCode(t, submitLifecycle(m, o, "bad-shell", "printf unused", false), -1)
	assertOwnerEmpty(t, o)
	o.Close()
	assertCode(t, submitLifecycle(m, o, "must-not-exist", "printf unused", false), ExitCancelled)
	if m.Get("must-not-exist") != nil {
		t.Fatal("closed owner created a session")
	}
}

func TestOwnerDisconnectKillsOrdinaryProcessGroupChildren(t *testing.T) {
	m := NewManager("/bin/sh")
	defer m.Close("tree")
	o := NewOwner()
	defer o.Close()
	j := submitLifecycle(m, o, "tree", "sleep 30 & printf '%s:ready\\n' \"$!\"; wait", false)
	waitReady(t, j)
	j.mu.Lock()
	text := strings.TrimSuffix(strings.TrimSpace(j.out), ":ready")
	j.mu.Unlock()
	pid, err := strconv.Atoi(text)
	if err != nil {
		t.Fatal(err)
	}
	o.Close()
	if jobCode(t, j) == 0 {
		t.Fatal("active process wasn't killed")
	}
	// A reparented child may briefly be a zombie before init reaps it. It is
	// stopped, not surviving execution; never signal any PID from this check.
	deadline := time.Now().Add(5 * time.Second)
	for {
		data, err := os.ReadFile(fmt.Sprintf("/proc/%d/stat", pid))
		if os.IsNotExist(err) || strings.Contains(string(data), ") Z ") {
			break
		}
		if syscall.Kill(pid, 0) == syscall.ESRCH {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("own child %d still executing", pid)
		}
		time.Sleep(time.Millisecond)
	}
}
