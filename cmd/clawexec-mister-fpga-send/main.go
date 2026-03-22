package main

import (
	"encoding/base64"
	"encoding/json"
	"flag"
	"fmt"
	"net"
	"os"
	"strconv"
	"strings"
	"time"
)

var version = "dev"

// Global flags
var (
	hostFlag    string
	portFlag    int
	jsonFlag    bool
	timeoutFlag int
)

func main() {
	flag.StringVar(&hostFlag, "host", "mister-fpga", "MiSTer-FPGA host (IP or hostname)")
	flag.StringVar(&hostFlag, "H", "mister-fpga", "MiSTer-FPGA host (shorthand)")
	flag.IntVar(&portFlag, "port", 9900, "Port")
	flag.IntVar(&portFlag, "p", 9900, "Port (shorthand)")
	flag.BoolVar(&jsonFlag, "json", false, "JSON output")
	flag.BoolVar(&jsonFlag, "j", false, "JSON output (shorthand)")
	flag.IntVar(&timeoutFlag, "timeout", 10, "Timeout in seconds")
	flag.IntVar(&timeoutFlag, "t", 10, "Timeout in seconds (shorthand)")

	flag.Usage = func() { printHelp() }
	flag.Parse()

	args := flag.Args()
	if len(args) == 0 {
		printHelp()
		os.Exit(0)
	}

	cmd := args[0]
	cmdArgs := args[1:]

	// Resolve host via auto-discovery for commands that need a connection
	if cmd != "help" && cmd != "discover" {
		hostFlag = resolveHost(hostFlag, portFlag)
	}

	var err error
	switch cmd {
	case "status":
		err = cmdStatus()
	case "launch":
		err = cmdLaunch(cmdArgs)
	case "search":
		err = cmdSearch(cmdArgs)
	case "systems":
		err = cmdSystems()
	case "screenshot":
		err = cmdScreenshot(cmdArgs)
	case "info":
		err = cmdInfo()
	case "tailscale":
		err = cmdTailscale(cmdArgs)
	case "shell":
		err = cmdShell(cmdArgs)
	case "discover":
		err = cmdDiscover()
	case "help":
		printHelp()
	default:
		fmt.Fprintf(os.Stderr, "Unknown command: %s\n", cmd)
		printHelp()
		os.Exit(1)
	}

	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}
}

// sendRequest sends a JSON request to the server and returns the response.
func sendRequest(req map[string]interface{}) (map[string]interface{}, error) {
	addr := net.JoinHostPort(hostFlag, strconv.Itoa(portFlag))
	timeout := time.Duration(timeoutFlag) * time.Second

	conn, err := net.DialTimeout("tcp", addr, timeout)
	if err != nil {
		return nil, fmt.Errorf("connecting to %s: %w", addr, err)
	}
	defer conn.Close()

	conn.SetDeadline(time.Now().Add(timeout))

	if err := json.NewEncoder(conn).Encode(req); err != nil {
		return nil, fmt.Errorf("sending request: %w", err)
	}

	var resp map[string]interface{}
	if err := json.NewDecoder(conn).Decode(&resp); err != nil {
		return nil, fmt.Errorf("reading response: %w", err)
	}

	if errMsg, ok := resp["error"].(string); ok && errMsg != "" {
		return resp, fmt.Errorf("%s", errMsg)
	}

	return resp, nil
}

// sendShellRequest sends a shell command and collects streamed output.
func sendShellRequest(command string) (string, int, error) {
	addr := net.JoinHostPort(hostFlag, strconv.Itoa(portFlag))
	timeout := time.Duration(timeoutFlag) * time.Second

	conn, err := net.DialTimeout("tcp", addr, timeout)
	if err != nil {
		return "", 1, fmt.Errorf("connecting to %s: %w", addr, err)
	}
	defer conn.Close()

	req := map[string]interface{}{
		"cmd":     command,
		"session": "clawexec-fpga-cli",
		"pty":     false,
	}
	if err := json.NewEncoder(conn).Encode(req); err != nil {
		return "", 1, fmt.Errorf("sending request: %w", err)
	}

	var output strings.Builder
	exitCode := 0
	dec := json.NewDecoder(conn)

	for {
		conn.SetDeadline(time.Now().Add(timeout))
		var resp map[string]interface{}
		if err := dec.Decode(&resp); err != nil {
			break
		}
		if data, ok := resp["data"].(string); ok {
			output.WriteString(data)
		}
		if done, ok := resp["done"].(bool); ok && done {
			if code, ok := resp["exit_code"].(float64); ok {
				exitCode = int(code)
			}
			break
		}
		if errMsg, ok := resp["error"].(string); ok && errMsg != "" {
			return "", 1, fmt.Errorf("%s", errMsg)
		}
	}

	return output.String(), exitCode, nil
}

