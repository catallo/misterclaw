package mister

import (
	"encoding/json"
	"testing"
)

func TestGetSystemInfo(t *testing.T) {
	info := GetSystemInfo()

	// On any Linux system, we should get some values
	if info.RAMMb == 0 {
		t.Error("RAM total should be non-zero on Linux")
	}
	if info.Uptime == "unknown" {
		t.Error("uptime should be readable on Linux")
	}
	if info.Hostname == "" {
		t.Error("hostname should not be empty")
	}

	// Verify it's JSON-serializable
	data, err := json.Marshal(info)
	if err != nil {
		t.Fatalf("json.Marshal: %v", err)
	}
	if len(data) == 0 {
		t.Error("serialized JSON is empty")
	}
}

func TestReadUptime(t *testing.T) {
	up := readUptime()
	if up == "" || up == "unknown" {
		t.Errorf("readUptime() = %q", up)
	}
}

func TestReadMemInfo(t *testing.T) {
	total, free := readMemInfo()
	if total == 0 {
		t.Error("total RAM = 0")
	}
	if free == 0 {
		t.Error("free RAM = 0")
	}
	if free > total {
		t.Errorf("free (%d) > total (%d)", free, total)
	}
}
