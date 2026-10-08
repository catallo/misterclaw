package session

import (
	"errors"
	"fmt"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// Keep only admitted names; independent rejected names must be invisible.
func TestParallelRejectedNewNamesAtOwnerAndManagerBudgets(t *testing.T) {
	for _, scope := range []string{"owner", "manager"} {
		t.Run(scope, func(t *testing.T) {
			m := NewManager("/bin/sh")
			defer reviewCloseAll(t, m)
			var owners []*Owner
			var heads []*lifecycleJob
			var cancelled atomic.Int32
			ownerCount := 1
			if scope == "manager" {
				ownerCount = 4
			}
			for oi := 0; oi < ownerCount; oi++ {
				o := NewOwner()
				owners = append(owners, o)
				defer o.Close()
				for si := 0; si < 2; si++ {
					name := fmt.Sprintf("held-%d-%d", oi, si)
					head := submitLifecycle(m, o, name, "printf ready; read -r gate", false)
					waitReady(t, head)
					heads = append(heads, head)
					for j := 0; j < 63; j++ {
						err := m.ExecuteOwned(o, name, "exit 19", false, "", func([]byte) {}, func(code int) {
							if code != ExitCancelled {
								t.Errorf("held queue code=%d", code)
							}
							cancelled.Add(1)
						})
						if err != nil {
							t.Fatal(err)
						}
					}
				}
			}
			// Already admitted foreign work in an existing session must survive
			// unrelated rejected-new-name traffic and the full owner's cleanup.
			var kept *lifecycleJob
			if scope == "owner" {
				foreign := NewOwner()
				defer foreign.Close()
				kept = submitLifecycle(m, foreign, "held-0-0", "exit 23", false)
			}
			full := owners[0]
			if scope == "manager" {
				full = NewOwner()
				defer full.Close()
			}
			m.budget.mu.Lock()
			before := m.budget.total
			ownerEntries := len(m.budget.owners)
			sessionEntries := len(m.budget.sessions)
			m.budget.mu.Unlock()
			const attempts = 256
			start := make(chan struct{})
			var wg sync.WaitGroup
			var callbacks atomic.Int32
			for i := 0; i < attempts; i++ {
				wg.Add(1)
				go func(i int) {
					defer wg.Done()
					<-start
					name := fmt.Sprintf("rejected-%03d", i)
					err := m.Submit(full, name, "exit 7", false, "", func([]byte) { t.Error("rejected process started") }, func(r Result) {
						callbacks.Add(1)
						// Reenter manager and budget-facing operations: callback must
						// be outside every transaction lock, not merely once-only.
						_ = m.List()
						if r.ExitCode != ExitRejected || !errors.Is(r.Err, ErrAdmission) || !strings.Contains(r.Err.Error(), scope+" outstanding") {
							t.Errorf("rejection=%+v", r)
						}
					})
					if !errors.Is(err, ErrAdmission) {
						t.Errorf("Submit err=%v", err)
					}
				}(i)
			}
			close(start)
			done := make(chan struct{})
			go func() { wg.Wait(); close(done) }()
			select {
			case <-done:
			case <-time.After(5 * time.Second):
				t.Fatal("parallel rejection/callback deadlocked")
			}
			if callbacks.Load() != attempts || len(m.List()) != len(heads) {
				t.Fatalf("callbacks=%d slots=%d, admitted names=%d", callbacks.Load(), len(m.List()), len(heads))
			}
			for i := 0; i < attempts; i++ {
				if m.Get(fmt.Sprintf("rejected-%03d", i)) != nil {
					t.Fatal("rejected name registered")
				}
			}
			m.budget.mu.Lock()
			after := m.budget.total
			oe, se := len(m.budget.owners), len(m.budget.sessions)
			m.budget.mu.Unlock()
			if after != before || oe != ownerEntries || se != sessionEntries {
				t.Fatalf("budget side effect: before=%+v after=%+v owners=%d/%d sessions=%d/%d", before, after, ownerEntries, oe, sessionEntries, se)
			}
			if scope == "manager" {
				assertOwnerEmpty(t, full)
			}
			for _, o := range owners {
				o.Close()
			}
			for _, h := range heads {
				if jobCode(t, h) == 0 {
					t.Fatal("head survived cancellation")
				}
			}
			if cancelled.Load() != int32(126*ownerCount) {
				t.Fatalf("cancelled=%d", cancelled.Load())
			}
			if kept != nil {
				assertCode(t, kept, 23)
			}
			assertBudgetEmpty(t, m)
			for _, o := range owners {
				assertOwnerEmpty(t, o)
			}
			assertCode(t, submitLifecycle(m, NewOwner(), "foreign-new-name", "exit 23", false), 23)
			t.Logf("%s budget: %d parallel rejects, %d admitted names only; budgets unchanged then reusable", scope, attempts, len(heads))
		})
	}
}

func TestParallelMixedNewNameAdmissionCreatesOnlySuccessfulSlots(t *testing.T) {
	l := DefaultLimits()
	l.OwnerJobs = 8
	m, _ := NewManagerWithLimits("/bin/sh", l)
	defer reviewCloseAll(t, m)
	o := NewOwner()
	defer o.Close()
	const n = 64
	start := make(chan struct{})
	var wg sync.WaitGroup
	var accepted, rejected, completed atomic.Int32
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			<-start
			err := m.Submit(o, fmt.Sprintf("mixed-%02d", i), "read -r gate", false, "", func([]byte) {}, func(r Result) {
				completed.Add(1)
				if r.ExitCode == ExitRejected {
					rejected.Add(1)
				} else if r.ExitCode == 0 {
					t.Error("barrier unexpectedly finished")
				}
			})
			if err == nil {
				accepted.Add(1)
			} else if !errors.Is(err, ErrAdmission) {
				t.Errorf("err=%v", err)
			}
		}(i)
	}
	close(start)
	wg.Wait()
	if accepted.Load() != 8 || rejected.Load() != n-8 || len(m.List()) != 8 {
		t.Fatalf("accepted=%d rejected=%d names=%d", accepted.Load(), rejected.Load(), len(m.List()))
	}
	o.Close()
	deadline := time.After(5 * time.Second)
	for completed.Load() != n {
		select {
		case <-deadline:
			t.Fatalf("callbacks=%d", completed.Load())
		default:
			time.Sleep(time.Millisecond)
		}
	}
	assertBudgetEmpty(t, m)
	assertOwnerEmpty(t, o)
}

