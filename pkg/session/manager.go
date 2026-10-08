package session

import (
	"fmt"
	"strings"
	"sync"
	"time"

	ptyPkg "github.com/catallo/misterclaw/pkg/pty"
)

// Status represents the state of a session.
type Status string

const (
	StatusIdle    Status = "idle"
	StatusRunning Status = "running"
	StatusClosing Status = "closing"
)

// ExitCancelled is reported for commands cancelled before they start.
const ExitCancelled = -2

// Owner is an internal connection lifetime, never a client-supplied ID.
// Only live commands retain registrations; closed owners leave no manager
// tombstones, even when a named session is reused for many reconnects.
type Owner struct {
	mu       sync.Mutex
	done     chan struct{}
	sessions map[*Session]int
}

func NewOwner() *Owner {
	return &Owner{done: make(chan struct{}), sessions: make(map[*Session]int)}
}

func (o *Owner) cancelled() bool {
	if o == nil {
		return false
	}
	select {
	case <-o.done:
		return true
	default:
		return false
	}
}

func (o *Owner) acquire(s *Session) bool {
	if o == nil {
		return true
	}
	o.mu.Lock()
	defer o.mu.Unlock()
	if o.cancelled() {
		return false
	}
	o.sessions[s]++
	return true
}

func (o *Owner) release(s *Session) {
	if o == nil {
		return
	}
	o.mu.Lock()
	defer o.mu.Unlock()
	if n := o.sessions[s]; n > 1 {
		o.sessions[s] = n - 1
	} else {
		delete(o.sessions, s)
	}
}

// Close cancels this connection's commands only, and is safe to repeat or race
// with submission. Session pointers (not names) also protect a recreated
// session from an older connection's cleanup.
func (o *Owner) Close() {
	o.mu.Lock()
	if o.cancelled() {
		o.mu.Unlock()
		return
	}
	close(o.done)
	sessions := o.sessions
	o.sessions = nil
	o.mu.Unlock()
	// Never hold the owner or manager lock while starting/killing a process
	// or invoking a callback. Unrelated sessions can continue independently.
	for s := range sessions {
		s.drainOwner(o)
	}
}

// Info holds metadata about a session for list responses.
type Info struct {
	Name    string `json:"name"`
	Status  string `json:"status"`
	Agent   string `json:"agent,omitempty"`
	Pending int    `json:"pending"`
}

type command struct {
	owner    *Owner
	gen      uint64
	shell    string
	line     string
	usePty   bool
	outputCb ptyPkg.OutputCallback
	doneCb   func(Result)
	cost     int
	admitted bool
}

// Session represents a named execution context with sequential command execution.
type Session struct {
	Name     string
	Agent    string
	status   Status
	executor ptyPkg.Executor
	active   *command
	mu       sync.Mutex
	ready    *sync.Cond
	queue    []*command
	done     chan struct{} // closed when the worker exits
	closed   bool
	gen      uint64
	budget   *admissionBudget
	onClosed func(*Session)
}

func newSession(name string) *Session {
	return newManagedSession(name, newBudget(DefaultLimits()), nil)
}

// newSessionState has no worker and is not registered anywhere. Submit uses
// it as a private admission candidate; rejected candidates can be discarded.
func newSessionState(name string, budget *admissionBudget, onClosed func(*Session)) *Session {
	s := &Session{Name: name, status: StatusIdle, done: make(chan struct{}), budget: budget, onClosed: onClosed}
	s.ready = sync.NewCond(&s.mu)
	return s
}

func newManagedSession(name string, budget *admissionBudget, onClosed func(*Session)) *Session {
	s := newSessionState(name, budget, onClosed)
	go s.processQueue()
	return s
}

// processQueue has one worker per name. Submission never blocks the TCP reader
// on a full channel (which would prevent it from observing a disconnect).
func (s *Session) processQueue() {
	defer func() {
		// Process waits and completion callbacks have returned before this handoff.
		// Output quiescence follows the executor's Wait contract (PR5 OutputDone).
		// No session lock is held while the manager releases the name.
		if s.onClosed != nil {
			s.onClosed(s) // atomically publishes Done and releases the name
		} else {
			close(s.done)
		}
	}()
	for {
		s.mu.Lock()
		for len(s.queue) == 0 && !s.closed {
			s.ready.Wait()
		}
		if len(s.queue) == 0 && s.closed {
			s.mu.Unlock()
			return
		}
		job := s.queue[0]
		s.queue[0] = nil
		s.queue = s.queue[1:]
		if len(s.queue) == 0 {
			s.queue = nil
		}
		s.mu.Unlock()
		s.run(job)
	}
}

func (s *Session) finish(job *command, code int) {
	s.finishResult(job, Result{ExitCode: code})
}

