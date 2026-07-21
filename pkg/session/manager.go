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

// ExitCancelled is the exit code reported for a queued command that was
// cancelled by Drain (client disconnect or session-kill) before it ran.
const ExitCancelled = -2

// Info holds metadata about a session for list responses.
type Info struct {
	Name    string `json:"name"`
	Status  string `json:"status"`
	Agent   string `json:"agent,omitempty"`
	Pending int    `json:"pending"` // commands queued but not yet started
}

// Session represents a named execution context with sequential command execution.
type Session struct {
	Name     string
	Agent    string
	status   Status
	executor ptyPkg.Executor
	mu       sync.Mutex
	cmdQueue chan func()
	done     chan struct{}
	gen      uint64 // bumped by Drain; commands queued before the bump self-cancel
}

func newSession(name string) *Session {
	s := &Session{
		Name:     name,
		status:   StatusIdle,
		cmdQueue: make(chan func(), 64),
		done:     make(chan struct{}),
	}
	go s.processQueue()
	return s
}

// processQueue ensures commands run sequentially within a session.
func (s *Session) processQueue() {
	for {
		select {
		case fn := <-s.cmdQueue:
			fn()
		case <-s.done:
			return
		}
	}
}

// Execute runs a command in this session. Commands are queued and executed sequentially.
// The callback is called with output chunks. Returns exit code via the done callback.
func (s *Session) Execute(shell, cmdLine string, usePty bool, outputCb ptyPkg.OutputCallback, doneCb func(int)) {
	s.mu.Lock()
	myGen := s.gen
	s.mu.Unlock()

	s.cmdQueue <- func() {
		s.mu.Lock()
		if myGen != s.gen {
			// Queued before a Drain (client disconnect or session-kill):
			// cancel instead of running. This is the abandoned-queue fix
			// (task #18) — a backed-up queue of run_scene commands would
			// otherwise keep blanket-killing the user's live hack long
			// after the client that queued them had gone.
			s.mu.Unlock()
			doneCb(ExitCancelled)
			return
		}
		s.status = StatusRunning
		if usePty {
			s.executor = ptyPkg.NewPtyExecutor()
		} else {
			s.executor = ptyPkg.NewPipeExecutor()
		}
		exec := s.executor
		s.mu.Unlock()

		err := exec.Start(shell, cmdLine, outputCb)
		if err != nil {
			s.mu.Lock()
			s.status = StatusIdle
			s.executor = nil
			s.mu.Unlock()
			doneCb(-1)
			return
		}

		// Queue watchdog: the session queue is strictly sequential, so a
		// command whose Wait() never returns would wedge this session's
		// shell forever (observed in the field: killed clients + detached
		// grandchildren). Hard-cap a single command at 10 minutes, then
		// group-kill and move on; if Wait() still doesn't return within
		// grace, abandon it (leak one goroutine, keep the queue alive).
		waitCh := make(chan int, 1)
		go func() { code, _ := exec.Wait(); waitCh <- code }()
		var exitCode int
		select {
		case exitCode = <-waitCh:
		case <-time.After(10 * time.Minute):
			_ = exec.Kill()
			select {
			case exitCode = <-waitCh:
			case <-time.After(10 * time.Second):
				exitCode = -1
			}
		}

		s.mu.Lock()
		s.status = StatusIdle
		s.executor = nil
		s.mu.Unlock()

		doneCb(exitCode)
	}
}

// WriteInput sends input to the running process.
func (s *Session) WriteInput(data []byte) error {
	s.mu.Lock()
	exec := s.executor
	s.mu.Unlock()

	if exec == nil {
		return fmt.Errorf("no running process in session %q", s.Name)
	}
	return exec.WriteInput(data)
}

// Resize changes the PTY dimensions.
func (s *Session) Resize(cols, rows uint16) error {
	s.mu.Lock()
	exec := s.executor
	s.mu.Unlock()

	if exec == nil {
		return fmt.Errorf("no running process in session %q", s.Name)
	}
	return exec.Resize(cols, rows)
}