func TestRejectedNameTransactionVersusCloseAndRecreate(t *testing.T) {
	l := DefaultLimits()
	l.ManagerJobs = 1
	m, _ := NewManagerWithLimits("/bin/sh", l)
	defer reviewCloseAll(t, m)
	a, b := NewOwner(), NewOwner()
	defer a.Close()
	defer b.Close()
	head := submitLifecycle(m, a, "used", "printf ready; read -r gate", false)
	waitReady(t, head)
	// An explicit GetOrCreate reserves a legitimate idle name independently
	// of command admission. Failed Submit must not roll this object back.
	explicit, err := m.GetOrCreateChecked("explicit-idle")
	if err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"never-used", "explicit-idle", "used"} {
		var calls int
		err := m.Submit(b, name, "exit 7", false, "", func([]byte) {}, func(r Result) {
			calls++
			if r.ExitCode != ExitRejected {
				t.Errorf("result=%+v", r)
			}
		})
		if err == nil || calls != 1 {
			t.Fatalf("name=%s err=%v callbacks=%d", name, err, calls)
		}
	}
	if m.Get("never-used") != nil || m.Get("explicit-idle") != explicit || m.Get("used") == nil {
		t.Fatal("rejection removed an existing name or added a new one")
	}
	s := m.Get("used")
	m.Close("used")
	_ = jobCode(t, head)
	waitClosed(t, s)
	assertCode(t, submitLifecycle(m, b, "used", "exit 23", false), 23)
	assertCode(t, submitLifecycle(m, b, "never-used", "exit 7", false), 7)
	assertBudgetEmpty(t, m)
	assertOwnerEmpty(t, a)
	assertOwnerEmpty(t, b)
}

func TestOwnerEndRacesPublicationWithoutRejectedSlotsOrLeakedReferences(t *testing.T) {
	for round := 0; round < 20; round++ {
		l := DefaultLimits()
		l.OwnerJobs = 4
		m, _ := NewManagerWithLimits("/bin/sh", l)
		o := NewOwner()
		start := make(chan struct{})
		var wg sync.WaitGroup
		var callbacks atomic.Int32
		rejectedNames := make(chan string, 32)
		for i := 0; i < 32; i++ {
			wg.Add(1)
			go func(i int) {
				defer wg.Done()
				<-start
				name := fmt.Sprintf("race-%02d", i)
				err := m.Submit(o, name, "read -r gate", false, "", func([]byte) {}, func(r Result) { callbacks.Add(1) })
				if err != nil {
					rejectedNames <- name
				}
			}(i)
		}
		wg.Add(1)
		go func() { defer wg.Done(); <-start; o.Close() }()
		close(start)
		wg.Wait()
		close(rejectedNames)
		deadline := time.After(5 * time.Second)
		for callbacks.Load() != 32 {
			select {
			case <-deadline:
				t.Fatal("owner publication race lost completion")
			default:
				time.Sleep(time.Millisecond)
			}
		}
		for name := range rejectedNames {
			if m.Get(name) != nil {
				t.Fatalf("round=%d rejected slot=%s", round, name)
			}
		}
		assertBudgetEmpty(t, m)
		assertOwnerEmpty(t, o)
		reviewCloseAll(t, m)
	}
}