func (s *Session) finishResult(job *command, result Result) {
	callback := job.doneCb
	// Drop retained payloads before freeing admission credits, including when
	// a completion callback chains submissions or blocks on external work.
	job.line, job.shell, job.outputCb, job.doneCb = "", "", nil, nil
	if job.admitted {
		s.budget.release(s, job.owner, job.cost)
		job.admitted = false
	}
	job.owner.release(s)
	callback(result)
}

func (s *Session) run(job *command) {
	s.mu.Lock()
	if s.closed || job.gen != s.gen || job.owner.cancelled() {
		s.mu.Unlock()
		s.finish(job, ExitCancelled)
		return
	}
	if job.usePty {
		s.executor = ptyPkg.NewPtyExecutor()
	} else {
		s.executor = ptyPkg.NewPipeExecutor()
	}
	exec := s.executor
	s.active = job
	// Start and Kill share this per-session lock: cleanup cannot kill a
	// not-yet-started executor and then let its process escape afterwards.
	err := exec.Start(job.shell, job.line, job.outputCb)
	if err != nil {
		s.executor, s.active, s.status = nil, nil, StatusIdle
		s.mu.Unlock()
		s.finish(job, -1)
		return
	}
	s.status = StatusRunning
	s.mu.Unlock()

	// Retain the existing queue watchdog and executor output semantics.
	waitCh := make(chan int, 1)
	go func() { code, _ := exec.Wait(); waitCh <- code }()
	timer := time.NewTimer(10 * time.Minute)
	var exitCode int
	select {
	case exitCode = <-waitCh:
	case <-timer.C:
		s.mu.Lock()
		_ = exec.Kill()
		s.mu.Unlock()
		grace := time.NewTimer(10 * time.Second)
		select {
		case exitCode = <-waitCh:
		case <-grace.C:
			exitCode = -1
		}
		grace.Stop()
	}
	timer.Stop()

	s.mu.Lock()
	s.executor, s.active, s.status = nil, nil, StatusIdle
	s.mu.Unlock()
	s.finish(job, exitCode)
}

// Execute is the ownerless API for in-process callers. Explicit Drain still
// affects these commands as well as commands from every connection.
func (s *Session) Execute(shell, cmdLine string, usePty bool, outputCb ptyPkg.OutputCallback, doneCb func(int)) error {
	return s.execute(nil, shell, cmdLine, usePty, "", outputCb, func(r Result) { doneCb(r.ExitCode) })
}

func (s *Session) execute(owner *Owner, shell, cmdLine string, usePty bool, agent string, outputCb ptyPkg.OutputCallback, doneCb func(Result)) error {
	if len(shell) > 4096 {
		err := reject("shell path exceeds 4096 bytes")
		doneCb(Result{ExitCode: ExitRejected, Err: err})
		return err
	}
	if err := s.budget.validate(s.Name, agent, cmdLine); err != nil {
		doneCb(Result{ExitCode: ExitRejected, Err: err})
		return err
	}
	job := &command{owner: owner, shell: shell, line: cmdLine, usePty: usePty, outputCb: outputCb, doneCb: doneCb, cost: len(shell) + len(cmdLine) + len(agent) + len(s.Name)}
	if !owner.acquire(s) {
		doneCb(Result{ExitCode: ExitCancelled})
		return nil
	}
	s.mu.Lock()
	if owner.cancelled() {
		s.mu.Unlock()
		s.finish(job, ExitCancelled)
		return nil
	}
	var err error
	if s.closed {
		err = ErrSessionClosing
	} else {
		err = s.budget.acquire(s, owner, job.cost)
	}
	if err != nil {
		s.mu.Unlock()
		s.finishResult(job, Result{ExitCode: ExitRejected, Err: err})
		return err
	}
	job.admitted = true
	// Do not retain a small substring of an arbitrarily large caller buffer.
	job.line = strings.Clone(cmdLine)
	job.shell = strings.Clone(shell)
	job.gen = s.gen
	if agent != "" {
		s.Agent = strings.Clone(agent)
	}
	s.queue = append(s.queue, job)
	s.ready.Signal()
	s.mu.Unlock()
	return nil
}

func (s *Session) WriteInput(data []byte) error {
	s.mu.Lock()
	exec := s.executor
	s.mu.Unlock()
	if exec == nil {
		return fmt.Errorf("no running process in session %q", s.Name)
	}
	return exec.WriteInput(data)
}

func (s *Session) Resize(cols, rows uint16) error {
	s.mu.Lock()
	exec := s.executor
	s.mu.Unlock()
	if exec == nil {
		return fmt.Errorf("no running process in session %q", s.Name)
	}
	return exec.Resize(cols, rows)
}

// Kill terminates only the currently running process group. Keeping the lock
// through Kill prevents a delayed snapshot from killing a later command.
func (s *Session) Kill() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.executor == nil {
		return nil
	}
	return s.executor.Kill()
}

