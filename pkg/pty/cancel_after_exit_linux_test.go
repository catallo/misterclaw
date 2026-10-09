//go:build linux

package pty

import (
	"bufio"
	"errors"
	"fmt"
	"os"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"
)

func TestPtyDrainKillCancelsDescendantAfterDirectExit(t *testing.T) {
	e := NewPtyExecutor()
	e.drainGrace = 300 * time.Millisecond
	reader, writer, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	defer reader.Close()
	defer writer.Close()
	if err := reader.SetReadDeadline(time.Now().Add(3 * time.Second)); err != nil {
		t.Fatal(err)
	}
	// The descendant ignores terminal hangup and identifies itself through a
	// separate pipe. It remains in the shell's own process group.
	ack := fmt.Sprintf("/proc/%d/fd/%d", os.Getpid(), writer.Fd())
	command := fmt.Sprintf("trap '' HUP; /bin/sh -c 'trap \"\" HUP; printf \"%%s\\n\" \"$$\" > \"$1\"; while :; do sleep 30; done' holder '%s' & printf 'READY\\n'; IFS= read -r token", ack)
	ready := make(chan struct{})
	var once sync.Once
	var output ptyRegressionOutput
	if err := e.Start("/bin/sh", command, func(data []byte) {
		output.append(data)
		once.Do(func() { close(ready) })
	}); err != nil {
		t.Fatal(err)
	}
	// Cleanup only this fixture's private group, even when an assertion fails.
	defer func() {
		_ = syscall.Kill(-e.pid, syscall.SIGKILL)
		_, _ = e.Wait()
	}()
	ptyRegressionAwait(t, ready, "parent handshake")
	line, err := bufio.NewReader(reader).ReadString('\n')
	if err != nil {
		t.Fatal(err)
	}
	pid, err := strconv.Atoi(strings.TrimSpace(line))
	if err != nil || pid <= 1 {
		t.Fatalf("invalid descendant acknowledgement: %q (%v)", line, err)
	}
	if err := e.WriteInput([]byte("go\n")); err != nil {
		t.Fatal(err)
	}
	ptyRegressionAwait(t, e.childDone, "direct child exit")
	select {
	case <-e.waitCh:
		t.Fatal("retained terminal completed before cancellation")
	default:
	}
	if err := e.Kill(); err != nil {
		t.Fatalf("cancellation during drain: %v", err)
	}
	result := ptyRegressionReceive(t, ptyRegressionWait(e))
	if result.code != 0 || result.err != nil {
		t.Fatalf("cancellation did not finish output drain: (%d, %v)", result.code, result.err)
	}
	if !strings.Contains(output.snapshot(), "READY") {
		t.Fatalf("lost output before cancellation: %q", output.snapshot())
	}
	// A killed orphan can temporarily be a zombie before init reaps it;
	// either absence or zombie state means it is no longer running.
	deadline := time.Now().Add(3 * time.Second)
	for {
		status, err := os.ReadFile(fmt.Sprintf("/proc/%d/stat", pid))
		if os.IsNotExist(err) || errors.Is(err, syscall.ESRCH) {
			break
		}
		if err != nil {
			t.Fatal(err)
		}
		end := strings.LastIndexByte(string(status), ')')
		if end >= 0 && len(status) > end+2 && status[end+2] == 'Z' {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("same-group descendant still running after cancellation: %s", status)
		}
		time.Sleep(time.Millisecond)
	}
}
