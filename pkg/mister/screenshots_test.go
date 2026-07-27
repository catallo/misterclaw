package mister

import (
	"bytes"
	"encoding/base64"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestWaitForStableFileSize(t *testing.T) {
	origInterval := screenshotStabilizeInterval
	screenshotStabilizeInterval = 10 * time.Millisecond
	t.Cleanup(func() {
		screenshotStabilizeInterval = origInterval
	})

	path := filepath.Join(t.TempDir(), "shot.png")
	if err := os.WriteFile(path, []byte("a"), 0o644); err != nil {
		t.Fatalf("os.WriteFile: %v", err)
	}

	go func() {
		time.Sleep(15 * time.Millisecond)
		_ = os.WriteFile(path, []byte("abcdef"), 0o644)
	}()

	if err := waitForStableFileSize(path, 300*time.Millisecond); err != nil {
		t.Fatalf("waitForStableFileSize: %v", err)
	}

	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("os.Stat: %v", err)
	}
	if info.Size() != 6 {
		t.Fatalf("size = %d, want 6", info.Size())
	}
}

func TestWaitForStableFileSizeTimeout(t *testing.T) {
	origInterval := screenshotStabilizeInterval
	screenshotStabilizeInterval = 10 * time.Millisecond
	t.Cleanup(func() {
		screenshotStabilizeInterval = origInterval
	})

	path := filepath.Join(t.TempDir(), "shot.png")
	if err := os.WriteFile(path, []byte("a"), 0o644); err != nil {
		t.Fatalf("os.WriteFile: %v", err)
	}

	done := make(chan struct{})
	go func() {
		defer close(done)
		for i := 0; i < 8; i++ {
			time.Sleep(8 * time.Millisecond)
			_ = os.WriteFile(path, bytes.Repeat([]byte{'x'}, i+2), 0o644)
		}
	}()

	err := waitForStableFileSize(path, 40*time.Millisecond)
	if err == nil {
		t.Fatal("expected timeout error")
	}
	if !strings.Contains(err.Error(), "did not stabilize") {
		t.Fatalf("unexpected error: %v", err)
	}
	<-done
}

func TestTakeScreenshotAndCaptureWaitsForStableSize(t *testing.T) {
	tmpDir := t.TempDir()
	oldCoreDir := filepath.Join(tmpDir, "oldcore")
	newCoreDir := filepath.Join(tmpDir, "newcore")
	if err := os.MkdirAll(oldCoreDir, 0o755); err != nil {
		t.Fatalf("os.MkdirAll old: %v", err)
	}
	if err := os.MkdirAll(newCoreDir, 0o755); err != nil {
		t.Fatalf("os.MkdirAll new: %v", err)
	}

	oldPath := filepath.Join(oldCoreDir, "old.png")
	if err := os.WriteFile(oldPath, []byte("old"), 0o644); err != nil {
		t.Fatalf("os.WriteFile old: %v", err)
	}
	oldTime := time.Now().Add(-time.Minute)
	if err := os.Chtimes(oldPath, oldTime, oldTime); err != nil {
		t.Fatalf("os.Chtimes old: %v", err)
	}

	fullData := bytes.Repeat([]byte("PNGDATA"), 64)
	newPath := filepath.Join(newCoreDir, "new.png")

	origRoot := screenshotRootDir
	origTrigger := triggerScreenshot
	origPoll := screenshotPollInterval
	origStableInterval := screenshotStabilizeInterval
	origStableTimeout := screenshotStabilizeTimeout
	screenshotRootDir = tmpDir
	screenshotPollInterval = 10 * time.Millisecond
	screenshotStabilizeInterval = 10 * time.Millisecond
	screenshotStabilizeTimeout = 300 * time.Millisecond
	triggerScreenshot = func() error {
		if err := os.WriteFile(newPath, fullData[:32], 0o644); err != nil {
			return err
		}
		now := time.Now()
		if err := os.Chtimes(newPath, now, now); err != nil {
			return err
		}
		go func() {
			time.Sleep(15 * time.Millisecond)
			_ = os.WriteFile(newPath, fullData[:128], 0o644)
			time.Sleep(15 * time.Millisecond)
			_ = os.WriteFile(newPath, fullData, 0o644)
		}()
		return nil
	}
	t.Cleanup(func() {
		screenshotRootDir = origRoot
		triggerScreenshot = origTrigger
		screenshotPollInterval = origPoll
		screenshotStabilizeInterval = origStableInterval
		screenshotStabilizeTimeout = origStableTimeout
	})

	got, err := TakeScreenshotAndCapture(500 * time.Millisecond)
	if err != nil {
		t.Fatalf("TakeScreenshotAndCapture: %v", err)
	}

	if got.Path != newPath {
		t.Fatalf("Path = %q, want %q", got.Path, newPath)
	}
	if got.CoreName != "newcore" {
		t.Fatalf("CoreName = %q, want %q", got.CoreName, "newcore")
	}
	if got.FileName != "new.png" {
		t.Fatalf("FileName = %q, want %q", got.FileName, "new.png")
	}
	if got.SizeBytes != len(fullData) {
		t.Fatalf("SizeBytes = %d, want %d", got.SizeBytes, len(fullData))
	}

	decoded, err := base64.StdEncoding.DecodeString(got.Data)
	if err != nil {
		t.Fatalf("DecodeString: %v", err)
	}
	if !bytes.Equal(decoded, fullData) {
		t.Fatal("decoded screenshot data does not match final file contents")
	}
}
