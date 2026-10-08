package mister

import (
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
)

func TestArcadeLaunchMalformedXML(t *testing.T) {
	for _, content := range []string{"not XML", "<misterromdescription>", "<wrongroot/>"} {
		t.Run(content, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "Broken.mra")
			reviewWriteFile(t, path, content)
			sink := reviewCommandSink(t)
			if err := LaunchGame(GameInfo{Name: "Broken", System: "Arcade", Path: path}); err == nil {
				t.Fatal("malformed MRA accepted")
			}
			data, err := os.ReadFile(sink)
			if err != nil {
				t.Fatal(err)
			}
			if len(data) != 0 {
				t.Fatalf("command sent for invalid XML: %q", data)
			}
		})
	}
}

func TestArcadeCoreNameFromDescriptor(t *testing.T) {
	path := filepath.Join(t.TempDir(), "1942 Alias.MRA")
	reviewWriteFile(t, path, "<misterromdescription><name>1942 (Revision B)</name><rbf> jt1942 </rbf><setname>1942</setname></misterromdescription>")
	if got := LaunchCoreName(GameInfo{Path: path, System: "Arcade"}); got != "jt1942" {
		t.Fatalf("core_name=%q, want jt1942", got)
	}
	parsed, err := ParseMRA(path)
	if err != nil {
		t.Fatal(err)
	}
	if parsed.Name != "1942 (Revision B)" || parsed.SetName != "1942" {
		t.Fatalf("MRA metadata=%+v", parsed)
	}
}

func TestArcadeCoreNameDoesNotGuessMalformedDescriptor(t *testing.T) {
	path := filepath.Join(t.TempDir(), "Bad.mra")
	reviewWriteFile(t, path, "<broken")
	if got := LaunchCoreName(GameInfo{Path: path, System: "Arcade"}); got != "" {
		t.Fatalf("guessed core_name=%q", got)
	}
}

func TestValidateCorePathRejectsCommandDelimiters(t *testing.T) {
	for _, delimiter := range []string{"\n", "\r"} {
		path := filepath.Join(t.TempDir(), "Core"+delimiter+"injected.mra")
		reviewWriteFile(t, path, "<misterromdescription/>")
		if err := ValidateCorePath(path); err == nil || !strings.Contains(err.Error(), "delimiter") {
			t.Fatalf("delimiter path not rejected: %v", err)
		}
	}
}

func TestValidateCorePathRejectsSpecialMRAFileWithoutReading(t *testing.T) {
	path := filepath.Join(t.TempDir(), "Pipe.mra")
	if err := syscall.Mkfifo(path, 0600); err != nil {
		t.Fatal(err)
	}
	if err := ValidateCorePath(path); err == nil || !strings.Contains(err.Error(), "regular file") {
		t.Fatalf("special MRA file not rejected: %v", err)
	}
}
