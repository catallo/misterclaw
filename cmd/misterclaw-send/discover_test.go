package main

import (
	"encoding/json"
	"fmt"
	"net"
	"testing"
	"time"
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

// Discovery tests use loopback fixtures, never scan a developer or CI subnet.
func TestDiscoverProbeLoopback(t *testing.T) {
	for _, valid := range []bool{true, false} {
		t.Run(fmt.Sprintf("valid=%t", valid), func(t *testing.T) {
			listener, err := net.Listen("tcp", "127.0.0.1:0")
			if err != nil {
				t.Fatal(err)
			}
			defer listener.Close()
			done := make(chan error, 1)
			go func() {
				conn, err := listener.Accept()
				if err != nil {
					done <- err
					return
				}
				defer conn.Close()
				conn.SetDeadline(time.Now().Add(2 * time.Second))
				var request map[string]string
				if err := json.NewDecoder(conn).Decode(&request); err != nil {
					done <- err
					return
				}
				if request["mister"] != "status" {
					done <- fmt.Errorf("unexpected discovery request: %v", request)
					return
				}
				response := map[string]string{"unrelated": "service"}
				if valid {
					response = map[string]string{"mister": "status", "core_name": "SNES_fixture"}
				}
				done <- json.NewEncoder(conn).Encode(response)
			}()
			port := listener.Addr().(*net.TCPAddr).Port
			server, found := probeServer("127.0.0.1", port, 2*time.Second)
			if err := <-done; err != nil {
				t.Fatal(err)
			}
			if found != valid {
				t.Fatalf("found=%t, want %t", found, valid)
			}
			if valid && (server.Host != "127.0.0.1" || server.Port != port || server.Core != "SNES_fixture") {
				t.Fatalf("unexpected discovery result: %+v", server)
			}
		})
	}
}
