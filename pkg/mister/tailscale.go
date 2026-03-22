package mister

import (
	"encoding/json"
	"bufio"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

const (
	tailscaleDir        = "/media/fat/tailscale"
	tailscaleBin        = tailscaleDir + "/tailscale"
	tailscaledBin       = tailscaleDir + "/tailscaled"
	tailscaleState      = tailscaleDir + "/state"
	tailscaleSocket     = "/tmp/tailscale.sock"
	tailscaleLog        = "/tmp/tailscaled.log"
	tailscaleDefaultURL = "https://pkgs.tailscale.com/stable/tailscale_1.80.3_arm.tgz"
	startupFile         = "/media/fat/linux/user-startup.sh"
)

// TailscaleStatus holds the current state of Tailscale on this MiSTer.
type TailscaleStatus struct {
	Installed    bool   `json:"installed"`
	Running      bool   `json:"running"`
	IP           string `json:"ip,omitempty"`
	Hostname     string `json:"hostname,omitempty"`
	Online       bool   `json:"online"`
	Version      string `json:"version,omitempty"`
	BackendState string `json:"backend_state,omitempty"`
}

// tailscaleStatusJSON is the subset of `tailscale status --json` we care about.
type tailscaleStatusJSON struct {
	BackendState string `json:"BackendState"`
	Self         struct {
		TailscaleIPs []string `json:"TailscaleIPs"`
		HostName     string   `json:"HostName"`
		Online       bool     `json:"Online"`
	} `json:"Self"`
	Version string `json:"Version"`
}

// TailscaleSetup downloads, installs, starts tailscaled, runs tailscale up,
// and configures autostart. Returns the auth URL if login is needed.
func TailscaleSetup(downloadURL string, hostname string) (string, error) {
	if downloadURL == "" {
		downloadURL = tailscaleDefaultURL
	}
	if hostname == "" {
		hostname = "mister-fpga"
	}

	// Download if not installed
	if _, err := os.Stat(tailscaledBin); os.IsNotExist(err) {
		if err := downloadTailscale(downloadURL); err != nil {
			return "", fmt.Errorf("download failed: %w", err)
		}
	}

	// Start daemon if not running
	if err := TailscaleStart(); err != nil {
		return "", fmt.Errorf("start failed: %w", err)
	}

	// Run tailscale up
	authURL, err := tailscaleUp(hostname)
	if err != nil {
		return "", fmt.Errorf("tailscale up failed: %w", err)
	}

	// Configure autostart
	if err := addAutostart(); err != nil {
		return "", fmt.Errorf("autostart setup failed: %w", err)
	}

	// If no auth URL, we're already authenticated — return IP
	if authURL == "" {
		status, err := TailscaleGetStatus()
		if err == nil && status.IP != "" {
			return "", nil
		}
	}

	return authURL, nil
}

// TailscaleGetStatus returns the current Tailscale status.
func TailscaleGetStatus() (*TailscaleStatus, error) {
	status := &TailscaleStatus{}

	// Check if installed
	_, errBin := os.Stat(tailscaleBin)
	_, errDaemon := os.Stat(tailscaledBin)
	status.Installed = errBin == nil && errDaemon == nil

	// Check if running
	status.Running = isTailscaledRunning()

	if !status.Running {
		return status, nil
	}

	// Get detailed status from tailscale CLI
	out, err := exec.Command(tailscaleBin, "--socket="+tailscaleSocket, "status", "--json").Output()
	if err != nil {
		return status, nil // running but can't get details
	}

	if err := parseTailscaleStatusJSON(out, status); err != nil {
		return status, nil
	}

	return status, nil
}

// TailscaleStop kills the tailscaled process.
func TailscaleStop() error {
	if !isTailscaledRunning() {
		return nil
	}
	return exec.Command("pkill", "tailscaled").Run()
}

// TailscaleStart starts tailscaled if not already running.
func TailscaleStart() error {
	if isTailscaledRunning() {
		return nil
	}

	if _, err := os.Stat(tailscaledBin); os.IsNotExist(err) {
		return fmt.Errorf("tailscaled not installed at %s", tailscaledBin)
	}

	cmd := exec.Command(tailscaledBin,
		"--tun=userspace-networking",
		"--state="+tailscaleState,
		"--socket="+tailscaleSocket,
	)

	logFile, err := os.OpenFile(tailscaleLog, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0644)
	if err == nil {
		cmd.Stdout = logFile
		cmd.Stderr = logFile
	}

	if err := cmd.Start(); err != nil {
		return fmt.Errorf("failed to start tailscaled: %w", err)
	}

	// Release the process so it runs in the background
	if cmd.Process != nil {
		cmd.Process.Release()
	}

	return nil
}

func isTailscaledRunning() bool {
	out, err := exec.Command("pidof", "tailscaled").Output()
	_ = out
	return err == nil
}

func downloadTailscale(url string) error {
	if err := os.MkdirAll(tailscaleDir, 0755); err != nil {
		return err
	}

	tarball := "/tmp/tailscale.tgz"

	// Download with curl -kL (insecure + follow redirects, no CA certs on MiSTer)
	cmd := exec.Command("curl", "-kL", "-o", tarball, url)
	if out, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("curl failed: %s: %w", string(out), err)
	}

	// Extract just the binaries
	// The tarball contains a directory like tailscale_1.80.3_arm/
	// We need tailscale and tailscaled from inside it
	cmd = exec.Command("tar", "xzf", tarball, "-C", "/tmp")
	if out, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("tar failed: %s: %w", string(out), err)
	}

	// Find and copy binaries
	matches, _ := filepath.Glob("/tmp/tailscale_*/tailscale")
	if len(matches) == 0 {
		return fmt.Errorf("tailscale binary not found in archive")
	}
	extractedDir := filepath.Dir(matches[0])

	for _, bin := range []string{"tailscale", "tailscaled"} {
		src := filepath.Join(extractedDir, bin)
		dst := filepath.Join(tailscaleDir, bin)
		data, err := os.ReadFile(src)
		if err != nil {
			return fmt.Errorf("read %s: %w", bin, err)
		}
		if err := os.WriteFile(dst, data, 0755); err != nil {
			return fmt.Errorf("write %s: %w", bin, err)
		}
	}

	// Cleanup
	os.Remove(tarball)
	os.RemoveAll(extractedDir)

	return nil
}

