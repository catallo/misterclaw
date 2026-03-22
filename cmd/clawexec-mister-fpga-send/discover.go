package main

import (
	"encoding/json"
	"fmt"
	"net"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"
)

// DiscoveredServer represents a MiSTer-FPGA server found on the network.
type DiscoveredServer struct {
	Host string `json:"host"`
	Port int    `json:"port"`
	Core string `json:"core,omitempty"`
}

// discoverServers scans all local /24 subnets for ClawExec MiSTer-FPGA servers.
func discoverServers(port int, timeout time.Duration) []DiscoveredServer {
	subnets := localSubnets()
	if len(subnets) == 0 {
		return nil
	}

	var (
		mu      sync.Mutex
		results []DiscoveredServer
		wg      sync.WaitGroup
		sem     = make(chan struct{}, 50) // max 50 concurrent connections
	)

	// Overall deadline
	deadline := time.After(3 * time.Second)

	for _, subnet := range subnets {
		ips := subnetIPs(subnet)
		for _, ip := range ips {
			select {
			case <-deadline:
				goto done
			default:
			}

			wg.Add(1)
			sem <- struct{}{}
			go func(ip string) {
				defer wg.Done()
				defer func() { <-sem }()

				if srv, ok := probeServer(ip, port, timeout); ok {
					mu.Lock()
					results = append(results, srv)
					mu.Unlock()
				}
			}(ip)
		}
	}

done:
	// Wait for in-flight goroutines
	wg.Wait()

	return results
}

// probeServer checks if a host:port has a ClawExec MiSTer-FPGA server.
func probeServer(host string, port int, timeout time.Duration) (DiscoveredServer, bool) {
	addr := net.JoinHostPort(host, strconv.Itoa(port))
	conn, err := net.DialTimeout("tcp", addr, timeout)
	if err != nil {
		return DiscoveredServer{}, false
	}
	defer conn.Close()

	conn.SetDeadline(time.Now().Add(timeout))

	// Send status request
	req := map[string]string{"mister": "status"}
	if err := json.NewEncoder(conn).Encode(req); err != nil {
		return DiscoveredServer{}, false
	}

	var resp map[string]interface{}
	if err := json.NewDecoder(conn).Decode(&resp); err != nil {
		return DiscoveredServer{}, false
	}

	// Check for "mister" key in response
	if _, ok := resp["mister"]; !ok {
		return DiscoveredServer{}, false
	}

	srv := DiscoveredServer{
		Host: host,
		Port: port,
	}
	if core, ok := resp["core_name"].(string); ok {
		srv.Core = core
	}

	return srv, true
}

// localSubnets returns /24 subnet prefixes (e.g. "192.168.1.") for all non-loopback interfaces.
func localSubnets() []string {
	ifaces, err := net.Interfaces()
	if err != nil {
		return nil
	}

	seen := make(map[string]bool)
	var subnets []string

	for _, iface := range ifaces {
		if iface.Flags&net.FlagLoopback != 0 || iface.Flags&net.FlagUp == 0 {
			continue
		}
		addrs, err := iface.Addrs()
		if err != nil {
			continue
		}
		for _, addr := range addrs {
			ipnet, ok := addr.(*net.IPNet)
			if !ok {
				continue
			}
			ip4 := ipnet.IP.To4()
			if ip4 == nil {
				continue
			}
			// Skip loopback range
			if ip4[0] == 127 {
				continue
			}
			// Use /24 prefix
			prefix := fmt.Sprintf("%d.%d.%d.", ip4[0], ip4[1], ip4[2])
			if !seen[prefix] {
				seen[prefix] = true
				subnets = append(subnets, prefix)
			}
		}
	}

	return subnets
}

// subnetIPs returns all 254 host IPs for a /24 subnet prefix (e.g. "192.168.1.").
func subnetIPs(prefix string) []string {
	ips := make([]string, 0, 254)
	for i := 1; i <= 254; i++ {
		ips = append(ips, prefix+strconv.Itoa(i))
	}
	return ips
}

// resolveHost tries direct connection first, falls back to LAN discovery.
func resolveHost(host string, port int) string {
	// Try direct connection first (works for Tailscale hostname or explicit IP)
	addr := net.JoinHostPort(host, strconv.Itoa(port))
	conn, err := net.DialTimeout("tcp", addr, 2*time.Second)
	if err == nil {
		conn.Close()
		return host
	}

	// Auto-discover on LAN
	fmt.Fprintf(os.Stderr, "Host %q not reachable, scanning local network...\n", host)
	servers := discoverServers(port, 500*time.Millisecond)

	if len(servers) == 1 {
		fmt.Fprintf(os.Stderr, "Auto-discovered MiSTer-FPGA at %s\n", servers[0].Host)
		return servers[0].Host
	}

	if len(servers) > 1 {
		fmt.Fprintf(os.Stderr, "Found %d servers — specify --host:\n", len(servers))
		for _, s := range servers {
			fmt.Fprintf(os.Stderr, "  %s:%d\n", s.Host, s.Port)
		}
		os.Exit(1)
	}

	// No servers found, return original host (will fail with connection error)
	return host
}

// cmdDiscover implements the "discover" subcommand.
func cmdDiscover() error {
	servers := discoverServers(portFlag, 500*time.Millisecond)

	if jsonFlag {
		enc := json.NewEncoder(os.Stdout)
		enc.SetIndent("", "  ")
		enc.Encode(servers)
		return nil
	}

	if len(servers) == 0 {
		fmt.Println("No MiSTer-FPGA servers found on local network.")
		fmt.Println("\nTips:")
		fmt.Println("  - Make sure the MiSTer-FPGA server is running")
		fmt.Println("  - Check that you're on the same network")
		fmt.Println("  - Try specifying the host directly: --host <IP>")
		return nil
	}

	fmt.Printf("Found %d MiSTer-FPGA server(s):\n", len(servers))
	for _, s := range servers {
		line := fmt.Sprintf("  %s:%d", s.Host, s.Port)
		if s.Core != "" {
			line += fmt.Sprintf(" — Core: %s", s.Core)
		}
		fmt.Println(line)
	}

	return nil
}

// SubnetFromIP extracts the /24 subnet prefix from an IP string.
// Exported for testing.
func SubnetFromIP(ip string) string {
	parts := strings.Split(ip, ".")
	if len(parts) != 4 {
		return ""
	}
	return parts[0] + "." + parts[1] + "." + parts[2] + "."
}