func outputJSON(resp map[string]interface{}) {
	enc := json.NewEncoder(os.Stdout)
	enc.SetIndent("", "  ")
	enc.Encode(resp)
}

func cmdStatus() error {
	resp, err := sendRequest(map[string]interface{}{"mister": "status"})
	if err != nil {
		return err
	}

	if jsonFlag {
		outputJSON(resp)
		return nil
	}

	coreName, _ := resp["core_name"].(string)
	gamePath, _ := resp["game_path"].(string)

	if coreName == "" {
		coreName = "(none)"
	}
	fmt.Printf("Core: %s\n", coreName)
	if gamePath != "" {
		fmt.Printf("Game: %s\n", gamePath)
	}
	return nil
}

func cmdLaunch(args []string) error {
	system, rest := extractFlag(args, "system", "s")
	path, positional := extractFlag(rest, "path", "")

	req := map[string]interface{}{"mister": "launch"}

	if path != "" {
		if system == "" {
			return fmt.Errorf("--path requires --system")
		}
		req["path"] = path
		req["system"] = system
	} else {
		query := strings.Join(positional, " ")
		if query == "" {
			return fmt.Errorf("usage: clawexec-mister-fpga-send launch <query> [--system <system>]\n       clawexec-mister-fpga-send launch --path <path> --system <system>")
		}
		req["query"] = query
		if system != "" {
			req["system"] = system
		}
	}

	resp, err := sendRequest(req)
	if err != nil {
		return err
	}

	if jsonFlag {
		outputJSON(resp)
		return nil
	}

	game, _ := resp["game"].(string)
	fmt.Printf("Launched: %s\n", game)
	return nil
}

func cmdSearch(args []string) error {
	system, rest := extractFlag(args, "system", "s")
	limitStr, positional := extractFlag(rest, "limit", "")
	limit := 20
	if limitStr != "" {
		if n, err := strconv.Atoi(limitStr); err == nil {
			limit = n
		}
	}

	query := strings.Join(positional, " ")
	if query == "" {
		return fmt.Errorf("usage: clawexec-mister-fpga-send search <query> [--system <system>] [--limit N]")
	}

	req := map[string]interface{}{
		"mister": "search",
		"query":  query,
	}
	if system != "" {
		req["system"] = system
	}

	resp, err := sendRequest(req)
	if err != nil {
		return err
	}

	if jsonFlag {
		outputJSON(resp)
		return nil
	}

	results, _ := resp["results"].([]interface{})
	shown := 0
	for i, r := range results {
		if shown >= limit {
			break
		}
		if game, ok := r.(map[string]interface{}); ok {
			name, _ := game["name"].(string)
			sys, _ := game["system"].(string)
			loc, _ := game["location"].(string)
			fmt.Printf("%d. %s [%s, %s]\n", i+1, name, sys, loc)
			shown++
		}
	}
	total, _ := resp["total"].(float64)
	fmt.Printf("Found: %d results\n", int(total))
	return nil
}

func cmdSystems() error {
	resp, err := sendRequest(map[string]interface{}{"mister": "systems"})
	if err != nil {
		return err
	}

	if jsonFlag {
		outputJSON(resp)
		return nil
	}

	systems, _ := resp["systems"].([]interface{})
	for _, s := range systems {
		if sys, ok := s.(map[string]interface{}); ok {
			name, _ := sys["system"].(string)
			count, _ := sys["rom_count"].(float64)
			loc, _ := sys["location"].(string)
			fmt.Printf("%-16s %5d ROMs (%s)\n", name, int(count), loc)
		}
	}
	return nil
}

func cmdScreenshot(args []string) error {
	output, _ := extractFlag(args, "output", "o")

	resp, err := sendRequest(map[string]interface{}{"mister": "screenshot"})
	if err != nil {
		return err
	}

	if jsonFlag && output == "" {
		outputJSON(resp)
		return nil
	}

	data, _ := resp["data"].(string)
	if data == "" {
		return fmt.Errorf("no screenshot data received")
	}

	if output == "" {
		// Print base64 to stdout
		fmt.Print(data)
		return nil
	}

	decoded, err := base64.StdEncoding.DecodeString(data)
	if err != nil {
		return fmt.Errorf("decoding screenshot: %w", err)
	}

	if err := os.WriteFile(output, decoded, 0644); err != nil {
		return fmt.Errorf("writing file: %w", err)
	}

	core, _ := resp["core"].(string)
	fmt.Printf("Screenshot saved: %s (%dKB, %s core)\n", output, len(decoded)/1024, core)
	return nil
}

