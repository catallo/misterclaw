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

const Version = "0.1.0"

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

	versionFlag := flag.Bool("version", false, "Print version and exit")
	flag.Usage = func() { printHelp() }
	flag.Parse()

	if *versionFlag {
		fmt.Printf("misterclaw-send v%s\n", Version)
		os.Exit(0)
	}

	args := flag.Args()
	if len(args) == 0 {
		printHelp()
		os.Exit(0)
	}

	cmd := args[0]
	cmdArgs := stripGlobalFlags(args[1:])

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
	case "input":
		err = cmdInput(cmdArgs)
	case "shell":
		err = cmdShell(cmdArgs)
	case "osd-info":
		err = cmdOSDInfo(cmdArgs)
	case "osd-visible":
		err = cmdOSDVisible(cmdArgs)
	case "cfg-read":
		err = cmdCFGRead(cmdArgs)
	case "cfg-write":
		err = cmdCFGWrite(cmdArgs)
	case "reload":
		err = cmdReload()
	case "rescan":
		err = cmdRescan(cmdArgs)
	case "osd-navigate":
		err = cmdOSDNavigate(cmdArgs)
	case "system-info":
		err = cmdSystemInfo(cmdArgs)
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
		"session": "misterclaw-cli",
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
			return fmt.Errorf("usage: misterclaw-send launch <query> [--system <system>]\n       misterclaw-send launch --path <path> --system <system>")
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

	if status, ok := resp["status"].(string); ok && status == "pending" {
		if msg, ok := resp["message"].(string); ok {
			fmt.Println(msg)
		}
		return nil
	}
	if errMsg, ok := resp["error"].(string); ok {
		return fmt.Errorf("%s", errMsg)
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
		return fmt.Errorf("usage: misterclaw-send search <query> [--system <system>] [--limit N]")
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
	if status, ok := resp["status"].(string); ok && status == "pending" {
		if msg, ok := resp["message"].(string); ok {
			fmt.Println(msg)
		}
		return nil
	}
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
		return fmt.Errorf("usage: misterclaw-send tailscale <setup|status|start|stop>")
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
		return fmt.Errorf("usage: misterclaw-send shell <command>")
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

func cmdInput(args []string) error {
	if len(args) == 0 {
		return fmt.Errorf("usage: misterclaw-send input <key|raw|combo|button|dpad> <value>")
	}

	mode := args[0]
	modeArgs := args[1:]

	var req map[string]interface{}
	switch mode {
	case "key":
		if len(modeArgs) == 0 {
			return fmt.Errorf("usage: misterclaw-send input key <name>\nNames: osd, menu, confirm, up, down, left, right, core_select, screenshot, reset, user, etc.")
		}
		req = map[string]interface{}{"mister": "input", "key": modeArgs[0]}
	case "raw":
		if len(modeArgs) == 0 {
			return fmt.Errorf("usage: misterclaw-send input raw <keycode>")
		}
		code, err := strconv.Atoi(modeArgs[0])
		if err != nil {
			return fmt.Errorf("invalid keycode: %s", modeArgs[0])
		}
		req = map[string]interface{}{"mister": "input", "raw": code}
	case "combo":
		if len(modeArgs) == 0 {
			return fmt.Errorf("usage: misterclaw-send input combo <key1> <key2> ...\nExample: misterclaw-send input combo leftalt f12")
		}
		req = map[string]interface{}{"mister": "input", "combo": modeArgs}
	case "button":
		if len(modeArgs) == 0 {
			return fmt.Errorf("usage: misterclaw-send input button <name>\nNames: a, b, x, y, start, select, l, r, coin")
		}
		req = map[string]interface{}{"mister": "input", "button": modeArgs[0]}
	case "dpad":
		if len(modeArgs) == 0 {
			return fmt.Errorf("usage: misterclaw-send input dpad <direction>\nDirections: up, down, left, right")
		}
		req = map[string]interface{}{"mister": "input", "dpad": modeArgs[0]}
	case "type":
		if len(modeArgs) == 0 {
			return fmt.Errorf("usage: misterclaw-send input type <text>\nTypes the text string character by character using correct keycodes.\nExample: misterclaw-send input type \"LOAD\\\"*\\\",8,1\"")
		}
		req = map[string]interface{}{"mister": "input", "text": strings.Join(modeArgs, " ")}
	default:
		return fmt.Errorf("unknown input mode: %s (use: key, raw, combo, button, dpad, type)", mode)
	}

	resp, err := sendRequest(req)
	if err != nil {
		return err
	}

	if jsonFlag {
		outputJSON(resp)
		return nil
	}

	fmt.Println("OK")
	return nil
}

func cmdOSDInfo(args []string) error {
	coreName, _ := extractFlag(args, "core", "c")

	req := map[string]interface{}{"mister": "osd_info"}
	if coreName != "" {
		req["core"] = coreName
	}

	resp, err := sendRequest(req)
	if err != nil {
		return err
	}

	if jsonFlag {
		outputJSON(resp)
		return nil
	}

	core, _ := resp["core_name"].(string)
	repo, _ := resp["repo"].(string)
	fmt.Printf("Core: %s (%s)\n\n", core, repo)

	menu, _ := resp["menu"].([]interface{})
	for _, m := range menu {
		item, ok := m.(map[string]interface{})
		if !ok {
			continue
		}
		typ, _ := item["type"].(string)
		name, _ := item["name"].(string)

		switch typ {
		case "separator":
			fmt.Println("  ────────────")
		case "label":
			fmt.Printf("  [%s]\n", name)
		case "option", "option_hidden":
			values, _ := item["values"].([]interface{})
			vals := make([]string, len(values))
			for i, v := range values {
				vals[i], _ = v.(string)
			}
			fmt.Printf("  Option: %s = [%s]\n", name, strings.Join(vals, ", "))
		case "trigger", "trigger_hidden":
			fmt.Printf("  Trigger: %s\n", name)
		case "file_load", "file_load_core":
			label, _ := item["label"].(string)
			exts, _ := item["extensions"].([]interface{})
			extStrs := make([]string, len(exts))
			for i, e := range exts {
				extStrs[i], _ = e.(string)
			}
			fmt.Printf("  File: %s (%s)\n", label, strings.Join(extStrs, ", "))
		case "mount":
			label, _ := item["label"].(string)
			fmt.Printf("  Mount: %s\n", label)
		case "sub_page":
			fmt.Printf("  Sub-page: %s\n", name)
		case "reset":
			fmt.Printf("  Reset: %s\n", name)
		case "joystick":
			fmt.Printf("  Joystick: %s\n", name)
		case "dip":
			fmt.Println("  DIP switches")
		default:
			raw, _ := item["raw"].(string)
			fmt.Printf("  %s: %s\n", typ, raw)
		}
	}
	return nil
}

func cmdOSDVisible(args []string) error {
	coreName, _ := extractFlag(args, "core", "c")

	req := map[string]interface{}{"mister": "osd_visible"}
	if coreName != "" {
		req["core"] = coreName
	}

	resp, err := sendRequest(req)
	if err != nil {
		return err
	}

	if jsonFlag {
		outputJSON(resp)
		return nil
	}

	core, _ := resp["core_name"].(string)
	fmt.Printf("Core: %s (visible items only)\n\n", core)

	menu, _ := resp["menu"].([]interface{})
	for _, m := range menu {
		item, ok := m.(map[string]interface{})
		if !ok {
			continue
		}
		typ, _ := item["type"].(string)
		name, _ := item["name"].(string)

		switch typ {
		case "separator":
			fmt.Println("  ────────────")
		case "label":
			fmt.Printf("  [%s]\n", name)
		case "option", "option_hidden":
			values, _ := item["values"].([]interface{})
			vals := make([]string, len(values))
			for i, v := range values {
				vals[i], _ = v.(string)
			}
			fmt.Printf("  Option: %s = [%s]\n", name, strings.Join(vals, ", "))
		case "trigger", "trigger_hidden":
			fmt.Printf("  Trigger: %s\n", name)
		case "file_load", "file_load_core":
			label, _ := item["label"].(string)
			fmt.Printf("  File: %s\n", label)
		default:
			fmt.Printf("  %s: %s\n", typ, name)
		}
	}
	return nil
}

func cmdCFGRead(args []string) error {
	coreName, _ := extractFlag(args, "core", "c")

	req := map[string]interface{}{"mister": "cfg_read"}
	if coreName != "" {
		req["core"] = coreName
	}

	resp, err := sendRequest(req)
	if err != nil {
		return err
	}

	if jsonFlag {
		outputJSON(resp)
		return nil
	}

	core, _ := resp["core_name"].(string)
	dipPath, _ := resp["dip_path"].(string)
	cfgPath, _ := resp["cfg_path"].(string)

	fmt.Printf("Core: %s\n", core)
	fmt.Printf("CFG:  %s\n", cfgPath)
	if dipPath != "" {
		fmt.Printf("DIP:  %s\n", dipPath)
	}
	fmt.Println()

	settings, _ := resp["settings"].([]interface{})
	lastSource := ""
	for _, o := range settings {
		opt, ok := o.(map[string]interface{})
		if !ok {
			continue
		}
		source, _ := opt["source"].(string)
		if source != lastSource {
			if lastSource != "" {
				fmt.Println()
			}
			if source == "cfg" {
				fmt.Println("Core Options:")
			} else if source == "dip" {
				fmt.Println("DIP Switches:")
			}
			lastSource = source
		}
		name, _ := opt["name"].(string)
		val, _ := opt["value"].(float64)
		valName, _ := opt["value_name"].(string)
		if valName != "" {
			fmt.Printf("  %-24s = %s (%d)\n", name, valName, int(val))
		} else {
			fmt.Printf("  %-24s = %d\n", name, int(val))
		}
	}
	return nil
}

func cmdCFGWrite(args []string) error {
	optionName, rest := extractFlag(args, "option", "o")
	valueName, _ := extractFlag(rest, "value", "v")

	if optionName == "" || valueName == "" {
		return fmt.Errorf("usage: misterclaw-send cfg-write --option <name> --value <value>")
	}

	resp, err := sendRequest(map[string]interface{}{
		"mister": "cfg_write",
		"option": optionName,
		"value":  valueName,
	})
	if err != nil {
		return err
	}

	if jsonFlag {
		outputJSON(resp)
		return nil
	}

	option, _ := resp["option"].(string)
	value, _ := resp["value"].(string)
	cfgPath, _ := resp["cfg_path"].(string)
	fmt.Printf("Set %s = %s\n", option, value)
	fmt.Printf("Written to: %s (backup created)\n", cfgPath)
	if reload, ok := resp["reload_required"].(bool); ok && reload {
		fmt.Println("(reload core to apply)")
	}
	return nil
}

func cmdReload() error {
	resp, err := sendRequest(map[string]interface{}{"mister": "reload"})
	if err != nil {
		return err
	}

	if jsonFlag {
		outputJSON(resp)
		return nil
	}

	path, _ := resp["path"].(string)
	fmt.Printf("Reloaded: %s\n", path)
	return nil
}

func cmdRescan(args []string) error {
	location, _ := extractFlag(args, "location", "l")

	req := map[string]interface{}{"mister": "rescan"}
	if location != "" {
		req["location"] = location
	}

	resp, err := sendRequest(req)
	if err != nil {
		return err
	}

	if jsonFlag {
		outputJSON(resp)
		return nil
	}

	if msg, ok := resp["message"].(string); ok {
		fmt.Println(msg)
	}
	if sf, ok := resp["systems_found"].(float64); ok && sf > 0 {
		loc, _ := resp["location"].(string)
		fmt.Printf("Rescan complete: %d systems found (location: %s)\n", int(sf), loc)
	}
	return nil
}

func cmdOSDNavigate(args []string) error {
	target := strings.Join(args, " ")
	if target == "" {
		return fmt.Errorf("usage: misterclaw-send osd-navigate <target>\nExample: misterclaw-send osd-navigate Reset")
	}

	resp, err := sendRequest(map[string]interface{}{
		"mister": "osd_navigate",
		"target": target,
	})
	if err != nil {
		return err
	}

	if jsonFlag {
		outputJSON(resp)
		return nil
	}

	tgt, _ := resp["target"].(string)
	core, _ := resp["core"].(string)
	fmt.Printf("Navigated to: %s", tgt)
	if core != "" {
		fmt.Printf(" (core: %s)", core)
	}
	fmt.Println()
	return nil
}

func cmdSystemInfo(args []string) error {
	system := strings.Join(args, " ")
	if system == "" {
		return fmt.Errorf("usage: misterclaw-send system-info <system>\nExample: misterclaw-send system-info PC8801")
	}

	resp, err := sendRequest(map[string]interface{}{
		"mister": "system_info",
		"system": system,
	})
	if err != nil {
		return err
	}

	if jsonFlag {
		outputJSON(resp)
		return nil
	}

	fmt.Printf("System: %s\n", system)

	if config, ok := resp["config"].(map[string]interface{}); ok {
		if core, ok := config["core"].(string); ok {
			fmt.Printf("Core: %s\n", core)
		}
		if romType, ok := config["type"].(string); ok {
			fmt.Printf("ROM type: %s\n", romType)
		}
		if index, ok := config["index"].(float64); ok {
			fmt.Printf("Index: %d\n", int(index))
		}
		if exts, ok := config["extensions"].([]interface{}); ok && len(exts) > 0 {
			extStrs := make([]string, len(exts))
			for i, e := range exts {
				extStrs[i], _ = e.(string)
			}
			fmt.Printf("Extensions: %s\n", strings.Join(extStrs, ", "))
		}
	}

	if notes, ok := resp["notes"].(string); ok && notes != "" {
		fmt.Printf("\nNotes:\n%s\n", notes)
	}

	if coreName, ok := resp["core_name"].(string); ok && coreName != "" {
		fmt.Printf("\nOSD Core: %s\n", coreName)
	}

	if menu, ok := resp["menu"].([]interface{}); ok && len(menu) > 0 {
		fmt.Println("\nOSD Menu:")
		for _, m := range menu {
			item, ok := m.(map[string]interface{})
			if !ok {
				continue
			}
			typ, _ := item["type"].(string)
			name, _ := item["name"].(string)

			switch typ {
			case "separator":
				fmt.Println("  ────────────")
			case "label":
				fmt.Printf("  [%s]\n", name)
			case "option", "option_hidden":
				values, _ := item["values"].([]interface{})
				vals := make([]string, len(values))
				for i, v := range values {
					vals[i], _ = v.(string)
				}
				fmt.Printf("  Option: %s = [%s]\n", name, strings.Join(vals, ", "))
			case "trigger", "trigger_hidden":
				fmt.Printf("  Trigger: %s\n", name)
			case "file_load", "file_load_core":
				label, _ := item["label"].(string)
				exts, _ := item["extensions"].([]interface{})
				extStrs := make([]string, len(exts))
				for i, e := range exts {
					extStrs[i], _ = e.(string)
				}
				fmt.Printf("  File: %s (%s)\n", label, strings.Join(extStrs, ", "))
			case "mount":
				label, _ := item["label"].(string)
				fmt.Printf("  Mount: %s\n", label)
			case "reset":
				fmt.Printf("  Reset: %s\n", name)
			default:
				raw, _ := item["raw"].(string)
				fmt.Printf("  %s: %s\n", typ, raw)
			}
		}
	}
	return nil
}

func printHelp() {
	fmt.Print(`MisterClaw — Remote control for MiSTer-FPGA retro gaming platform.

QUICK START:
  misterclaw-send --host 100.92.156.99 status
  misterclaw-send launch "super mario world" --system SNES
  misterclaw-send search "zelda"
  misterclaw-send screenshot --output shot.png

COMMANDS:
  status        Show current core and game
  launch        Launch a game (by search or direct path)
  search        Search ROM library
  systems       List available systems and ROM counts
  screenshot    Take a screenshot (returns PNG)
  info          System information
  input         Send input (key/raw/combo)
  osd-info      Show OSD menu structure for current or specified core
  osd-visible   Show only visible OSD menu items (based on CFG state)
  osd-navigate  Navigate to a specific OSD menu item by name (experimental)
  system-info   Get system config, notes, and OSD menu for a system
  cfg-read      Read current CFG file and decode option values
  cfg-write     Set a core option by name (with automatic backup)
  reload        Reload current core (apply config changes)
  rescan        Rescan ROM library (optionally for specific location)
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
  misterclaw-send status
  misterclaw-send status --json
  misterclaw-send launch "sonic 2" --system MegaDrive
  misterclaw-send launch --path "/media/usb0/SNES/Super Mario World (USA).sfc" --system SNES
  misterclaw-send search "zelda" --system SNES --limit 5
  misterclaw-send systems
  misterclaw-send screenshot --output game.png
  misterclaw-send info
  misterclaw-send tailscale setup
  misterclaw-send tailscale status
  misterclaw-send input key osd
  misterclaw-send input raw 28
  misterclaw-send input combo leftalt f12
  misterclaw-send osd-info
  misterclaw-send osd-info --core SNES
  misterclaw-send osd-visible
  misterclaw-send osd-navigate Reset
  misterclaw-send osd-navigate "Aspect ratio"
  misterclaw-send system-info PC8801
  misterclaw-send system-info SNES
  misterclaw-send cfg-read
  misterclaw-send cfg-write --option "Free Play" --value On
  misterclaw-send -H mister-fpga reload
  misterclaw-send shell "ls /media/fat/games/"

AGENT NOTES:
  - Use --json for machine-parseable output on any command
  - Default host "mister-fpga" works if Tailscale is configured
  - On LAN, --host is usually not needed — auto-discovery finds the MiSTer
  - Launch accepts fuzzy search queries; use --system to narrow results
  - Screenshot returns base64 PNG to stdout without --output
  - Tailscale setup is fully automated: setup → auth URL → poll → IP returned
`)
}


// stripGlobalFlags removes global flags (--json, -j, --timeout, -t) from subcommand args.
// This handles the case where users place global flags after the subcommand name.
func stripGlobalFlags(args []string) []string {
	var clean []string
	for i := 0; i < len(args); i++ {
		a := args[i]
		if a == "--json" || a == "-j" {
			jsonFlag = true
			continue
		}
		if a == "--timeout" || a == "-t" {
			if i+1 < len(args) {
				if n, err := strconv.Atoi(args[i+1]); err == nil {
					timeoutFlag = n
				}
				i++
			}
			continue
		}
		clean = append(clean, a)
	}
	return clean
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
	case "input":
		if len(args) == 0 {
			return nil, fmt.Errorf("input requires a mode: key, raw, combo, button, or dpad")
		}
		mode := args[0]
		modeArgs := args[1:]
		switch mode {
		case "key":
			if len(modeArgs) == 0 {
				return nil, fmt.Errorf("input key requires a key name")
			}
			return map[string]interface{}{"mister": "input", "key": modeArgs[0]}, nil
		case "raw":
			if len(modeArgs) == 0 {
				return nil, fmt.Errorf("input raw requires a keycode")
			}
			code, err := strconv.Atoi(modeArgs[0])
			if err != nil {
				return nil, fmt.Errorf("invalid keycode: %s", modeArgs[0])
			}
			return map[string]interface{}{"mister": "input", "raw": code}, nil
		case "combo":
			if len(modeArgs) == 0 {
				return nil, fmt.Errorf("input combo requires at least one key")
			}
			return map[string]interface{}{"mister": "input", "combo": modeArgs}, nil
		case "button":
			if len(modeArgs) == 0 {
				return nil, fmt.Errorf("input button requires a button name")
			}
			return map[string]interface{}{"mister": "input", "button": modeArgs[0]}, nil
		case "dpad":
			if len(modeArgs) == 0 {
				return nil, fmt.Errorf("input dpad requires a direction")
			}
			return map[string]interface{}{"mister": "input", "dpad": modeArgs[0]}, nil
		default:
			return nil, fmt.Errorf("unknown input mode: %s", mode)
		}
	case "osd-info":
		coreName, _ := extractFlag(args, "core", "c")
		req := map[string]interface{}{"mister": "osd_info"}
		if coreName != "" {
			req["core"] = coreName
		}
		return req, nil
	case "osd-visible":
		coreName, _ := extractFlag(args, "core", "c")
		req := map[string]interface{}{"mister": "osd_visible"}
		if coreName != "" {
			req["core"] = coreName
		}
		return req, nil
	case "osd-navigate":
		target := strings.Join(args, " ")
		if target == "" {
			return nil, fmt.Errorf("osd-navigate requires a target")
		}
		return map[string]interface{}{"mister": "osd_navigate", "target": target}, nil
	case "system-info":
		system := strings.Join(args, " ")
		if system == "" {
			return nil, fmt.Errorf("system-info requires a system name")
		}
		return map[string]interface{}{"mister": "system_info", "system": system}, nil
	case "cfg-read":
		coreName, _ := extractFlag(args, "core", "c")
		req := map[string]interface{}{"mister": "cfg_read"}
		if coreName != "" {
			req["core"] = coreName
		}
		return req, nil
	case "cfg-write":
		optionName, rest := extractFlag(args, "option", "o")
		valueName, _ := extractFlag(rest, "value", "v")
		if optionName == "" || valueName == "" {
			return nil, fmt.Errorf("cfg-write requires --option and --value")
		}
		return map[string]interface{}{
			"mister": "cfg_write",
			"option": optionName,
			"value":  valueName,
		}, nil
	case "reload":
		return map[string]interface{}{"mister": "reload"}, nil
	case "rescan":
		location, _ := extractFlag(args, "location", "l")
		req := map[string]interface{}{"mister": "rescan"}
		if location != "" {
			req["location"] = location
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
