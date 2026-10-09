//go:build linux

package pty

import (
	"errors"
	"fmt"
	"io"
	"os"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"
	"unsafe"
)

// These are observation/watchdog deadlines, never output-flush sleeps. Only the
// inherited-slave fixture shortens the configurable post-child I/O drain grace.
const (
	ptyRegressionWatchdog  = 8 * time.Second
	ptyCallbackObservation = 100 * time.Millisecond
)

type ptyRegressionResult struct {
	code int
	err  error
}

type ptyRegressionOutput struct {
	mu   sync.Mutex
	text strings.Builder
}

func (o *ptyRegressionOutput) append(data []byte) {
	o.mu.Lock()
	defer o.mu.Unlock()
	o.text.Write(data)
}

func (o *ptyRegressionOutput) snapshot() string {
	o.mu.Lock()
	defer o.mu.Unlock()
	return o.text.String()
}

func ptyRegressionWait(e *PtyExecutor) <-chan ptyRegressionResult {
	result := make(chan ptyRegressionResult, 1)
	go func() {
		code, err := e.Wait()
		result <- ptyRegressionResult{code, err}
	}()
	return result
}

func ptyRegressionReceive(t *testing.T, result <-chan ptyRegressionResult) ptyRegressionResult {
	t.Helper()
	select {
	case got := <-result:
		return got
	case <-time.After(ptyRegressionWatchdog):
		t.Fatal("Wait did not complete within the watchdog deadline")
		return ptyRegressionResult{}
	}
}

func ptyRegressionAwait(t *testing.T, event <-chan struct{}, description string) {
	t.Helper()
	select {
	case <-event:
	case <-time.After(ptyRegressionWatchdog):
		t.Fatalf("timed out waiting for %s", description)
	}
}

func ptyRegressionRelease(t *testing.T) (<-chan struct{}, func()) {
	t.Helper()
	release := make(chan struct{})
	var once sync.Once
	unblock := func() { once.Do(func() { close(release) }) }
	t.Cleanup(unblock)
	return release, unblock
}

func ptyRegressionRequireResult(t *testing.T, got ptyRegressionResult, code int) {
	t.Helper()
	if got.code != code || got.err != nil {
		t.Errorf("Wait = (%d, %v), want (%d, nil)", got.code, got.err, code)
	}
}

// A genuine synchronous consumer is held inside its first callback while the
// child writes its tail. An independent local pipe acknowledges that the tail
// has been written; it never reads or interferes with the PTY output stream.
func TestPtyDrainQueuedTailAndCallbackBarrier(t *testing.T) {
	e := NewPtyExecutor()
	t.Cleanup(func() { _ = e.Kill() })
	release, unblock := ptyRegressionRelease(t)
	entered := make(chan struct{})
	var first sync.Once
	var output ptyRegressionOutput
	ackRead, ackWrite, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = ackRead.Close(); _ = ackWrite.Close() })
	ackPath := fmt.Sprintf("/proc/%d/fd/%d", os.Getpid(), ackWrite.Fd())
	command := fmt.Sprintf("printf 'FIRST\\n'; IFS= read -r token; printf 'TAIL_SENTINEL\\n'; printf x > '%s'", ackPath)
	if err := e.Start("/bin/sh", command, func(data []byte) {
		output.append(data)
		first.Do(func() { close(entered); <-release })
	}); err != nil {
		t.Fatal(err)
	}
	ptyRegressionAwait(t, entered, "the first output callback")
	if err := e.WriteInput([]byte("go\n")); err != nil {
		t.Fatal(err)
	}
	ack := make(chan error, 1)
	go func() {
		var one [1]byte
		_, err := io.ReadFull(ackRead, one[:])
		if err == nil && one[0] != 'x' {
			err = fmt.Errorf("child acknowledgement = %q, want x", one[:])
		}
		ack <- err
	}()
	select {
	case err := <-ack:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(ptyRegressionWatchdog):
		t.Fatal("child did not acknowledge writing the queued tail")
	}
	waited := ptyRegressionWait(e)
	var early *ptyRegressionResult
	select {
	case got := <-waited:
		early = &got
		t.Error("Wait returned while the first output callback was still gated")
	case <-time.After(ptyCallbackObservation):
	}
	unblock()
	var got ptyRegressionResult
	if early != nil {
		got = *early
	} else {
		got = ptyRegressionReceive(t, waited)
	}
	ptyRegressionRequireResult(t, got, 0)
	// Compare at completion itself, with no post-Wait grace or output polling.
	if text := output.snapshot(); text != "FIRST\r\ngo\r\nTAIL_SENTINEL\r\n" {
		t.Errorf("output at Wait completion = %q, want the exact first line, input echo, and tail", text)
	}
}

