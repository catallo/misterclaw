package main

import (
	"fmt"
	"io"
	"os"
	"strings"
	"time"
)

const (
	targetBinaryPath = "/media/fat/Scripts/misterclaw"
	startupFilePath  = "/media/fat/linux/user-startup.sh"
	clawexecMarker   = "misterclaw"
)

func installServer(port int) {
	// 1. Get own binary path
	selfPath, err := os.Executable()
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: cannot determine own path: %v\n", err)
		os.Exit(1)
	}

	// 2. Copy self to target if not already there
	if selfPath != targetBinaryPath {
		if err := copyFile(selfPath, targetBinaryPath); err != nil {
			fmt.Fprintf(os.Stderr, "Error: cannot copy binary: %v\n", err)
			os.Exit(1)
		}
	}

	// 3. Backup startup file
	backupPath := ""
	content, err := os.ReadFile(startupFilePath)
	if err != nil && !os.IsNotExist(err) {
		fmt.Fprintf(os.Stderr, "Error: cannot read startup file: %v\n", err)
		os.Exit(1)
	}
	if len(content) > 0 {
		backupPath = startupFilePath + ".bak-" + time.Now().Format("20060102")
		if err := os.WriteFile(backupPath, content, 0755); err != nil {
			fmt.Fprintf(os.Stderr, "Error: cannot backup startup file: %v\n", err)
			os.Exit(1)
		}
	}

	// 4. Add autostart entry (idempotent)
	newContent, changed := addClawexecAutostart(string(content), port)
	if !changed {
		fmt.Println("MisterClaw is already installed.")
		return
	}

	if err := os.WriteFile(startupFilePath, []byte(newContent), 0755); err != nil {
		fmt.Fprintf(os.Stderr, "Error: cannot write startup file: %v\n", err)
		os.Exit(1)
	}

	// 5. Print confirmation
	fmt.Println("MisterClaw installed!")
	fmt.Printf("  Binary: %s\n", targetBinaryPath)
	fmt.Printf("  Autostart: %s (port %d)\n", startupFilePath, port)
	if backupPath != "" {
		fmt.Printf("  Backup: %s\n", backupPath)
	}
	fmt.Printf("\nStart now with: %s --port %d &\n", targetBinaryPath, port)
	fmt.Println("Or reboot your MiSTer-FPGA.")
}

func uninstallServer() {
	// 1. Read startup file
	content, err := os.ReadFile(startupFilePath)
	if err != nil {
		if os.IsNotExist(err) {
			fmt.Println("Nothing to uninstall: startup file not found.")
			return
		}
		fmt.Fprintf(os.Stderr, "Error: cannot read startup file: %v\n", err)
		os.Exit(1)
	}

	// 2. Remove autostart entry
	newContent, changed := removeClawexecAutostart(string(content))
	if !changed {
		fmt.Println("MisterClaw is not in autostart.")
		return
	}

	// 3. Backup
	backupPath := startupFilePath + ".bak-" + time.Now().Format("20060102")
	if err := os.WriteFile(backupPath, content, 0755); err != nil {
		fmt.Fprintf(os.Stderr, "Error: cannot backup startup file: %v\n", err)
		os.Exit(1)
	}

	// 4. Write modified file
	if err := os.WriteFile(startupFilePath, []byte(newContent), 0755); err != nil {
		fmt.Fprintf(os.Stderr, "Error: cannot write startup file: %v\n", err)
		os.Exit(1)
	}

	fmt.Println("MisterClaw removed from autostart.")
	fmt.Printf("  Backup: %s\n", backupPath)
	fmt.Printf("  Binary NOT removed: %s\n", targetBinaryPath)
	fmt.Println("  Delete it manually if desired.")
}

// addClawexecAutostart adds the MisterClaw autostart entry to startup script content.
// Returns the modified content and whether it was changed.
func addClawexecAutostart(content string, port int) (string, bool) {
	if strings.Contains(content, clawexecMarker) {
		return content, false
	}
	entry := fmt.Sprintf("\n# MisterClaw\n[[ -e %s ]] && %s --port %d &\n",
		targetBinaryPath, targetBinaryPath, port)
	return content + entry, true
}

// removeClawexecAutostart removes MisterClaw autostart lines from startup script content.
// Returns the modified content and whether it was changed.
func removeClawexecAutostart(content string) (string, bool) {
	lines := strings.Split(content, "\n")
	var result []string
	changed := false
	for _, line := range lines {
		if strings.Contains(line, clawexecMarker) || strings.Contains(line, "# MisterClaw") {
			changed = true
			continue
		}
		result = append(result, line)
	}
	if !changed {
		return content, false
	}
	// Clean up trailing empty lines that were left behind
	out := strings.Join(result, "\n")
	for strings.HasSuffix(out, "\n\n") {
		out = strings.TrimSuffix(out, "\n")
	}
	return out, true
}

func copyFile(src, dst string) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()

	out, err := os.OpenFile(dst, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0755)
	if err != nil {
		return err
	}
	defer out.Close()

	if _, err := io.Copy(out, in); err != nil {
		return err
	}
	return out.Close()
}
