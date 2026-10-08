package session

// Independent review fixtures, only in this review's disposable source copy.
import (
	"errors"
	"fmt"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func reviewCloseAll(t *testing.T, m *Manager) {
	t.Helper()
	for _, info := range m.List() {
		s := m.Get(info.Name)
		m.Close(info.Name)
		if s != nil {
			waitClosed(t, s)
		}
	}
}

func TestIndependentWithinCommandCapByteBudgetForeignOwnerRelease(t *testing.T) {
	m := NewManager("/bin/sh")
	defer reviewCloseAll(t, m)
	a, b := NewOwner(), NewOwner()
	defer a.Close()
	defer b.Close()
	head := submitLifecycle(m, a, "aggregate", "printf ready; read -r gate", false)
	waitReady(t, head)
	var calls, rejected, cancelled atomic.Int32
	payload := strings.Repeat(" ", 256*1024-1) + ":" // exactly the maximum, not oversize
	for i := 0; i < 256; i++ {
		err := m.Submit(a, "aggregate", payload, false, "", func([]byte) {}, func(r Result) {
			calls.Add(1)
			switch r.ExitCode {
			case ExitRejected:
				rejected.Add(1)
				if !errors.Is(r.Err, ErrAdmission) {
					t.Errorf("rejection missing reason: %+v", r)
				}
			case ExitCancelled:
				cancelled.Add(1)
			default:
				t.Errorf("large queued command unexpectedly ran: %+v", r)
			}
		})
		if err != nil && !errors.Is(err, ErrAdmission) {
			t.Fatal(err)
		}
	}
	s := m.Get("aggregate")
	pending := s.Info().Pending
	m.budget.mu.Lock()
	used := m.budget.total
	m.budget.mu.Unlock()
	t.Logf("exactly 256KiB per command: queued=%d rejected=%d outstanding jobs=%d string bytes=%d", pending, rejected.Load(), used.jobs, used.bytes)
	if pending != 7 || rejected.Load() != 249 || used.jobs != 8 || used.bytes > m.Limits().SessionBytes {
		t.Fatalf("wrong aggregate accounting: pending=%d rejects=%d usage=%+v", pending, rejected.Load(), used)
	}
	kept := submitLifecycle(m, b, "aggregate", "exit 23", false)
	a.Close()
	if c := jobCode(t, head); c == 0 {
		t.Fatal("head survived owner end")
	}
	assertCode(t, kept, 23)
	if calls.Load() != 256 || cancelled.Load() != 7 || rejected.Load() != 249 {
		t.Fatalf("callbacks=%d cancelled=%d rejected=%d", calls.Load(), cancelled.Load(), rejected.Load())
	}
	assertBudgetEmpty(t, m)
	assertOwnerEmpty(t, a)
	assertOwnerEmpty(t, b)
	assertCode(t, submitLifecycle(m, b, "aggregate", "exit 7", false), 7)
	assertBudgetEmpty(t, m)
}

func TestIndependentRejectedNewNamesMustNotConsumeIdleSlots(t *testing.T) {
	// Default limits; only one Owner is saturated. Every rejected name is NEW.
	m := NewManager("/bin/sh")
	defer reviewCloseAll(t, m)
	full, foreign := NewOwner(), NewOwner()
	defer full.Close()
	defer foreign.Close()
	active := make([]*lifecycleJob, 0, 2)
	var cancelled atomic.Int32
	for index := 0; index < 2; index++ {
		name := fmt.Sprintf("admitted-%d", index)
		head := submitLifecycle(m, full, name, "printf ready; read -r gate", false)
		waitReady(t, head)
		active = append(active, head)
		for queued := 0; queued < 63; queued++ {
			err := m.ExecuteOwned(full, name, "exit 19", false, "", func([]byte) {}, func(code int) {
				if code == ExitCancelled {
					cancelled.Add(1)
				} else {
					t.Errorf("unexpected admitted queue result=%d", code)
				}
			})
			if err != nil {
				t.Fatal(err)
			}
		}
	}
	m.budget.mu.Lock()
	u := m.budget.owners[full]
	m.budget.mu.Unlock()
	if u.jobs != 128 {
		t.Fatalf("owner jobs=%d", u.jobs)
	}
	var rejections atomic.Int32
	for i := 0; i < 126; i++ {
		name := fmt.Sprintf("never-admitted-%03d", i)
		err := m.Submit(full, name, "exit 7", false, "", func([]byte) { t.Error("rejected command started") }, func(r Result) {
			rejections.Add(1)
			if r.ExitCode != ExitRejected || !errors.Is(r.Err, ErrAdmission) || !strings.Contains(r.Err.Error(), "owner outstanding") {
				t.Errorf("unexpected rejection=%+v", r)
			}
		})
		if !errors.Is(err, ErrAdmission) {
			t.Fatalf("name=%s err=%v", name, err)
		}
	}
	before := len(m.List())
	t.Logf("default policy: admitted session names=2, owner-budget rejections=126, named slots after rejection=%d", before)
	full.Close()
	for _, job := range active {
		if code := jobCode(t, job); code == 0 {
			t.Fatal("active survived")
		}
	}
	if cancelled.Load() != 126 || rejections.Load() != 126 {
		t.Fatalf("cancel=%d reject=%d", cancelled.Load(), rejections.Load())
	}
	assertBudgetEmpty(t, m)
	assertOwnerEmpty(t, full)
	var result Result
	var callbacks int
	err := m.Submit(foreign, "legitimate-new-name", "exit 7", false, "", func([]byte) {}, func(r Result) { result = r; callbacks++ })
	if err == nil {
		// Unexpectedly accepted: this is the desired post-fix policy, await its result.
		s := m.Get("legitimate-new-name")
		m.Close("legitimate-new-name")
		waitClosed(t, s)
	}
	t.Logf("after all job/byte/owner credits released: names=%d foreign new-name result=%+v err=%v callbacks=%d", len(m.List()), result, err, callbacks)
	// Demonstrate OperatorClose recovery even for a slot with NO admitted jobs.
	victim := m.Get("never-admitted-000")
	if victim != nil {
		if !m.Close(victim.Name) {
			t.Fatal("operator Close rejected")
		}
		waitClosed(t, victim)
		assertCode(t, submitLifecycle(m, foreign, "operator-recovered", "exit 23", false), 23)
	}
	if before != 2 {
		t.Errorf("rejected requests retained %d empty named workers/slots; expected no persistent slot for never-admitted jobs", before-2)
	}
	if err != nil {
		t.Errorf("a foreign connection remains locked out after temporary owner saturation ended: %v", err)
	}
}

func TestIndependentDoneReservesNameAcrossForeignConnection(t *testing.T) {
	m := NewManager("/bin/sh")
	defer reviewCloseAll(t, m)
	a, b := NewOwner(), NewOwner()
	defer a.Close()
	defer b.Close()
	s := m.GetOrCreate("completion-reserved")
	entered := make(chan struct{})
	release := make(chan struct{})
	finished := make(chan struct{})
	defer func() {
		select {
		case <-release:
		default:
			close(release)
		}
	}()
	m.Submit(a, "completion-reserved", "exit 7", false, "", func([]byte) {}, func(r Result) {
		if r.ExitCode != 7 {
			t.Errorf("result=%+v", r)
		}
		if !m.Close("completion-reserved") {
			t.Error("callback Close rejected")
		}
		close(entered)
		<-release
		close(finished)
	})
	select {
	case <-entered:
	case <-time.After(3 * time.Second):
		t.Fatal("callback did not enter")
	}
	for i := 0; i < 20; i++ {
		var calls int
		err := m.Submit(b, "completion-reserved", "exit 23", false, "", func([]byte) { t.Error("foreign same-name process bypassed reservation") }, func(r Result) {
			calls++
			if r.ExitCode != ExitRejected || !errors.Is(r.Err, ErrSessionClosing) {
				t.Errorf("result=%+v", r)
			}
		})
		if !errors.Is(err, ErrSessionClosing) || calls != 1 {
			t.Fatalf("reservation error=%v calls=%d", err, calls)
		}
	}
	select {
	case <-s.Done():
		t.Fatal("Done before completion callback returned")
	default:
	}
	assertCode(t, submitLifecycle(m, b, "independent-name", "exit 23", false), 23)
	close(release)
	<-finished
	waitClosed(t, s)
	a.Close()
	assertCode(t, submitLifecycle(m, b, "completion-reserved", "exit 7", false), 7)
	assertBudgetEmpty(t, m)
	assertOwnerEmpty(t, a)
	assertOwnerEmpty(t, b)
}
