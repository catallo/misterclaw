package mister

import (
	"os"
	"strings"
	"testing"
)

func TestExtractAuthURL(t *testing.T) {
	tests := []struct {
		name   string
		input  string
		expect string
	}{
		{
			name:   "url in output",
			input:  "To authenticate, visit:\n\nhttps://login.tailscale.com/a/abc123def456\n",
			expect: "https://login.tailscale.com/a/abc123def456",
		},
		{
			name:   "no url",
			input:  "Success.\n",
			expect: "",
		},
		{
			name:   "url with extra text",
			input:  "Some info\nhttps://login.tailscale.com/a/xyz789\nMore info\n",
			expect: "https://login.tailscale.com/a/xyz789",
		},
		{
			name:   "empty",
			input:  "",
			expect: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := extractAuthURL(tt.input)
			if got != tt.expect {
				t.Errorf("extractAuthURL() = %q, want %q", got, tt.expect)
			}
		})
	}
}

func TestParseTailscaleStatusJSON(t *testing.T) {
	jsonData := []byte(`{
		"BackendState": "Running",
		"Version": "1.80.3",
		"Self": {
			"TailscaleIPs": ["100.64.0.1", "fd7a:115c:a1e0::1"],
			"HostName": "mister-fpga",
			"Online": true
		}
	}`)

	status := &TailscaleStatus{}
	err := parseTailscaleStatusJSON(jsonData, status)
	if err != nil {
		t.Fatalf("parseTailscaleStatusJSON() error: %v", err)
	}

	if status.BackendState != "Running" {
		t.Errorf("BackendState = %q, want %q", status.BackendState, "Running")
	}
	if status.IP != "100.64.0.1" {
		t.Errorf("IP = %q, want %q", status.IP, "100.64.0.1")
	}
	if status.Hostname != "mister-fpga" {
		t.Errorf("Hostname = %q, want %q", status.Hostname, "mister-fpga")
	}
	if !status.Online {
		t.Error("Online = false, want true")
	}
	if status.Version != "1.80.3" {
		t.Errorf("Version = %q, want %q", status.Version, "1.80.3")
	}
}

func TestParseTailscaleStatusJSON_NeedsLogin(t *testing.T) {
	jsonData := []byte(`{
		"BackendState": "NeedsLogin",
		"Self": {
			"TailscaleIPs": [],
			"HostName": "",
			"Online": false
		}
	}`)

	status := &TailscaleStatus{}
	err := parseTailscaleStatusJSON(jsonData, status)
	if err != nil {
		t.Fatalf("error: %v", err)
	}

	if status.BackendState != "NeedsLogin" {
		t.Errorf("BackendState = %q, want %q", status.BackendState, "NeedsLogin")
	}
	if status.IP != "" {
		t.Errorf("IP = %q, want empty", status.IP)
	}
	if status.Online {
		t.Error("Online = true, want false")
	}
}

func TestParseTailscaleStatusJSON_Invalid(t *testing.T) {
	status := &TailscaleStatus{}
	err := parseTailscaleStatusJSON([]byte("not json"), status)
	if err == nil {
		t.Error("expected error for invalid JSON")
	}
}

func TestAddAutostartToContent(t *testing.T) {
	t.Run("adds to empty", func(t *testing.T) {
		result, changed := AddAutostartToContent("")
		if !changed {
			t.Error("expected changed=true")
		}
		if !strings.Contains(result, "tailscaled") {
			t.Error("result should contain tailscaled")
		}
		if !strings.Contains(result, "--tun=userspace-networking") {
			t.Error("result should contain --tun=userspace-networking")
		}
		if !strings.Contains(result, "# Tailscale VPN") {
			t.Error("result should contain comment header")
		}
	})

	t.Run("adds to existing content", func(t *testing.T) {
		existing := "#!/bin/bash\nsome_command &\n"
		result, changed := AddAutostartToContent(existing)
		if !changed {
			t.Error("expected changed=true")
		}
		if !strings.HasPrefix(result, existing) {
			t.Error("should preserve existing content")
		}
		if !strings.Contains(result, "tailscaled") {
			t.Error("should add tailscaled line")
		}
	})

	t.Run("idempotent", func(t *testing.T) {
		existing := "#!/bin/bash\nsome_command &\n"
		first, _ := AddAutostartToContent(existing)
		second, changed := AddAutostartToContent(first)
		if changed {
			t.Error("expected changed=false on second call")
		}
		if first != second {
			t.Error("content should not change on second call")
		}
	})

	t.Run("already has tailscaled", func(t *testing.T) {
		existing := "#!/bin/bash\ntailscaled --some-flag &\n"
		_, changed := AddAutostartToContent(existing)
		if changed {
			t.Error("expected changed=false when tailscaled already present")
		}
	})
}

func TestAddAutostart_TempFile(t *testing.T) {
	// Create a temp file to simulate the startup script
	tmpFile, err := os.CreateTemp("", "user-startup-*.sh")
	if err != nil {
		t.Fatal(err)
	}
	defer os.Remove(tmpFile.Name())

	initial := "#!/bin/bash\necho hello\n"
	tmpFile.WriteString(initial)
	tmpFile.Close()

	// Read, modify, and verify
	content, err := os.ReadFile(tmpFile.Name())
	if err != nil {
		t.Fatal(err)
	}

	result, changed := AddAutostartToContent(string(content))
	if !changed {
		t.Error("expected changed=true")
	}

	if err := os.WriteFile(tmpFile.Name(), []byte(result), 0755); err != nil {
		t.Fatal(err)
	}

	// Verify the file
	final, _ := os.ReadFile(tmpFile.Name())
	finalStr := string(final)
	if !strings.Contains(finalStr, "echo hello") {
		t.Error("original content should be preserved")
	}
	if !strings.Contains(finalStr, "tailscaled") {
		t.Error("tailscaled entry should be present")
	}

	// Apply again — should be idempotent
	_, changed = AddAutostartToContent(finalStr)
	if changed {
		t.Error("second application should be idempotent")
	}
}

func TestTailscaleConstants(t *testing.T) {
	if tailscaleDir != "/media/fat/tailscale" {
		t.Errorf("tailscaleDir = %q", tailscaleDir)
	}
	if tailscaleBin != "/media/fat/tailscale/tailscale" {
		t.Errorf("tailscaleBin = %q", tailscaleBin)
	}
	if tailscaledBin != "/media/fat/tailscale/tailscaled" {
		t.Errorf("tailscaledBin = %q", tailscaledBin)
	}
	if tailscaleSocket != "/tmp/tailscale.sock" {
		t.Errorf("tailscaleSocket = %q", tailscaleSocket)
	}
	if tailscaleDefaultURL != "https://pkgs.tailscale.com/stable/tailscale_1.80.3_arm.tgz" {
		t.Errorf("tailscaleDefaultURL = %q", tailscaleDefaultURL)
	}
}
