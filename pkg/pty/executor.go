package pty

import (
	"io"
	"os"
	"os/exec"
	"sync"
	"syscall"
	"time"

	"github.com/creack/pty"
)

// OutputCallback is called with chunks of output from the process.
type OutputCallback func(data []byte)

// Executor runs commands and streams output.
type Executor interface {
	Start(shell string, cmdLine string, cb OutputCallback) error
	WriteInput(data []byte) error
	Resize(cols, rows uint16) error
	Kill() error
	Wait() (int, error)
}

// PtyExecutor spawns commands in a PTY.
type PtyExecutor struct {
	cmd *exec.Cmd
	ptm *os.File
	mu  sync.Mutex
}

func NewPtyExecutor() *PtyExecutor {
	return &PtyExecutor{}
}

func (e *PtyExecutor) Start(shell string, cmdLine string, cb OutputCallback) error {
	e.mu.Lock()
	defer e.mu.Unlock()

	e.cmd = exec.Command(shell, "-c", cmdLine)
	e.cmd.Env = append(os.Environ(), "TERM=xterm-256color")

	ptm, err := pty.Start(e.cmd)
	if err != nil {
		return err
	}
	e.ptm = ptm

	// Stream output in background
	go func() {
		buf := make([]byte, 4096)
		for {
			n, err := ptm.Read(buf)
			if n > 0 {
				chunk := make([]byte, n)
				copy(chunk, buf[:n])
				cb(chunk)
			}
			if err != nil {
				break
			}
		}
	}()

	return nil
}

func (e *PtyExecutor) WriteInput(data []byte) error {
	e.mu.Lock()
	ptm := e.ptm
	e.mu.Unlock()

	if ptm == nil {
		return io.ErrClosedPipe
	}
	_, err := ptm.Write(data)
	return err
}

func (e *PtyExecutor) Resize(cols, rows uint16) error {
	e.mu.Lock()
	ptm := e.ptm
	e.mu.Unlock()

	if ptm == nil {
		return io.ErrClosedPipe
	}
	return pty.Setsize(ptm, &pty.Winsize{Cols: cols, Rows: rows})
}

func (e *PtyExecutor) Kill() error {
	e.mu.Lock()
	cmd := e.cmd
	e.mu.Unlock()

	if cmd == nil || cmd.Process == nil {
		return nil
	}
	// pty.Start runs the child via Setsid, so it leads its own process
	// group — kill the group to reap detached grandchildren too.
	_ = syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL)
	return cmd.Process.Signal(syscall.SIGKILL)
}

func (e *PtyExecutor) Wait() (int, error) {
	if e.cmd == nil {
		return -1, nil
	}
	err := e.cmd.Wait()

	e.mu.Lock()
	if e.ptm != nil {
		e.ptm.Close()
		e.ptm = nil
	}
	e.mu.Unlock()

	return exitCode(err), nil
}

// PipeExecutor runs commands without a PTY using os/exec with merged stdout+stderr.
type PipeExecutor struct {
	cmd    *exec.Cmd
	in     io.WriteCloser
	mu     sync.Mutex
	waitCh chan struct{} // closed when cmd.Wait() completes
	result int           // exit code, set before waitCh is closed
}

func NewPipeExecutor() *PipeExecutor {
	return &PipeExecutor{
		waitCh: make(chan struct{}),
	}
}

func (e *PipeExecutor) Start(shell string, cmdLine string, cb OutputCallback) error {
	e.mu.Lock()
	defer e.mu.Unlock()

	e.cmd = exec.Command(shell, "-c", cmdLine)
	// Own process group: lets Kill() take out detached grandchildren
	// (setsid/nohup'd) that would otherwise survive and hold the output
	// pipe open forever.
	e.cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	// THE wedge fix: cmd.Wait() normally blocks until the stdout/stderr
	// copy goroutines hit EOF. A detached grandchild inheriting the pipe
	// prevents EOF permanently, wedging the session queue behind this
	// command for good (the daemon then ignores every later shell request
	// while status/screenshot still work). WaitDelay bounds that wait:
	// once the direct child exits, the pipes are force-closed after 5s.
	e.cmd.WaitDelay = 5 * time.Second

	stdin, err := e.cmd.StdinPipe()
	if err != nil {
		return err
	}
	e.in = stdin

	// Merge stdout and stderr into a single pipe
	pr, pw := io.Pipe()
	e.cmd.Stdout = pw
	e.cmd.Stderr = pw

	if err := e.cmd.Start(); err != nil {
		return err
	}

	// Wait() must not return before the last output callback finishes. Otherwise
	// the server can send done=true ahead of stdout and clients lose that output.
	outputDone := make(chan struct{})
	go func() {
		err := e.cmd.Wait()
		e.result = exitCode(err)
		pw.Close()
		<-outputDone
		close(e.waitCh)
	}()

	// Stream output
	go func() {
		defer close(outputDone)
		defer pr.Close()
		buf := make([]byte, 4096)
		for {
			n, err := pr.Read(buf)
			if n > 0 {
				chunk := make([]byte, n)
				copy(chunk, buf[:n])
				cb(chunk)
			}
			if err != nil {
				break
			}
		}
	}()

	return nil
}

func (e *PipeExecutor) WriteInput(data []byte) error {
	e.mu.Lock()
	in := e.in
	e.mu.Unlock()

	if in == nil {
		return io.ErrClosedPipe
	}
	_, err := in.Write(data)
	return err
}

func (e *PipeExecutor) Resize(cols, rows uint16) error {
	// No PTY to resize
	return nil
}

func (e *PipeExecutor) Kill() error {
	e.mu.Lock()
	cmd := e.cmd
	e.mu.Unlock()

	if cmd == nil || cmd.Process == nil {
		return nil
	}
	// Kill the whole process group (Setpgid in Start makes the shell the
	// group leader), then the direct child as belt-and-braces.
	_ = syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL)
	return cmd.Process.Signal(syscall.SIGKILL)
}

func (e *PipeExecutor) Wait() (int, error) {
	if e.cmd == nil {
		return -1, nil
	}
	<-e.waitCh
	return e.result, nil
}

func exitCode(err error) int {
	if err == nil {
		return 0
	}
	if exitErr, ok := err.(*exec.ExitError); ok {
		return exitErr.ExitCode()
	}
	return -1
}
