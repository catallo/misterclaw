package session

import (
	"errors"
	"fmt"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func TestIndependentManagerRejectedNewNamesMustNotConsumeIdleSlots(t *testing.T) {
	m := NewManager("/bin/sh")
	defer reviewCloseAll(t, m)
	owners := []*Owner{NewOwner(), NewOwner(), NewOwner(), NewOwner()}
	defer func() {
		for _, o := range owners {
			o.Close()
		}
	}()
	active := make([]*lifecycleJob, 0, 8)
	var cancelled atomic.Int32
	for ownerIndex, o := range owners {
		for index := 0; index < 2; index++ {
			name := fmt.Sprintf("manager-full-%d-%d", ownerIndex, index)
			head := submitLifecycle(m, o, name, "printf ready; read -r gate", false)
			waitReady(t, head)
			active = append(active, head)
			for j := 0; j < 63; j++ {
				err := m.ExecuteOwned(o, name, "exit 19", false, "", func([]byte) {}, func(code int) {
					if code == ExitCancelled {
						cancelled.Add(1)
					} else {
						t.Errorf("unexpected queued result=%d", code)
					}
				})
				if err != nil {
					t.Fatal(err)
				}
			}
		}
	}
	m.budget.mu.Lock()
	jobs := m.budget.total.jobs
	m.budget.mu.Unlock()
	if jobs != 512 {
		t.Fatalf("manager jobs=%d", jobs)
	}
	foreign := NewOwner()
	defer foreign.Close()
	var rejections atomic.Int32
	for j := 0; j < 120; j++ {
		name := fmt.Sprintf("manager-never-admitted-%03d", j)
		err := m.Submit(foreign, name, "exit 7", false, "", func([]byte) { t.Error("rejected process started") }, func(r Result) {
			rejections.Add(1)
			if r.ExitCode != ExitRejected || !errors.Is(r.Err, ErrAdmission) || !strings.Contains(r.Err.Error(), "manager outstanding") {
				t.Errorf("result=%+v", r)
			}
		})
		if !errors.Is(err, ErrAdmission) {
			t.Fatalf("err=%v", err)
		}
	}
	retained := len(m.List())
	t.Logf("default manager policy: 512 outstanding jobs in 8 names; 120 manager-budget rejections create named slots=%d", retained)
	for _, o := range owners {
		o.Close()
	}
	for _, j := range active {
		if jobCode(t, j) == 0 {
			t.Fatal("active survived")
		}
	}
	if cancelled.Load() != 504 || rejections.Load() != 120 {
		t.Fatalf("cancel=%d reject=%d", cancelled.Load(), rejections.Load())
	}
	assertBudgetEmpty(t, m)
	assertOwnerEmpty(t, foreign)
	for _, o := range owners {
		assertOwnerEmpty(t, o)
	}
	// Follow-up latch: on a corrected implementation this Submit is accepted,
	// so await its asynchronous callback before reading the logged result.
	var result Result
	completed := make(chan struct{})
	err := m.Submit(foreign, "after-manager-pressure", "exit 7", false, "", func([]byte) {}, func(r Result) { result = r; close(completed) })
	select {
	case <-completed:
	case <-time.After(5 * time.Second):
		t.Fatal("missing post-pressure completion")
	}
	t.Logf("after temporary manager pressure is fully released: result=%+v err=%v slots=%d", result, err, len(m.List()))
	if retained != 8 {
		t.Errorf("manager-budget rejected jobs retained %d never-admitted idle slots", retained-8)
	}
	if err != nil {
		t.Errorf("new name still rejected after manager job/byte budgets cleared: %v", err)
	}
}