func cmdInfo() error {
	resp, err := sendRequest(map[string]interface{}{"mister": "info"})
	if err != nil {
		return err
	}

	if jsonFlag {
		outputJSON(resp)
		return nil
	}

	temp, _ := resp["temp"].(float64)
	ramMb, _ := resp["ram_mb"].(float64)
	ramFree, _ := resp["ram_free_mb"].(float64)
	uptime, _ := resp["uptime"].(string)
	ip, _ := resp["ip"].(string)
	hostname, _ := resp["hostname"].(string)

	fmt.Printf("Hostname: %s\n", hostname)
	fmt.Printf("IP:       %s\n", ip)
	fmt.Printf("Temp:     %.1f°C\n", temp)
	fmt.Printf("RAM:      %d/%d MB free\n", int(ramFree), int(ramMb))

	// Display all mounted disks
	if disks, ok := resp["disks"].([]interface{}); ok {
		for _, d := range disks {
			disk, ok := d.(map[string]interface{})
			if !ok {
				continue
			}
			mount, _ := disk["mount"].(string)
			totalMb, _ := disk["total_mb"].(float64)
			freeMb, _ := disk["free_mb"].(float64)
			usePct, _ := disk["use_pct"].(string)
			dev, _ := disk["device"].(string)
			fmt.Printf("Disk:     %s — %d/%d MB free (%s used) [%s]\n", mount, int(freeMb), int(totalMb), usePct, dev)
		}
	}

	fmt.Printf("Uptime:   %s\n", uptime)
	return nil
}

func cmdTailscale(args []string) error {
	if len(args) == 0 {
		return fmt.Errorf("usage: clawexec-mister-fpga-send tailscale <setup|status|start|stop>")
	}

	action := args[0]
	switch action {
	case "setup":
		return cmdTailscaleSetup()
	case "status":
		return cmdTailscaleStatus()
	case "start", "stop":
		return cmdTailscaleSimple(action)
	default:
		return fmt.Errorf("unknown tailscale action: %s (use: setup, status, start, stop)", action)
	}
}

func cmdTailscaleSetup() error {
	resp, err := sendRequest(map[string]interface{}{
		"mister": "tailscale",
		"action": "setup",
	})
	if err != nil {
		return err
	}

	authURL, _ := resp["auth_url"].(string)

	if authURL == "" {
		// Already authenticated
		ip, _ := resp["ip"].(string)
		if jsonFlag {
			outputJSON(resp)
			return nil
		}
		fmt.Println("Tailscale: already authenticated")
		if ip != "" {
			fmt.Printf("IP: %s\n", ip)
		}
		return nil
	}

	if !jsonFlag {
		fmt.Printf("Please authenticate: %s\n", authURL)
		fmt.Println("Waiting for authentication...")
	}

	// Poll status every 3 seconds until online
	for i := 0; i < 60; i++ {
		time.Sleep(3 * time.Second)
		statusResp, err := sendRequest(map[string]interface{}{
			"mister": "tailscale",
			"action": "status",
		})
		if err != nil {
			continue
		}

		online, _ := statusResp["online"].(bool)
		if online {
			ip, _ := statusResp["ip"].(string)
			if jsonFlag {
				outputJSON(statusResp)
				return nil
			}
			fmt.Printf("Connected! IP: %s\n", ip)
			return nil
		}
	}

	return fmt.Errorf("timed out waiting for authentication")
}

func cmdTailscaleStatus() error {
	resp, err := sendRequest(map[string]interface{}{
		"mister": "tailscale",
		"action": "status",
	})
	if err != nil {
		return err
	}

	if jsonFlag {
		outputJSON(resp)
		return nil
	}

	running, _ := resp["running"].(bool)
	ip, _ := resp["ip"].(string)
	hostname, _ := resp["hostname"].(string)
	online, _ := resp["online"].(bool)

	state := "stopped"
	if running {
		state = "running"
	}
	fmt.Printf("Tailscale: %s\n", state)
	if ip != "" {
		fmt.Printf("IP: %s\n", ip)
	}
	if hostname != "" {
		fmt.Printf("Hostname: %s\n", hostname)
	}
	onlineStr := "no"
	if online {
		onlineStr = "yes"
	}
	fmt.Printf("Online: %s\n", onlineStr)
	return nil
}

func cmdTailscaleSimple(action string) error {
	resp, err := sendRequest(map[string]interface{}{
		"mister": "tailscale",
		"action": action,
	})
	if err != nil {
		return err
	}

	if jsonFlag {
		outputJSON(resp)
		return nil
	}

	fmt.Printf("Tailscale %s: OK\n", action)
	return nil
}

func cmdShell(args []string) error {
	command := strings.Join(args, " ")
	if command == "" {
		return fmt.Errorf("usage: clawexec-mister-fpga-send shell <command>")
	}

	output, exitCode, err := sendShellRequest(command)
	if err != nil {
		return err
	}

	if jsonFlag {
		resp := map[string]interface{}{
			"output":    output,
			"exit_code": exitCode,
		}
		outputJSON(resp)
		return nil
	}

	fmt.Print(output)
	if exitCode != 0 {
		os.Exit(exitCode)
	}
	return nil
}

