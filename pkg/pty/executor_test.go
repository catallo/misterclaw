package pty

import (
	"strings"
	"sync"
	"testing"
	"time"
)

func TestPtyExecutor_EchoHello(t *testing.T) {
	exec := NewPtyExecutor()
	var output strings.Builder
	var mu sync.Mutex
	done := make(chan int, 1)

	err := exec.Start("/bin/sh", "echo hello", func(data []byte) {
		mu.Lock()
		output.Write(data)
		mu.Unlock()
	})
	if err != nil {
		t.Fatalf("Start: %v", err)
	}

	go func() {
		code, _ := exec.Wait()
		done <- code
	}()

	select {
	case code := <-done:
		if code != 0 {
			t.Errorf("exit code = %d, want 0", code)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("timeout waiting for command")
	}

	// Give output goroutine a moment to flush
	time.Sleep(50 * time.Millisecond)
	mu.Lock()
	got := output.String()
	mu.Unlock()
	if !strings.Contains(got, "hello") {
		t.Errorf("output = %q, want it to contain 'hello'", got)
	}
}

func TestPtyExecutor_ExitCode(t *testing.T) {
	exec := NewPtyExecutor()
	err := exec.Start("/bin/sh", "exit 42", func(data []byte) {})
	if err != nil {
		t.Fatalf("Start: %v", err)
	}

	code, _ := exec.Wait()
	if code != 42 {
		t.Errorf("exit code = %d, want 42", code)
	}
}

func TestPipeExecutor_EchoHello(t *testing.T) {
	exec := NewPipeExecutor()
	var output strings.Builder
	var mu sync.Mutex
	done := make(chan int, 1)

	err := exec.Start("/bin/sh", "echo hello", func(data []byte) {
		mu.Lock()
		output.Write(data)
		mu.Unlock()
	})
	if err != nil {
		t.Fatalf("Start: %v", err)
	}

	go func() {
		code, _ := exec.Wait()
		done <- code
	}()

	select {
	case code := <-done:
		if code != 0 {
			t.Errorf("exit code = %d, want 0", code)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("timeout waiting for command")
	}

	time.Sleep(50 * time.Millisecond)
	mu.Lock()
	got := output.String()
	mu.Unlock()
	if !strings.Contains(got, "hello") {
		t.Errorf("output = %q, want it to contain 'hello'", got)
	}
}

func TestPipeExecutor_ExitCode(t *testing.T) {
	exec := NewPipeExecutor()
	err := exec.Start("/bin/sh", "exit 7", func(data []byte) {})
	if err != nil {
		t.Fatalf("Start: %v", err)
	}

	code, _ := exec.Wait()
	if code != 7 {
		t.Errorf("exit code = %d, want 7", code)
	}
}