func (s *Session) drainOwner(owner *Owner) {
	s.mu.Lock()
	var cancelled []*command
	kept := s.queue[:0]
	for _, job := range s.queue {
		if job.owner == owner {
			cancelled = append(cancelled, job)
		} else {
			kept = append(kept, job)
		}
	}
	for i := len(kept); i < len(s.queue); i++ {
		s.queue[i] = nil
	}
	s.queue = kept
	if len(s.queue) == 0 {
		s.queue = nil
	}
	if s.active != nil && s.active.owner == owner {
		_ = s.executor.Kill()
	}
	s.mu.Unlock()
	for _, job := range cancelled {
		s.finish(job, ExitCancelled)
	}
}

// Drain is deliberately global to this session, regardless of owner.
func (s *Session) Drain() int {
	s.mu.Lock()
	s.gen++ // includes a command popped by the worker but not yet started
	cancelled := s.queue
	s.queue = nil
	if s.executor != nil {
		_ = s.executor.Kill()
	}
	s.mu.Unlock()
	for _, job := range cancelled {
		s.finish(job, ExitCancelled)
	}
	return len(cancelled)
}

// Close requests shutdown; it never joins the worker or invokes queued
// callbacks on the caller's stack. Safe inside output/completion callbacks.
// Done is the separate quiescence signal. Do not wait for Done inside a
// callback of this same session: the worker must wait for that callback first.
func (s *Session) Close() { s.requestClose() }

func (s *Session) requestClose() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return false
	}
	s.closed = true
	s.gen++
	if s.executor != nil {
		_ = s.executor.Kill()
	}
	s.ready.Broadcast()
	return true
}

// Done closes only after the worker, its process wait and all completion
// callbacks have returned, and its manager has released the reserved name.
func (s *Session) Done() <-chan struct{} { return s.done }

func (s *Session) Info() Info {
	s.mu.Lock()
	defer s.mu.Unlock()
	status := s.status
	if s.closed {
		status = StatusClosing
	}
	return Info{Name: s.Name, Status: string(status), Agent: s.Agent, Pending: len(s.queue)}
}

// Manager manages named sessions.
type Manager struct {
	sessions map[string]*Session
	mu       sync.RWMutex
	shell    string
	budget   *admissionBudget
}

func NewManager(shell string) *Manager {
	m, _ := NewManagerWithLimits(shell, DefaultLimits())
	return m
}

func NewManagerWithLimits(shell string, limits Limits) (*Manager, error) {
	if err := limits.validate(); err != nil {
		return nil, err
	}
	if len(shell) > 4096 {
		return nil, fmt.Errorf("shell path exceeds 4096 bytes")
	}
	return &Manager{sessions: make(map[string]*Session), shell: strings.Clone(shell), budget: newBudget(limits)}, nil
}

// GetOrCreate returns nil on name/worker-limit rejection. New code should use
// GetOrCreateChecked to obtain the precise admission error.
func (m *Manager) GetOrCreate(name string) *Session {
	s, _ := m.GetOrCreateChecked(name)
	return s
}

func (m *Manager) GetOrCreateChecked(name string) (*Session, error) {
	if err := m.budget.validate(name, "", ""); err != nil {
		return nil, err
	}
	m.mu.Lock()
	if s, ok := m.sessions[name]; ok {
		m.mu.Unlock()
		s.mu.Lock()
		closing := s.closed
		s.mu.Unlock()
		if closing {
			return nil, ErrSessionClosing
		}
		return s, nil
	}
	defer m.mu.Unlock()
	if len(m.sessions) >= m.budget.limits.Sessions {
		return nil, reject(fmt.Sprintf("manager named-session limit %d reached", m.budget.limits.Sessions))
	}
	name = strings.Clone(name)
	s := newManagedSession(name, m.budget, m.releaseClosedSession)
	m.sessions[name] = s
	return s, nil
}

func (m *Manager) Get(name string) *Session {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.sessions[name]
}

func (m *Manager) Execute(sessionName, cmdLine string, usePty bool, agent string, outputCb ptyPkg.OutputCallback, doneCb func(int)) error {
	return m.ExecuteOwned(nil, sessionName, cmdLine, usePty, agent, outputCb, doneCb)
}

// ExecuteOwned keeps the legacy exit-code callback and returns admission errors.
// Exactly one callback is delivered even when the returned error is non-nil.
func (m *Manager) ExecuteOwned(owner *Owner, sessionName, cmdLine string, usePty bool, agent string, outputCb ptyPkg.OutputCallback, doneCb func(int)) error {
	return m.Submit(owner, sessionName, cmdLine, usePty, agent, outputCb, func(r Result) { doneCb(r.ExitCode) })
}