func printHelp() {
	fmt.Print(`ClawExec for MiSTer-FPGA — Remote control for MiSTer-FPGA retro gaming platform.

QUICK START:
  clawexec-mister-fpga-send --host 100.92.156.99 status
  clawexec-mister-fpga-send launch "super mario world" --system SNES
  clawexec-mister-fpga-send search "zelda"
  clawexec-mister-fpga-send screenshot --output shot.png

COMMANDS:
  status        Show current core and game
  launch        Launch a game (by search or direct path)
  search        Search ROM library
  systems       List available systems and ROM counts
  screenshot    Take a screenshot (returns PNG)
  info          System information
  tailscale     Tailscale VPN management (setup/status/start/stop)
  shell         Execute shell command on MiSTer-FPGA
  discover      Scan local network for MiSTer-FPGA servers
  help          Show this help

GLOBAL FLAGS:
  --host, -H    MiSTer-FPGA host (default: "mister-fpga")
  --port, -p    Port (default: 9900)
  --json, -j    JSON output (for agent/script consumption)
  --timeout, -t Timeout in seconds (default: 10)

EXAMPLES:
  clawexec-mister-fpga-send status
  clawexec-mister-fpga-send status --json
  clawexec-mister-fpga-send launch "sonic 2" --system MegaDrive
  clawexec-mister-fpga-send launch --path "/media/usb0/SNES/Super Mario World (USA).sfc" --system SNES
  clawexec-mister-fpga-send search "zelda" --system SNES --limit 5
  clawexec-mister-fpga-send systems
  clawexec-mister-fpga-send screenshot --output game.png
  clawexec-mister-fpga-send info
  clawexec-mister-fpga-send tailscale setup
  clawexec-mister-fpga-send tailscale status
  clawexec-mister-fpga-send shell "ls /media/fat/games/"

AGENT NOTES:
  - Use --json for machine-parseable output on any command
  - Default host "mister-fpga" works if Tailscale is configured
  - On LAN, --host is usually not needed — auto-discovery finds the MiSTer
  - Launch accepts fuzzy search queries; use --system to narrow results
  - Screenshot returns base64 PNG to stdout without --output
  - Tailscale setup is fully automated: setup → auth URL → poll → IP returned
`)
}

// extractFlag pulls a named flag (--name or -short) and its value from args,
// returning the value and remaining positional args. Handles both "--flag value"
// and "--flag=value" forms.
func extractFlag(args []string, long, short string) (string, []string) {
	var value string
	var remaining []string
	for i := 0; i < len(args); i++ {
		a := args[i]
		// --flag=value
		if strings.HasPrefix(a, "--"+long+"=") {
			value = a[len("--"+long+"="):]
			continue
		}
		if short != "" && strings.HasPrefix(a, "-"+short+"=") {
			value = a[len("-"+short+"="):]
			continue
		}
		// --flag value
		if a == "--"+long || (short != "" && a == "-"+short) {
			if i+1 < len(args) {
				value = args[i+1]
				i++
			}
			continue
		}
		remaining = append(remaining, a)
	}
	return value, remaining
}

// BuildRequest constructs the JSON request for a given command and arguments.
// Exported for testing.
func BuildRequest(cmd string, args []string) (map[string]interface{}, error) {
	switch cmd {
	case "status":
		return map[string]interface{}{"mister": "status"}, nil
	case "systems":
		return map[string]interface{}{"mister": "systems"}, nil
	case "info":
		return map[string]interface{}{"mister": "info"}, nil
	case "screenshot":
		return map[string]interface{}{"mister": "screenshot"}, nil
	case "search":
		system, positional := extractFlag(args, "system", "s")
		query := strings.Join(positional, " ")
		if query == "" {
			return nil, fmt.Errorf("search requires a query")
		}
		req := map[string]interface{}{"mister": "search", "query": query}
		if system != "" {
			req["system"] = system
		}
		return req, nil
	case "launch":
		system, rest := extractFlag(args, "system", "s")
		path, positional := extractFlag(rest, "path", "")
		req := map[string]interface{}{"mister": "launch"}
		if path != "" {
			if system == "" {
				return nil, fmt.Errorf("--path requires --system")
			}
			req["path"] = path
			req["system"] = system
		} else {
			query := strings.Join(positional, " ")
			if query == "" {
				return nil, fmt.Errorf("launch requires a query or --path")
			}
			req["query"] = query
			if system != "" {
				req["system"] = system
			}
		}
		return req, nil
	case "tailscale":
		if len(args) == 0 {
			return nil, fmt.Errorf("tailscale requires an action")
		}
		return map[string]interface{}{"mister": "tailscale", "action": args[0]}, nil
	default:
		return nil, fmt.Errorf("unknown command: %s", cmd)
	}
}
