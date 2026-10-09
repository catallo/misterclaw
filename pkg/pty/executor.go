package pty

import (
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"sync"
	"syscall"
	"time"
	"unsafe"

	"github.com/creack/pty"
)

// OutputCallback is called with chunks of output from the process.
// Callbacks may request Kill but must not await their own executor completion.
// A synchronously blocked callback cannot be forcibly timed out by the reader.
type OutputCallback func(data []byte)

// Executor runs commands and streams output.
type Executor interface {
	Start(shell string, cmdLine string, cb OutputCallback) error
	WriteInput(data []byte) error
	Resize(cols, rows uint16) error
	Kill() error
	Wait() (int, error)
}

// ErrPTYDrainTimeout reports that a descendant kept the terminal open after
// the direct child exited. Completion must not claim all output was delivered.
var ErrPTYDrainTimeout = errors.New("PTY output drain timed out after child exit; output may be incomplete")

// PtyExecutor spawns commands in a PTY. Output callbacks may request Kill, but
// must not wait on this executor's completion: Wait includes callback return.
type PtyExecutor struct {
	cmd        *exec.Cmd
	pid        int // immutable group ID captured before the reaper can release Process
	ptm        *os.File
	mu         sync.Mutex
	started    bool
	childDone  chan struct{}
	readerDone chan struct{}
	waitCh     chan struct{}
	result     int
	resultErr  error
	drainGrace time.Duration
}

func NewPtyExecutor() *PtyExecutor {
	return &PtyExecutor{drainGrace: 5 * time.Second}
}

// pollablePTY duplicates the master before os.NewFile registers it with the Go
// poller. Changing only the old File's flags can leave a blocking Read that
// deadlines and Close cannot interrupt. The caller relinquishes the old File.
func pollablePTY(master *os.File) (*os.File, error) {
	syscall.ForkLock.RLock()
	fd, err := syscall.Dup(int(master.Fd()))
	if err == nil {
		syscall.CloseOnExec(fd)
	}
	syscall.ForkLock.RUnlock()
	if err != nil {
		return nil, err
	}
	if err := syscall.SetNonblock(fd, true); err != nil {
		_ = syscall.Close(fd)
		return nil, err
	}
	file := os.NewFile(uintptr(fd), master.Name())
	if err := file.SetReadDeadline(time.Time{}); err != nil {
		_ = file.Close()
		return nil, err
	}
	return file, nil
}

func (e *PtyExecutor) Start(shell string, cmdLine string, cb OutputCallback) error {
	e.mu.Lock()
	if e.cmd != nil {
		e.mu.Unlock()
		return errors.New("PTY executor has already been started")
	}
	cmd := exec.Command(shell, "-c", cmdLine)
	cmd.Env = append(os.Environ(), "TERM=xterm-256color")
	e.cmd = cmd
	master, err := pty.Start(cmd)
	if err != nil {
		e.resultErr = err
		e.mu.Unlock()
		return err
	}
	file, err := pollablePTY(master)
	_ = master.Close()
	if err != nil {
		e.resultErr = err
		e.mu.Unlock()
		_ = syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL)
		_ = cmd.Process.Kill()
		_ = cmd.Wait()
		return err
	}
	e.ptm = file
	e.pid = cmd.Process.Pid
	e.started = true
	e.childDone = make(chan struct{})
	e.readerDone = make(chan struct{})
	e.waitCh = make(chan struct{})
	grace := e.drainGrace
	if grace <= 0 {
		grace = 5 * time.Second
	}
	e.mu.Unlock()

	var readErr error // published by readerDone, consumed by the sole reaper
	go func() {
		defer close(e.readerDone)
		buf := make([]byte, 4096)
		for {
			n, err := file.Read(buf)
			if n > 0 {
				chunk := append([]byte(nil), buf[:n]...)
				cb(chunk)
			}
			if err != nil {
				if !errors.Is(err, io.EOF) && !errors.Is(err, syscall.EIO) {
					readErr = err
				}
				return
			}
		}
	}()

	go func() {
		code := exitCode(cmd.Wait())
		close(e.childDone)
		var drainErr error
		select {
		case <-e.readerDone:
		default:
			if err := file.SetReadDeadline(time.Now().Add(grace)); err != nil {
				drainErr = fmt.Errorf("cannot bound PTY output drain: %w", err)
				_ = file.Close()
			}
			<-e.readerDone
		}
		if errors.Is(readErr, os.ErrDeadlineExceeded) {
			drainErr = ErrPTYDrainTimeout
		} else if readErr != nil && drainErr == nil {
			drainErr = fmt.Errorf("PTY output read failed: %w", readErr)
		}
		_ = file.Close()
		if drainErr != nil && code == 0 {
			code = -1
		}
		e.mu.Lock()
		e.ptm = nil
		e.result, e.resultErr = code, drainErr
		e.mu.Unlock()
		close(e.waitCh)
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
	// Do not call pty.Setsize/File.Fd: Fd switches the pollable master back
	// into blocking mode and can defeat the post-child drain deadline.
	raw, err := ptm.SyscallConn()
	if err != nil {
		return err
	}
	size := pty.Winsize{Cols: cols, Rows: rows}
	var ioctlErr syscall.Errno
	if err := raw.Control(func(fd uintptr) {
		_, _, ioctlErr = syscall.Syscall(syscall.SYS_IOCTL, fd, uintptr(syscall.TIOCSWINSZ), uintptr(unsafe.Pointer(&size)))
	}); err != nil {
		return err
	}
	if ioctlErr != 0 {
		return ioctlErr
	}
	return nil
}

// Kill requests cancellation only; it never joins the output callback.
func (e *PtyExecutor) Kill() error {
	e.mu.Lock()
	cmd, pid, started, waitCh := e.cmd, e.pid, e.started, e.waitCh
	e.mu.Unlock()
	if !started || cmd.Process == nil {
		return nil
	}
	select {
	case <-waitCh:
		return nil
	default:
	}
	// Preserve cancellation of same-group descendants during pending drain.
	// Numeric process-group IDs retain the historical identifier-reuse limit;
	// this is a best-effort cancellation request, not strong group ownership.
	groupErr := syscall.Kill(-pid, syscall.SIGKILL)
	directErr := cmd.Process.Signal(syscall.SIGKILL)
	if groupErr == nil {
		return nil
	}
	if errors.Is(directErr, os.ErrProcessDone) {
		if errors.Is(groupErr, syscall.ESRCH) {
			return nil
		}
		return groupErr
	}
	return directErr
}

func (e *PtyExecutor) Wait() (int, error) {
	e.mu.Lock()
	started, waitCh, startErr := e.started, e.waitCh, e.resultErr
	e.mu.Unlock()
	if !started {
		return -1, startErr
	}
	<-waitCh
	e.mu.Lock()
	defer e.mu.Unlock()
	return e.result, e.resultErr
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
