package session

import (
	"errors"
	"fmt"
	"sync"
)

// ExitRejected means the command never entered the queue. ExitCancelled (-2)
// remains reserved for owner cancellation or cancellation after admission.
const ExitRejected = -3

var (
	ErrAdmission      = errors.New("command admission rejected")
	ErrSessionClosing = errors.New("session is closing; retry after shutdown completion")
)

// Limits bound retained active+queued jobs, their command/metadata string bytes,
// and named session workers. Zero/negative values are not unlimited: invalid
// configurations are rejected by NewManagerWithLimits.
type Limits struct {
	CommandBytes int
	NameBytes    int
	AgentBytes   int
	Sessions     int
	SessionJobs  int
	SessionBytes int
	OwnerJobs    int
	OwnerBytes   int
	ManagerJobs  int
	ManagerBytes int
}

func DefaultLimits() Limits {
	return Limits{
		CommandBytes: 256 * 1024, NameBytes: 128, AgentBytes: 256,
		Sessions: 128, SessionJobs: 65, SessionBytes: 2 * 1024 * 1024,
		OwnerJobs: 128, OwnerBytes: 4 * 1024 * 1024,
		ManagerJobs: 512, ManagerBytes: 16 * 1024 * 1024,
	}
}

func (l Limits) validate() error {
	for _, n := range []int{l.CommandBytes, l.NameBytes, l.AgentBytes, l.Sessions, l.SessionJobs, l.SessionBytes, l.OwnerJobs, l.OwnerBytes, l.ManagerJobs, l.ManagerBytes} {
		if n <= 0 {
			return fmt.Errorf("all admission limits must be positive")
		}
	}
	return nil
}

// Result is delivered exactly once, including immediate rejection. Rejection
// callbacks run synchronously, outside all manager/session/owner/budget locks.
type Result struct {
	ExitCode int
	Err      error
}

type usage struct{ jobs, bytes int }

type admissionBudget struct {
	mu       sync.Mutex
	limits   Limits
	total    usage
	sessions map[*Session]usage
	owners   map[*Owner]usage // nil represents the ownerless API
}

func newBudget(l Limits) *admissionBudget {
	return &admissionBudget{limits: l, sessions: make(map[*Session]usage), owners: make(map[*Owner]usage)}
}

func reject(reason string) error { return fmt.Errorf("%w: %s", ErrAdmission, reason) }

func (b *admissionBudget) validate(name, agent, line string) error {
	l := b.limits
	if len(line) > l.CommandBytes {
		return reject(fmt.Sprintf("command exceeds %d bytes", l.CommandBytes))
	}
	if len(name) > l.NameBytes {
		return reject(fmt.Sprintf("session name exceeds %d bytes", l.NameBytes))
	}
	if len(agent) > l.AgentBytes {
		return reject(fmt.Sprintf("agent exceeds %d bytes", l.AgentBytes))
	}
	return nil
}

func fits(u usage, jobs, bytes, cost int) bool { return u.jobs < jobs && cost <= bytes-u.bytes }

func (b *admissionBudget) acquire(s *Session, o *Owner, cost int) error {
	b.mu.Lock()
	defer b.mu.Unlock()
	l := b.limits
	for _, check := range []struct {
		scope       string
		used        usage
		jobs, bytes int
	}{
		{"session", b.sessions[s], l.SessionJobs, l.SessionBytes},
		{"owner", b.owners[o], l.OwnerJobs, l.OwnerBytes},
		{"manager", b.total, l.ManagerJobs, l.ManagerBytes},
	} {
		if !fits(check.used, check.jobs, check.bytes, cost) {
			return reject(fmt.Sprintf("%s outstanding budget exhausted (max %d jobs / %d bytes)", check.scope, check.jobs, check.bytes))
		}
	}
	u := b.sessions[s]
	u.jobs++
	u.bytes += cost
	b.sessions[s] = u
	u = b.owners[o]
	u.jobs++
	u.bytes += cost
	b.owners[o] = u
	b.total.jobs++
	b.total.bytes += cost
	return nil
}

func (b *admissionBudget) release(s *Session, o *Owner, cost int) {
	b.mu.Lock()
	defer b.mu.Unlock()
	u := b.sessions[s]
	u.jobs--
	u.bytes -= cost
	if u.jobs == 0 {
		delete(b.sessions, s)
	} else {
		b.sessions[s] = u
	}
	u = b.owners[o]
	u.jobs--
	u.bytes -= cost
	if u.jobs == 0 {
		delete(b.owners, o)
	} else {
		b.owners[o] = u
	}
	b.total.jobs--
	b.total.bytes -= cost
}
