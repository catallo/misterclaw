package mister

import (
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"
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

	// Open the read end FIRST (non-blocking) so a reader is present when
	// writeCmd does its own non-blocking open — this mirrors the MiSTer
	// main process holding /dev/MiSTer_cmd open. Without a reader, writeCmd
	// now (correctly) fails fast with ENXIO; see TestWriteCmd_NoReader.
	rfd, err := os.OpenFile(fifoPath, os.O_RDONLY|syscall.O_NONBLOCK, 0)
	if err != nil {
		t.Fatalf("open read end: %v", err)
	}
	defer rfd.Close()

	if err := LoadCore("/media/fat/_Console/SNES.rbf"); err != nil {
		t.Fatalf("LoadCore: %v", err)
	}

	// Drain the written command from the pipe buffer.
	buf := make([]byte, 256)
	var got string
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if n, _ := rfd.Read(buf); n > 0 {
			got = string(buf[:n])
			break
		}
		time.Sleep(10 * time.Millisecond)
	}

	expected := "load_core /media/fat/_Console/SNES.rbf"
	if !strings.Contains(got, expected) {
		t.Errorf("FIFO received %q, want %q", got, expected)
	}
}

// TestWriteCmd_NoReader verifies the task-#18 fix: a FIFO with no reader
// (MiSTer main dead) fails fast with a clear message instead of blocking
// the daemon forever on open(2).
func TestWriteCmd_NoReader(t *testing.T) {
	tmpDir := t.TempDir()
	fifoPath := tmpDir + "/MiSTer_cmd"
	if err := createFIFO(fifoPath); err != nil {
		t.Skipf("cannot create FIFO: %v", err)
	}
	origPath := cmdPath
	setMisterCmdPath(fifoPath)
	defer setMisterCmdPath(origPath)

	// No reader is open. A blocking open would hang here forever; the fix
	// makes it return promptly.
	done := make(chan error, 1)
	go func() { done <- LoadCore("/media/fat/_Console/SNES.rbf") }()

	select {
	case err := <-done:
		if err == nil {
			t.Fatal("expected an error writing to a reader-less FIFO, got nil")
		}
		if !strings.Contains(err.Error(), "no reader") {
			t.Errorf("expected a 'no reader' diagnostic, got: %v", err)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("writeCmd blocked on a reader-less FIFO — the O_NONBLOCK guard is not working")
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

// TestValidateCorePath covers the loadcore-guard (task #18): reject missing,
// truncated, empty, or wrong-type core files before /dev/MiSTer_cmd.
func TestValidateCorePath(t *testing.T) {
	dir := t.TempDir()

	writeFile := func(name string, size int) string {
		p := filepath.Join(dir, name)
		if err := os.WriteFile(p, make([]byte, size), 0644); err != nil {
			t.Fatalf("write %s: %v", name, err)
		}
		return p
	}

	goodRBF := writeFile("core.rbf", 64*1024)     // exactly the floor
	tinyRBF := writeFile("trunc.rbf", 100)        // truncated
	emptyMGL := writeFile("empty.mgl", 0)         // empty descriptor
	goodMGL := writeFile("game.mgl", 42)          // non-empty descriptor
	txt := writeFile("notacore.txt", 100000)      // wrong extension

	cases := []struct {
		name    string
		path    string
		wantErr bool
	}{
		{"good rbf", goodRBF, false},
		{"truncated rbf", tinyRBF, true},
		{"missing", filepath.Join(dir, "nope.rbf"), true},
		{"empty mgl", emptyMGL, true},
		{"good mgl", goodMGL, false},
		{"wrong ext", txt, true},
		{"directory", dir, true},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			err := ValidateCorePath(c.path)
			if c.wantErr && err == nil {
				t.Errorf("ValidateCorePath(%s): expected error, got nil", c.path)
			}
			if !c.wantErr && err != nil {
				t.Errorf("ValidateCorePath(%s): unexpected error: %v", c.path, err)
			}
		})
	}
}