func TestPtyDrainWaitIncludesCallbackReturn(t *testing.T) {
	e := NewPtyExecutor()
	t.Cleanup(func() { _ = e.Kill() })
	release, unblock := ptyRegressionRelease(t)
	entered, returned := make(chan struct{}), make(chan struct{})
	var first sync.Once
	if err := e.Start("/bin/sh", "printf final-output", func(data []byte) {
		first.Do(func() { close(entered); <-release; close(returned) })
	}); err != nil {
		t.Fatal(err)
	}
	ptyRegressionAwait(t, entered, "the gated final callback")
	waited := ptyRegressionWait(e)
	var early *ptyRegressionResult
	select {
	case got := <-waited:
		early = &got
		t.Error("Wait returned before the synchronous callback returned")
	case <-time.After(ptyCallbackObservation):
	}
	unblock()
	if early == nil {
		got := ptyRegressionReceive(t, waited)
		ptyRegressionRequireResult(t, got, 0)
		select {
		case <-returned:
		default:
			t.Error("callback-return signal was absent at Wait completion")
		}
	} else {
		ptyRegressionRequireResult(t, *early, 0)
	}
	ptyRegressionAwait(t, returned, "callback release")
}

// Immediate Wait must see the entire stream. Repetition exercises scheduling,
// but the deterministic gated tests above are the negative-control oracle.
func TestPtyDrainImmediateOutputBeforeWait(t *testing.T) {
	for i := 0; i < 32; i++ {
		t.Run(fmt.Sprintf("iteration-%02d", i), func(t *testing.T) {
			e := NewPtyExecutor()
			t.Cleanup(func() { _ = e.Kill() })
			var output ptyRegressionOutput
			if err := e.Start("/bin/sh", "printf 'hello\\n'", output.append); err != nil {
				t.Fatal(err)
			}
			got := ptyRegressionReceive(t, ptyRegressionWait(e))
			ptyRegressionRequireResult(t, got, 0)
			if text := output.snapshot(); text != "hello\r\n" {
				t.Errorf("output at Wait completion = %q, want %q", text, "hello\r\n")
			}
		})
	}
}

func TestPtyDrainNaturalEOFOrEIO(t *testing.T) {
	for _, tc := range []struct {
		name, command, want string
		code                int
	}{
		{"empty", "exit 0", "", 0},
		{"stdout-and-stderr", "printf 'out\\n'; printf 'err\\n' >&2", "out\r\nerr\r\n", 0},
		{"nonzero", "printf 'last\\n'; exit 42", "last\r\n", 42},
	} {
		t.Run(tc.name, func(t *testing.T) {
			e := NewPtyExecutor()
			t.Cleanup(func() { _ = e.Kill() })
			var output ptyRegressionOutput
			if err := e.Start("/bin/sh", tc.command, output.append); err != nil {
				t.Fatal(err)
			}
			got := ptyRegressionReceive(t, ptyRegressionWait(e))
			ptyRegressionRequireResult(t, got, tc.code)
			if text := output.snapshot(); text != tc.want {
				t.Errorf("natural terminal completion output = %q, want %q", text, tc.want)
			}
		})
	}
}

