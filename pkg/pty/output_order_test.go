package pty

import (
	"sync"
	"testing"
	"time"
)

func TestPipeExecutorWaitsForOutputCallback(t *testing.T) {
	executor := NewPipeExecutor()
	callbackStarted := make(chan struct{})
	release := make(chan struct{})
	var startedOnce, releaseOnce sync.Once
	releaseOutput := func() { releaseOnce.Do(func() { close(release) }) }
	defer releaseOutput()
	defer executor.Kill()

	if err := executor.Start("/bin/sh", "printf final-output", func(data []byte) {
		startedOnce.Do(func() { close(callbackStarted) })
		<-release
	}); err != nil {
		t.Fatal(err)
	}
	waited := make(chan int, 1)
	go func() {
		code, _ := executor.Wait()
		waited <- code
	}()
	select {
	case <-callbackStarted:
	case <-time.After(3 * time.Second):
		t.Fatal("output callback did not start")
	}
	select {
	case <-waited:
		t.Fatal("Wait returned before the output callback finished")
	case <-time.After(50 * time.Millisecond):
	}
	releaseOutput()
	select {
	case code := <-waited:
		if code != 0 {
			t.Fatalf("exit code = %d, want 0", code)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("Wait did not finish after output callback was released")
	}
}