// Kill sends SIGKILL to the running process.
func (s *Session) Kill() error {
	s.mu.Lock()
	exec := s.executor
	s.mu.Unlock()

	if exec == nil {
		return nil
	}
	return exec.Kill()
}

// Drain cancels every not-yet-started command in this session and kills the
// one currently running (task #18 abandoned-queue hazard). Bumping the
// generation makes each queued command self-cancel via the check in Execute
// (returning ExitCancelled the moment processQueue reaches it), so a backed-up
// queue is neutralised without draining the channel by hand; killing the
// running executor stops a long command (e.g. a golden run_scene mid-render)
// immediately. Returns the number of commands that were still queued.
func (s *Session) Drain() int {
	s.mu.Lock()
	s.gen++
	exec := s.executor
	pending := len(s.cmdQueue)
	s.mu.Unlock()

	if exec != nil {
		_ = exec.Kill()
	}
	return pending
}

// Close shuts down the session entirely.
func (s *Session) Close() {
	s.Kill()
	close(s.done)
}

// Info returns metadata about this session.
func (s *Session) Info() Info {
	s.mu.Lock()
	defer s.mu.Unlock()
	return Info{
		Name:    s.Name,
		Status:  string(s.status),
		Agent:   s.Agent,
		Pending: len(s.cmdQueue),
	}
}

// Manager manages named sessions.
type Manager struct {
	sessions map[string]*Session
	mu       sync.RWMutex
	shell    string
}

func NewManager(shell string) *Manager {
	return &Manager{
		sessions: make(map[string]*Session),
		shell:    shell,
	}
}

// GetOrCreate returns an existing session or creates a new one.
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

// Get returns a session by name, or nil if not found.
func (m *Manager) Get(name string) *Session {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.sessions[name]
}

// Execute runs a command in the named session.
func (m *Manager) Execute(sessionName, cmdLine string, usePty bool, agent string, outputCb ptyPkg.OutputCallback, doneCb func(int)) {
	s := m.GetOrCreate(sessionName)
	if agent != "" {
		s.mu.Lock()
		s.Agent = agent
		s.mu.Unlock()
	}
	s.Execute(m.shell, cmdLine, usePty, outputCb, doneCb)
}

// WriteInput sends input to a session's running process.
func (m *Manager) WriteInput(sessionName string, data []byte) error {
	s := m.Get(sessionName)
	if s == nil {
		return fmt.Errorf("session %q not found", sessionName)
	}
	return s.WriteInput(data)
}

// Resize changes the PTY dimensions for a session.
func (m *Manager) Resize(sessionName string, cols, rows uint16) error {
	s := m.Get(sessionName)
	if s == nil {
		return fmt.Errorf("session %q not found", sessionName)
	}
	return s.Resize(cols, rows)
}

// Kill terminates the running process in a session.
func (m *Manager) Kill(sessionName string) bool {
	s := m.Get(sessionName)
	if s == nil {
		return false
	}
	s.Kill()
	return true
}

// Drain cancels a session's queued commands and kills its running one.
// Returns the number of commands that were still queued, or -1 if the
// session does not exist.
func (m *Manager) Drain(sessionName string) int {
	s := m.Get(sessionName)
	if s == nil {
		return -1
	}
	return s.Drain()
}

// Close shuts down a session and removes it.
func (m *Manager) Close(sessionName string) bool {
	m.mu.Lock()
	defer m.mu.Unlock()

	s, ok := m.sessions[sessionName]
	if !ok {
		return false
	}
	s.Close()
	delete(m.sessions, sessionName)
	return true
}

// List returns info about all sessions.
func (m *Manager) List() []Info {
	m.mu.RLock()
	defer m.mu.RUnlock()

	infos := make([]Info, 0, len(m.sessions))
	for _, s := range m.sessions {
		infos = append(infos, s.Info())
	}
	return infos
}

// Shell returns the configured shell.
func (m *Manager) Shell() string {
	return m.shell
}