// Open an additional slave while the child is held at its input handshake. A
// live slave deterministically suppresses EOF/EIO after the direct child exits,
// without spawning an uncontrolled descendant or changing production behavior.
func ptyRegressionRetainSlave(t *testing.T, e *PtyExecutor) *os.File {
	t.Helper()
	e.mu.Lock()
	master := e.ptm
	e.mu.Unlock()
	if master == nil {
		t.Fatal("master closed before the child input handshake")
	}
	raw, err := master.SyscallConn()
	if err != nil {
		t.Fatal(err)
	}
	var number uint32
	var ioctlErr syscall.Errno
	if err := raw.Control(func(fd uintptr) {
		_, _, ioctlErr = syscall.Syscall(syscall.SYS_IOCTL, fd, syscall.TIOCGPTN, uintptr(unsafe.Pointer(&number)))
	}); err != nil {
		t.Fatal(err)
	}
	if ioctlErr != 0 {
		t.Fatal(ioctlErr)
	}
	slave, err := os.OpenFile(fmt.Sprintf("/dev/pts/%d", number), os.O_RDWR|syscall.O_NOCTTY, 0)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = slave.Close() })
	return slave
}

func TestPtyDrainInheritedSlave(t *testing.T) {
	for _, retain := range []bool{false, true} {
		name := "last-slave-closes-naturally"
		if retain {
			name = "retained-slave-times-out-explicitly"
		}
		t.Run(name, func(t *testing.T) {
			e := NewPtyExecutor()
			e.drainGrace = 200 * time.Millisecond
			t.Cleanup(func() { _ = e.Kill() })
			ready := make(chan struct{})
			var first sync.Once
			var output ptyRegressionOutput
			if err := e.Start("/bin/sh", "printf 'ready\\n'; IFS= read -r token; printf 'tail\\n'", func(data []byte) {
				output.append(data)
				first.Do(func() { close(ready) })
			}); err != nil {
				t.Fatal(err)
			}
			ptyRegressionAwait(t, ready, "the child input handshake")
			slave := ptyRegressionRetainSlave(t, e)
			// Resize must not revert the master to a non-interruptible read.
			if err := e.Resize(100, 40); err != nil {
				t.Fatal(err)
			}
			if !retain {
				if err := slave.Close(); err != nil {
					t.Fatal(err)
				}
			}
			if err := e.WriteInput([]byte("go\n")); err != nil {
				t.Fatal(err)
			}
			got := ptyRegressionReceive(t, ptyRegressionWait(e))
			if retain {
				if got.code != -1 || !errors.Is(got.err, ErrPTYDrainTimeout) {
					t.Errorf("retained-slave Wait = (%d, %v), want (-1, ErrPTYDrainTimeout)", got.code, got.err)
				}
			} else {
				ptyRegressionRequireResult(t, got, 0)
			}
			if text := output.snapshot(); text != "ready\r\ngo\r\ntail\r\n" {
				t.Errorf("output at Wait completion = %q, want complete child output", text)
			}
			if retain {
				// Publishing a terminal error must be as repeatable as success,
				// even after the descriptor that caused it has finally closed.
				if err := slave.Close(); err != nil {
					t.Fatal(err)
				}
				for i := 0; i < 3; i++ {
					again := ptyRegressionReceive(t, ptyRegressionWait(e))
					if again.code != -1 || !errors.Is(again.err, ErrPTYDrainTimeout) {
						t.Errorf("repeated timeout Wait = (%d, %v), want stored (-1, ErrPTYDrainTimeout)", again.code, again.err)
					}
				}
			}
		})
	}
}

// Callbacks may request Kill, but must not join the same executor's Wait. Kill
// must remain request-only even when Wait itself includes callback completion.
func TestPtyDrainCallbackKillDoesNotJoin(t *testing.T) {
	e := NewPtyExecutor()
	t.Cleanup(func() { _ = e.Kill() })
	release, unblock := ptyRegressionRelease(t)
	killReturned := make(chan error, 1)
	var first sync.Once
	if err := e.Start("/bin/sh", "printf 'ready\\n'; IFS= read -r token", func(data []byte) {
		first.Do(func() {
			killReturned <- e.Kill()
			<-release
		})
	}); err != nil {
		t.Fatal(err)
	}
	select {
	case err := <-killReturned:
		if err != nil {
			t.Errorf("callback Kill: %v", err)
		}
	case <-time.After(ptyRegressionWatchdog):
		t.Fatal("Kill joined the callback and deadlocked")
	}
	waited := ptyRegressionWait(e)
	var early *ptyRegressionResult
	select {
	case got := <-waited:
		early = &got
		t.Error("Wait returned before the callback following Kill returned")
	case <-time.After(ptyCallbackObservation):
	}
	unblock()
	if early != nil {
		ptyRegressionRequireResult(t, *early, -1)
	} else {
		ptyRegressionRequireResult(t, ptyRegressionReceive(t, waited), -1)
	}
}

