package pty

import (
	"errors"
	"io"
	"testing"
)

func TestPtyStartFailureDoesNotLeaveWaitOrKillBlocked(t *testing.T) {
	executor := NewPtyExecutor()
	if code, err := executor.Wait(); code != -1 || err != nil {
		t.Fatalf("unstarted Wait = (%d, %v)", code, err)
	}
	startErr := executor.Start("/misterclaw-test/nonexistent-shell", "unused", func([]byte) {})
	if startErr == nil {
		t.Fatal("missing executable was accepted")
	}
	for n := 0; n < 3; n++ {
		code, err := executor.Wait()
		if code != -1 || !errors.Is(err, startErr) {
			t.Fatalf("failed-start Wait = (%d, %v), want original start error", code, err)
		}
		if err := executor.Kill(); err != nil {
			t.Fatalf("Kill after failed start: %v", err)
		}
	}
	if err := executor.WriteInput([]byte("unused")); !errors.Is(err, io.ErrClosedPipe) {
		t.Fatalf("failed-start input: %v", err)
	}
	if err := executor.Resize(80, 25); !errors.Is(err, io.ErrClosedPipe) {
		t.Fatalf("failed-start resize: %v", err)
	}
}
