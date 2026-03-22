package main

import (
	"testing"
)

func TestSubnetFromIP(t *testing.T) {
	tests := []struct {
		ip   string
		want string
	}{
		{"192.168.1.100", "192.168.1."},
		{"10.0.0.8", "10.0.0."},
		{"172.16.0.1", "172.16.0."},
		{"invalid", ""},
		{"", ""},
		{"1.2.3", ""},
	}
	for _, tt := range tests {
		got := SubnetFromIP(tt.ip)
		if got != tt.want {
			t.Errorf("SubnetFromIP(%q) = %q, want %q", tt.ip, got, tt.want)
		}
	}
}

func TestSubnetIPs(t *testing.T) {
	ips := subnetIPs("192.168.1.")
	if len(ips) != 254 {
		t.Errorf("expected 254 IPs, got %d", len(ips))
	}
	if ips[0] != "192.168.1.1" {
		t.Errorf("expected first IP 192.168.1.1, got %s", ips[0])
	}
	if ips[253] != "192.168.1.254" {
		t.Errorf("expected last IP 192.168.1.254, got %s", ips[253])
	}
}

func TestLocalSubnets(t *testing.T) {
	subnets := localSubnets()
	// Should return at least one subnet on most machines
	// (but don't fail on CI with no network)
	for _, s := range subnets {
		if len(s) == 0 {
			t.Error("empty subnet prefix")
		}
		// Should end with "."
		if s[len(s)-1] != '.' {
			t.Errorf("subnet prefix %q should end with '.'", s)
		}
		// Should not be loopback
		if s == "127.0.0." {
			t.Error("loopback subnet should be excluded")
		}
	}
}

func TestDiscoveredServerStruct(t *testing.T) {
	srv := DiscoveredServer{
		Host: "10.0.0.8",
		Port: 9900,
		Core: "SNES_20250605",
	}
	if srv.Host != "10.0.0.8" {
		t.Errorf("unexpected host: %s", srv.Host)
	}
	if srv.Port != 9900 {
		t.Errorf("unexpected port: %d", srv.Port)
	}
	if srv.Core != "SNES_20250605" {
		t.Errorf("unexpected core: %s", srv.Core)
	}
}

func TestDiscoverCommandExists(t *testing.T) {
	// Verify the discover command is recognized by BuildRequest-style check
	// (discover doesn't go through BuildRequest, but we verify cmdDiscover exists)
	// This is a smoke test — cmdDiscover should not panic
	// We can't fully test network scanning without a server, but we can verify
	// the function is callable.
	oldJSON := jsonFlag
	jsonFlag = true
	defer func() { jsonFlag = oldJSON }()

	// cmdDiscover with no servers on network should succeed (prints empty JSON)
	err := cmdDiscover()
	if err != nil {
		t.Errorf("cmdDiscover returned error: %v", err)
	}
}
