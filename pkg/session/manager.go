package session

import (
	"fmt"
	"sync"
	"time"

	ptyPkg "github.com/catallo/misterclaw/pkg/pty"
)

// Status represents the state of a session.
type Status string

const (
	StatusIdle    Status = "idle"
	StatusRunning Status = "running"
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
	doneCb   func(int)
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
}

func newSession(name string) *Session {
	s := &Session{Name: name, status: StatusIdle, done: make(chan struct{})}
	s.ready = sync.NewCond(&s.mu)
	go s.processQueue()
	return s
}

// processQueue has one worker per name. Submission never blocks the TCP reader
// on a full channel (which would prevent it from observing a disconnect).
func (s *Session) processQueue() {
	defer close(s.done)
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
	job.owner.release(s)
	job.doneCb(code)
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
func (s *Session) Execute(shell, cmdLine string, usePty bool, outputCb ptyPkg.OutputCallback, doneCb func(int)) {
	s.execute(nil, shell, cmdLine, usePty, "", outputCb, doneCb)
}

func (s *Session) execute(owner *Owner, shell, cmdLine string, usePty bool, agent string, outputCb ptyPkg.OutputCallback, doneCb func(int)) {
	job := &command{owner: owner, shell: shell, line: cmdLine, usePty: usePty, outputCb: outputCb, doneCb: doneCb}
	if !owner.acquire(s) {
		doneCb(ExitCancelled)
		return
	}
	s.mu.Lock()
	if s.closed || owner.cancelled() {
		s.mu.Unlock()
		s.finish(job, ExitCancelled)
		return
	}
	job.gen = s.gen
	if agent != "" {
		s.Agent = agent
	}
	s.queue = append(s.queue, job)
	s.ready.Signal()
	s.mu.Unlock()
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

// Close stops active/queued work and wakes the idle worker. Completion is
// delivered for every queued command instead of abandoning a closed channel.
func (s *Session) Close() {
	s.mu.Lock()
	s.closed = true
	active := s.active != nil
	s.ready.Broadcast()
	s.mu.Unlock()
	s.Drain()
	// A completion callback runs on this worker after active was cleared.
	// Waiting for the worker there would make callback -> Close deadlock.
	// A closed idle worker cannot start another process; only active work
	// needs to finish before the manager can release the session name.
	if active {
		<-s.done
	}
}

func (s *Session) Info() Info {
	s.mu.Lock()
	defer s.mu.Unlock()
	return Info{Name: s.Name, Status: string(s.status), Agent: s.Agent, Pending: len(s.queue)}
}

// Manager manages named sessions.
type Manager struct {
	sessions map[string]*Session
	mu       sync.RWMutex
	shell    string
}

func NewManager(shell string) *Manager {
	return &Manager{sessions: make(map[string]*Session), shell: shell}
}

func (m *Manager) GetOrCreate(name string) *Session {
	m.mu.Lock()
	defer m.mu.Unlock()
	if s, ok := m.sessions[name]; ok {
		return s
	}
	s := newSession(name)
	m.sessions[name] = s
	return s
}

func (m *Manager) Get(name string) *Session {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.sessions[name]
}

func (m *Manager) Execute(sessionName, cmdLine string, usePty bool, agent string, outputCb ptyPkg.OutputCallback, doneCb func(int)) {
	m.ExecuteOwned(nil, sessionName, cmdLine, usePty, agent, outputCb, doneCb)
}

// ExecuteOwned associates the job with a server-created connection lifetime.
func (m *Manager) ExecuteOwned(owner *Owner, sessionName, cmdLine string, usePty bool, agent string, outputCb ptyPkg.OutputCallback, doneCb func(int)) {
	// A late submission from a dead connection must not create an idle session.
	if owner.cancelled() {
		doneCb(ExitCancelled)
		return
	}
	s := m.GetOrCreate(sessionName)
	s.execute(owner, m.shell, cmdLine, usePty, agent, outputCb, doneCb)
}

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

func (m *Manager) Close(sessionName string) bool {
	s := m.Get(sessionName)
	if s == nil {
		return false
	}
	// Mark the old worker closed and wait out any active process before
	// releasing its name. Do not hold the manager lock while waiting:
	// completion callbacks can call List or close an already-idle session.
	s.Close()
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.sessions[sessionName] != s {
		return false
	}
	delete(m.sessions, sessionName)
	return true
}

func (m *Manager) List() []Info {
	m.mu.RLock()
	defer m.mu.RUnlock()
	infos := make([]Info, 0, len(m.sessions))
	for _, s := range m.sessions {
		infos = append(infos, s.Info())
	}
	return infos
}

func (m *Manager) Shell() string { return m.shell }