// Submit is nonblocking admission. Result.Err supplies a deterministic rejection
// reason for wire/API users; accepted jobs complete asynchronously as before.
func (m *Manager) Submit(owner *Owner, sessionName, cmdLine string, usePty bool, agent string, outputCb ptyPkg.OutputCallback, doneCb func(Result)) error {
	if owner.cancelled() {
		doneCb(Result{ExitCode: ExitCancelled})
		return nil
	}
	if err := m.budget.validate(sessionName, agent, cmdLine); err != nil {
		doneCb(Result{ExitCode: ExitRejected, Err: err})
		return err
	}
	// Existing sessions are never rolled back: they can contain other work or
	// a closing reservation. Their per-session path performs final admission.
	m.mu.Lock()
	if s := m.sessions[sessionName]; s != nil {
		m.mu.Unlock()
		return s.execute(owner, m.shell, cmdLine, usePty, agent, outputCb, doneCb)
	}
	if len(m.sessions) >= m.budget.limits.Sessions {
		m.mu.Unlock()
		err := reject(fmt.Sprintf("manager named-session limit %d reached", m.budget.limits.Sessions))
		doneCb(Result{ExitCode: ExitRejected, Err: err})
		return err
	}

	// New-name transaction: registry -> owner -> budget. Owner.Close drops
	// its lock before touching sessions; finish drops budget before owner.
	// No reverse lock dependency, process start, or callback runs here.
	if owner != nil {
		owner.mu.Lock()
	}
	if owner.cancelled() {
		if owner != nil {
			owner.mu.Unlock()
		}
		m.mu.Unlock()
		doneCb(Result{ExitCode: ExitCancelled})
		return nil
	}
	name := strings.Clone(sessionName)
	s := newSessionState(name, m.budget, m.releaseClosedSession)
	cost := len(m.shell) + len(cmdLine) + len(agent) + len(name)
	if err := m.budget.acquire(s, owner, cost); err != nil {
		// Nothing was published, registered, or started. acquire either
		// reserves every counter atomically or changes no budget at all.
		if owner != nil {
			owner.mu.Unlock()
		}
		m.mu.Unlock()
		doneCb(Result{ExitCode: ExitRejected, Err: err})
		return err
	}
	job := &command{owner: owner, shell: strings.Clone(m.shell), line: strings.Clone(cmdLine), usePty: usePty, outputCb: outputCb, doneCb: doneCb, cost: cost, admitted: true}
	s.Agent = strings.Clone(agent)
	s.queue = []*command{job}
	if owner != nil {
		owner.sessions[s]++
	}
	m.sessions[name] = s
	if owner != nil {
		owner.mu.Unlock()
	}
	m.mu.Unlock()
	// Publication is complete before any execution/callback. A racing Close
	// or Owner.Close can cancel the preloaded queue even before this starts.
	go s.processQueue()
	return nil
}

// Limits returns a copy of the immutable admission policy.
func (m *Manager) Limits() Limits { return m.budget.limits }

func (m *Manager) WriteInput(sessionName string, data []byte) error {
	s := m.Get(sessionName)
	if s == nil {
		return fmt.Errorf("session %q not found", sessionName)
	}
	return s.WriteInput(data)
}

func (m *Manager) Resize(sessionName string, cols, rows uint16) error {
	s := m.Get(sessionName)
	if s == nil {
		return fmt.Errorf("session %q not found", sessionName)
	}
	return s.Resize(cols, rows)
}

func (m *Manager) Kill(sessionName string) bool {
	s := m.Get(sessionName)
	if s == nil {
		return false
	}
	_ = s.Kill()
	return true
}

func (m *Manager) Drain(sessionName string) int {
	s := m.Get(sessionName)
	if s == nil {
		return -1
	}
	return s.Drain()
}

// Close accepts a shutdown request, not a synchronous join. The actual
// Session remains reserved/closing until its Done signal; submissions to that
// name are rejected (-3, ErrSessionClosing), never redirected to a new worker.
func (m *Manager) Close(sessionName string) bool {
	s := m.Get(sessionName)
	return s != nil && s.requestClose()
}

func (m *Manager) releaseClosedSession(s *Session) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.sessions[s.Name] == s {
		delete(m.sessions, s.Name)
	}
	close(s.done) // no same-name GetOrCreate can interleave before Done
}

func (m *Manager) List() []Info {
	m.mu.RLock()
	snapshot := make([]*Session, 0, len(m.sessions))
	for _, s := range m.sessions {
		snapshot = append(snapshot, s)
	}
	m.mu.RUnlock()
	// Never hold the manager lock while waiting on a busy session's lock.
	infos := make([]Info, 0, len(snapshot))
	for _, s := range snapshot {
		infos = append(infos, s.Info())
	}
	return infos
}

func (m *Manager) Shell() string { return m.shell }