func TestPtyDrainRepeatedWaitReturnsSameResult(t *testing.T) {
	e := NewPtyExecutor()
	t.Cleanup(func() { _ = e.Kill() })
	if err := e.Start("/bin/sh", "exit 42", func(data []byte) {}); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 4; i++ {
		ptyRegressionRequireResult(t, ptyRegressionReceive(t, ptyRegressionWait(e)), 42)
	}
}

func TestPtyDrainConcurrentWaitReturnsSameResult(t *testing.T) {
	e := NewPtyExecutor()
	t.Cleanup(func() { _ = e.Kill() })
	ready := make(chan struct{})
	var first sync.Once
	if err := e.Start("/bin/sh", "printf ready; IFS= read -r token; exit 42", func(data []byte) {
		first.Do(func() { close(ready) })
	}); err != nil {
		t.Fatal(err)
	}
	ptyRegressionAwait(t, ready, "the child input handshake")
	const callers = 12
	start := make(chan struct{})
	results := make(chan ptyRegressionResult, callers)
	for i := 0; i < callers; i++ {
		go func() {
			<-start
			code, err := e.Wait()
			results <- ptyRegressionResult{code, err}
		}()
	}
	close(start)
	if err := e.WriteInput([]byte("go\n")); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < callers; i++ {
		ptyRegressionRequireResult(t, ptyRegressionReceive(t, results), 42)
	}
}

func TestPtyDrainKillAndCloseRaces(t *testing.T) {
	e := NewPtyExecutor()
	t.Cleanup(func() { _ = e.Kill() })
	release, unblock := ptyRegressionRelease(t)
	entered := make(chan struct{})
	var first sync.Once
	if err := e.Start("/bin/sh", "printf ready; IFS= read -r token", func(data []byte) {
		first.Do(func() { close(entered); <-release })
	}); err != nil {
		t.Fatal(err)
	}
	ptyRegressionAwait(t, entered, "the gated callback")
	const callers = 12
	start := make(chan struct{})
	results := make(chan ptyRegressionResult, callers)
	operationsDone := make(chan struct{})
	var operations sync.WaitGroup
	for i := 0; i < callers; i++ {
		go func() {
			<-start
			code, err := e.Wait()
			results <- ptyRegressionResult{code, err}
		}()
		operations.Add(1)
		go func() {
			defer operations.Done()
			<-start
			// Already-exited/closed errors are valid here; hangs and data races
			// are not. No input newline is sent, so only Kill can end the child.
			_ = e.Resize(80, 24)
			_ = e.WriteInput([]byte("x"))
			_ = e.Kill()
		}()
	}
	go func() { operations.Wait(); close(operationsDone) }()
	close(start)
	ptyRegressionAwait(t, operationsDone, "non-joining cancellation operations")
	var early *ptyRegressionResult
	select {
	case got := <-results:
		early = &got
		t.Error("a racing Wait returned while its output callback was gated")
	case <-time.After(ptyCallbackObservation):
	}
	unblock()
	if early != nil {
		ptyRegressionRequireResult(t, *early, -1)
	}
	remaining := callers
	if early != nil {
		remaining--
	}
	for i := 0; i < remaining; i++ {
		ptyRegressionRequireResult(t, ptyRegressionReceive(t, results), -1)
	}
	ptyRegressionRequireResult(t, ptyRegressionReceive(t, ptyRegressionWait(e)), -1)
}
