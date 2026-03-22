package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestAddClawexecAutostart(t *testing.T) {
	existing := "#!/bin/bash\necho 'hello'\n"

	result, changed := addClawexecAutostart(existing, 9900)
	if !changed {
		t.Fatal("expected change")
	}
	if !strings.Contains(result, "misterclaw") {
		t.Fatal("expected clawexec entry in result")
	}
	if !strings.Contains(result, "--port 9900") {
		t.Fatal("expected port in entry")
	}
	if !strings.Contains(result, "[[ -e /media/fat/Scripts/misterclaw ]]") {
		t.Fatal("expected existence check")
	}
}

func TestAddClawexecAutostartCustomPort(t *testing.T) {
	existing := "#!/bin/bash\n"

	result, changed := addClawexecAutostart(existing, 8080)
	if !changed {
		t.Fatal("expected change")
	}
	if !strings.Contains(result, "--port 8080") {
		t.Fatal("expected custom port 8080")
	}
}

func TestAddClawexecAutostartIdempotent(t *testing.T) {
	existing := "#!/bin/bash\necho 'hello'\n"

	result1, changed1 := addClawexecAutostart(existing, 9900)
	if !changed1 {
		t.Fatal("expected first change")
	}

	result2, changed2 := addClawexecAutostart(result1, 9900)
	if changed2 {
		t.Fatal("expected no change on second call")
	}
	if result1 != result2 {
		t.Fatal("content should be identical")
	}
}

func TestRemoveClawexecAutostart(t *testing.T) {
	content := "#!/bin/bash\necho 'hello'\n\n# MisterClaw\n[[ -e /media/fat/Scripts/misterclaw ]] && /media/fat/Scripts/misterclaw --port 9900 &\n"

	result, changed := removeClawexecAutostart(content)
	if !changed {
		t.Fatal("expected change")
	}
	if strings.Contains(result, "misterclaw") {
		t.Fatal("expected clawexec entry removed")
	}
	if !strings.Contains(result, "echo 'hello'") {
		t.Fatal("expected other content preserved")
	}
}

func TestRemoveClawexecAutostartNotPresent(t *testing.T) {
	content := "#!/bin/bash\necho 'hello'\n"

	result, changed := removeClawexecAutostart(content)
	if changed {
		t.Fatal("expected no change")
	}
	if result != content {
		t.Fatal("content should be unchanged")
	}
}

func TestInstallUninstallRoundtrip(t *testing.T) {
	original := "#!/bin/bash\necho 'existing stuff'\n"

	// Install
	installed, changed := addClawexecAutostart(original, 9900)
	if !changed {
		t.Fatal("install should change content")
	}

	// Uninstall
	uninstalled, changed := removeClawexecAutostart(installed)
	if !changed {
		t.Fatal("uninstall should change content")
	}

	// Should not contain clawexec
	if strings.Contains(uninstalled, "misterclaw") {
		t.Fatal("clawexec should be removed")
	}
	// Original content should be preserved
	if !strings.Contains(uninstalled, "echo 'existing stuff'") {
		t.Fatal("original content should be preserved")
	}
}

func TestStartupFileModification(t *testing.T) {
	dir := t.TempDir()
	startupPath := filepath.Join(dir, "user-startup.sh")

	original := "#!/bin/bash\necho 'boot'\n"
	if err := os.WriteFile(startupPath, []byte(original), 0755); err != nil {
		t.Fatal(err)
	}

	// Read, modify, write
	content, err := os.ReadFile(startupPath)
	if err != nil {
		t.Fatal(err)
	}

	newContent, changed := addClawexecAutostart(string(content), 9900)
	if !changed {
		t.Fatal("expected change")
	}

	if err := os.WriteFile(startupPath, []byte(newContent), 0755); err != nil {
		t.Fatal(err)
	}

	// Verify
	written, _ := os.ReadFile(startupPath)
	if !strings.Contains(string(written), "misterclaw") {
		t.Fatal("expected entry in file")
	}

	// Second install should be idempotent
	_, changed2 := addClawexecAutostart(string(written), 9900)
	if changed2 {
		t.Fatal("second install should not change file")
	}
}
