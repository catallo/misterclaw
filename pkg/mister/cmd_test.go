package mister

import (
	"os"
	"strings"
	"testing"
)

func TestWriteCmd_MockFIFO(t *testing.T) {
	// Create a temp FIFO to simulate /dev/MiSTer_cmd
	tmpDir := t.TempDir()
	fifoPath := tmpDir + "/MiSTer_cmd"

	// Create a named pipe
	if err := createFIFO(fifoPath); err != nil {
		t.Skipf("cannot create FIFO: %v", err)
	}

	// Override the cmd path for testing
	origPath := cmdPath
	setMisterCmdPath(fifoPath)
	defer setMisterCmdPath(origPath)

	// Read from FIFO in background
	done := make(chan string, 1)
	go func() {
		data, _ := os.ReadFile(fifoPath)
		done <- string(data)
	}()

	err := LoadCore("/media/fat/_Console/SNES.rbf")
	if err != nil {
		t.Fatalf("LoadCore: %v", err)
	}

	got := <-done
	expected := "load_core /media/fat/_Console/SNES.rbf"
	if !strings.Contains(got, expected) {
		t.Errorf("FIFO received %q, want %q", got, expected)
	}
}

func TestWriteCmd_NotOnMiSTer(t *testing.T) {
	// Point to a path that doesn't exist
	origPath := cmdPath
	setMisterCmdPath("/nonexistent/MiSTer_cmd")
	defer setMisterCmdPath(origPath)

	err := LoadCore("test")
	if err == nil {
		t.Error("expected error when MiSTer_cmd doesn't exist")
	}
	if !strings.Contains(err.Error(), "not found") && !strings.Contains(err.Error(), "not running on MiSTer") {
		t.Errorf("unexpected error: %v", err)
	}
}