func tailscaleUp(hostname string) (string, error) {
	cmd := exec.Command(tailscaleBin,
		"--socket="+tailscaleSocket,
		"up",
		"--hostname="+hostname,
	)

	// Use a pipe to read output as it comes — tailscale up blocks until auth
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return "", fmt.Errorf("creating stdout pipe: %w", err)
	}
	cmd.Stderr = cmd.Stdout // merge stderr into stdout

	if err := cmd.Start(); err != nil {
		return "", fmt.Errorf("starting tailscale up: %w", err)
	}

	// Read output line by line, looking for auth URL
	scanner := bufio.NewScanner(stdout)
	var authURL string
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if url := extractAuthURL(line); url != "" {
			authURL = url
			break
		}
	}

	if authURL != "" {
		// Let tailscale up continue in background — it will complete when user authenticates
		go cmd.Wait()
		return authURL, nil
	}

	// If no auth URL found, process may have exited (already authenticated?)
	cmd.Wait()
	return "", nil
}

// extractAuthURL finds a Tailscale login URL in command output.
func extractAuthURL(output string) string {
	for _, line := range strings.Split(output, "\n") {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "https://login.tailscale.com/") {
			return line
		}
	}
	return ""
}

// parseTailscaleStatusJSON parses the JSON output of `tailscale status --json`.
func parseTailscaleStatusJSON(data []byte, status *TailscaleStatus) error {
	var ts tailscaleStatusJSON
	if err := json.Unmarshal(data, &ts); err != nil {
		return err
	}

	status.BackendState = ts.BackendState
	status.Hostname = ts.Self.HostName
	status.Online = ts.Self.Online
	status.Version = ts.Version

	if len(ts.Self.TailscaleIPs) > 0 {
		status.IP = ts.Self.TailscaleIPs[0]
	}

	return nil
}

// autostartLine is the command added to user-startup.sh.
const autostartLine = tailscaledBin + " --tun=userspace-networking --state=" + tailscaleState + " --socket=" + tailscaleSocket + " &"

// addAutostart adds the tailscaled startup line to user-startup.sh.
func addAutostart() error {
	// Read existing content
	content, err := os.ReadFile(startupFile)
	if err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("read startup file: %w", err)
	}

	// Check if already present
	if strings.Contains(string(content), "tailscaled") {
		return nil
	}

	// Backup first
	if len(content) > 0 {
		backupPath := startupFile + ".bak"
		if err := os.WriteFile(backupPath, content, 0755); err != nil {
			return fmt.Errorf("backup failed: %w", err)
		}
	}

	// Append
	f, err := os.OpenFile(startupFile, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0755)
	if err != nil {
		return fmt.Errorf("open startup file: %w", err)
	}
	defer f.Close()

	line := "\n# Tailscale VPN\n" + autostartLine + "\n"
	if _, err := f.WriteString(line); err != nil {
		return err
	}

	return nil
}

// AddAutostartToContent is a testable helper that adds the autostart line to startup script content.
// Returns the modified content and whether it was changed.
func AddAutostartToContent(content string) (string, bool) {
	if strings.Contains(content, "tailscaled") {
		return content, false
	}
	return content + "\n# Tailscale VPN\n" + autostartLine + "\n", true
}
