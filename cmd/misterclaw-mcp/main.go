package main

import (
	"bufio"
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"net"
	"os"
	"strconv"
	"strings"
	"time"
)

var (
	host string
	port int
)

// JSONRPCRequest represents an incoming JSON-RPC 2.0 request.
type JSONRPCRequest struct {
	JSONRPC string           `json:"jsonrpc"`
	ID      *json.RawMessage `json:"id,omitempty"`
	Method  string           `json:"method"`
	Params  json.RawMessage  `json:"params,omitempty"`
}

// JSONRPCResponse represents an outgoing JSON-RPC 2.0 response.
type JSONRPCResponse struct {
	JSONRPC string      `json:"jsonrpc"`
	ID      interface{} `json:"id"`
	Result  interface{} `json:"result,omitempty"`
	Error   interface{} `json:"error,omitempty"`
}

// ToolDef defines an MCP tool for tools/list.
type ToolDef struct {
	Name        string      `json:"name"`
	Description string      `json:"description"`
	InputSchema interface{} `json:"inputSchema"`
}

// MCPContent represents a content block in an MCP tool result.
type MCPContent struct {
	Type     string `json:"type"`
	Text     string `json:"text,omitempty"`
	Data     string `json:"data,omitempty"`
	MimeType string `json:"mimeType,omitempty"`
}

// MCPToolResult is the result of a tools/call.
type MCPToolResult struct {
	Content []MCPContent `json:"content"`
	IsError bool         `json:"isError,omitempty"`
}

func main() {
	flag.StringVar(&host, "host", "mister-fpga", "MiSTer-FPGA host")
	flag.IntVar(&port, "port", 9900, "MiSTer-FPGA port")
	flag.Parse()

	log.SetOutput(os.Stderr)
	log.SetPrefix("[misterclaw-mcp] ")

	scanner := bufio.NewScanner(os.Stdin)
	scanner.Buffer(make([]byte, 64*1024), 4*1024*1024)

	for scanner.Scan() {
		line := scanner.Bytes()
		if len(line) == 0 {
			continue
		}

		var req JSONRPCRequest
		if err := json.Unmarshal(line, &req); err != nil {
			writeError(nil, -32700, "Parse error")
			continue
		}

		resp := handleRequest(req)
		if resp == nil {
			// Notification — no response
			continue
		}

		data, _ := json.Marshal(resp)
		fmt.Fprintf(os.Stdout, "%s\n", data)
	}
}

func handleRequest(req JSONRPCRequest) *JSONRPCResponse {
	var id interface{}
	if req.ID != nil {
		json.Unmarshal(*req.ID, &id)
	}

	switch req.Method {
	case "initialize":
		return &JSONRPCResponse{
			JSONRPC: "2.0",
			ID:      id,
			Result: map[string]interface{}{
				"protocolVersion": "2024-11-05",
				"capabilities":    map[string]interface{}{"tools": map[string]interface{}{}},
				"serverInfo": map[string]interface{}{
					"name":    "misterclaw-mcp",
					"version": "0.1.0",
				},
			},
		}

	case "notifications/initialized":
		return nil

	case "tools/list":
		return &JSONRPCResponse{
			JSONRPC: "2.0",
			ID:      id,
			Result: map[string]interface{}{
				"tools": toolsList(),
			},
		}

	case "tools/call":
		result := callTool(req.Params)
		return &JSONRPCResponse{
			JSONRPC: "2.0",
			ID:      id,
			Result:  result,
		}

	default:
		return &JSONRPCResponse{
			JSONRPC: "2.0",
			ID:      id,
			Error: map[string]interface{}{
				"code":    -32601,
				"message": "Method not found",
			},
		}
	}
}

func writeError(id interface{}, code int, message string) {
	resp := JSONRPCResponse{
		JSONRPC: "2.0",
		ID:      id,
		Error: map[string]interface{}{
			"code":    code,
			"message": message,
		},
	}
	data, _ := json.Marshal(resp)
	fmt.Fprintf(os.Stdout, "%s\n", data)
}

func toolsList() []ToolDef {
	return []ToolDef{
		{
			Name:        "mister_status",
			Description: "Get the current status of MiSTer-FPGA, including which core is loaded and what game (ROM) is running. Use this to check what's currently playing before launching something new.",
			InputSchema: map[string]interface{}{
				"type":       "object",
				"properties": map[string]interface{}{},
			},
		},
		{
			Name:        "mister_launch",
			Description: "Launch a game on MiSTer-FPGA by search query or direct ROM path. Use 'query' for fuzzy search (e.g. 'sonic 2') or 'path' for exact ROM path. Optionally filter by 'system' (e.g. 'SNES', 'MegaDrive', 'PSX'). Either 'query' or 'path' is required.",
			InputSchema: map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"query":  map[string]interface{}{"type": "string", "description": "Fuzzy search query (e.g. 'super mario world')"},
					"system": map[string]interface{}{"type": "string", "description": "System filter (e.g. 'SNES', 'MegaDrive', 'PSX', 'NES', 'GBA')"},
					"path":   map[string]interface{}{"type": "string", "description": "Direct ROM file path (requires 'system' to also be set)"},
				},
			},
		},
		{
			Name:        "mister_search",
			Description: "Search the MiSTer-FPGA ROM library by name. Returns matching games with name, path, system, and storage location. Use 'system' to filter by platform. Results are fuzzy-matched and ranked by relevance.",
			InputSchema: map[string]interface{}{
				"type":     "object",
				"required": []string{"query"},
				"properties": map[string]interface{}{
					"query":  map[string]interface{}{"type": "string", "description": "Search query (e.g. 'zelda', 'street fighter')"},
					"system": map[string]interface{}{"type": "string", "description": "System filter (e.g. 'SNES', 'MegaDrive', 'PSX')"},
					"limit":  map[string]interface{}{"type": "number", "description": "Maximum results to return (default: 20)"},
				},
			},
		},
		{
			Name:        "mister_systems",
			Description: "List all available systems (consoles/computers) on MiSTer-FPGA with their ROM counts and storage locations. Use this to discover what platforms are available before searching or launching games.",
			InputSchema: map[string]interface{}{
				"type":       "object",
				"properties": map[string]interface{}{},
			},
		},
		{
			Name:        "mister_screenshot",
			Description: "Take a screenshot of what's currently displayed on MiSTer-FPGA. Returns the screenshot as a PNG image. Useful for seeing what game is running or verifying a launch succeeded.",
			InputSchema: map[string]interface{}{
				"type":       "object",
				"properties": map[string]interface{}{},
			},
		},
		{
			Name:        "mister_info",
			Description: "Get MiSTer-FPGA system information including hostname, IP address, CPU temperature, RAM usage, disk space for all mounted volumes, and uptime. Useful for monitoring device health.",
			InputSchema: map[string]interface{}{
				"type":       "object",
				"properties": map[string]interface{}{},
			},
		},
		{
			Name:        "mister_tailscale",
			Description: "Manage Tailscale VPN on MiSTer-FPGA. Actions: 'setup' installs and configures Tailscale (returns auth URL if needed), 'status' shows connection state and IP, 'start' starts the daemon, 'stop' stops it. Tailscale enables secure remote access from anywhere.",
			InputSchema: map[string]interface{}{
				"type":     "object",
				"required": []string{"action"},
				"properties": map[string]interface{}{
					"action":   map[string]interface{}{"type": "string", "description": "Action to perform: 'setup', 'status', 'start', or 'stop'", "enum": []string{"setup", "status", "start", "stop"}},
					"hostname": map[string]interface{}{"type": "string", "description": "Custom hostname for Tailscale (only used with 'setup')"},
				},
			},
		},
		{
			Name:        "mister_input",
			Description: "Send input to MiSTer-FPGA via virtual keyboard. Use 'key' for named keyboard keys (osd, menu, confirm, up, down, left, right, coin, start). Use 'raw' for Linux keycodes. Use 'combo' for key combinations (e.g. ['leftalt', 'f12']).",
			InputSchema: map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"key":    map[string]interface{}{"type": "string", "description": "Named keyboard key to press (e.g. 'osd', 'menu', 'confirm', 'core_select')"},
					"raw":    map[string]interface{}{"type": "number", "description": "Raw Linux keycode to press (e.g. 28 for Enter)"},
					"combo":  map[string]interface{}{"type": "array", "items": map[string]interface{}{"type": "string"}, "description": "Key combination to press (e.g. ['leftalt', 'f12'])"},
					"button": map[string]interface{}{"type": "string", "description": "Gamepad button to press (a, b, x, y, start, select, l, r, coin)"},
					"dpad":   map[string]interface{}{"type": "string", "description": "Gamepad d-pad direction (up, down, left, right)"},
				},
			},
		},
		{
			Name:        "mister_shell",
			Description: "Execute a shell command on MiSTer-FPGA and return the output. Use for file operations, system administration, or any task not covered by other tools. Commands run as the MiSTer user with full system access.",
			InputSchema: map[string]interface{}{
				"type":     "object",
				"required": []string{"command"},
				"properties": map[string]interface{}{
					"command": map[string]interface{}{"type": "string", "description": "Shell command to execute (e.g. 'ls /media/fat/games/', 'df -h')"},
				},
			},
		},
		{
			Name:        "mister_osd_info",
			Description: "Get the OSD (On-Screen Display) menu structure for the currently loaded core or a specified core. Returns the parsed CONF_STR menu items including options, triggers, file loaders, and sub-pages. Useful for understanding what settings a core supports and what the OSD menu looks like.",
			InputSchema: map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"core": map[string]interface{}{"type": "string", "description": "Core name to look up (e.g. 'SNES', 'Genesis', 'NES'). If omitted, uses the currently loaded core."},
				},
			},
		},
		{
			Name:        "mister_osd_visible",
			Description: "Get only the visible OSD menu items for the current core, based on the current CFG state. This shows exactly what the user would see on screen — items hidden by H/h flags are filtered out. Use this instead of osd_info when you want to know what options are actually available right now.",
			InputSchema: map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"core": map[string]interface{}{"type": "string", "description": "Core name. If omitted, uses the currently loaded core."},
				},
			},
		},
		{
			Name:        "mister_cfg_read",
			Description: "Read the current CFG file for the running core/game. Returns the raw hex data and decoded option values (e.g. 'Aspect Ratio = Original'). CFG files store per-game/core settings as bit fields.",
			InputSchema: map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"core": map[string]interface{}{"type": "string", "description": "Core name. If omitted, uses the currently loaded core."},
				},
			},
		},
		{
			Name:        "mister_cfg_write",
			Description: "Set a core option by name and value. Automatically backs up the CFG file before writing. Use cfg_read first to see available options and their current values. Example: option='Free Play', value='On'. After writing, use mister_reload to apply changes.",
			InputSchema: map[string]interface{}{
				"type":     "object",
				"required": []string{"option", "value"},
				"properties": map[string]interface{}{
					"option": map[string]interface{}{"type": "string", "description": "Option name (e.g. 'Aspect Ratio', 'Free Play', 'Region')"},
					"value":  map[string]interface{}{"type": "string", "description": "Value name (e.g. 'Original', 'On', 'US')"},
				},
			},
		},
		{
			Name:        "mister_reload",
			Description: "Reload the current core on MiSTer-FPGA. Use after changing settings with mister_cfg_write to apply changes.",
			InputSchema: map[string]interface{}{
				"type":       "object",
				"properties": map[string]interface{}{},
			},
		},
		{
			Name:        "mister_rescan",
			Description: "Rescan ROM library to detect new games. Optionally specify location (sd, usb0, usb1, ...) to scan only one drive.",
			InputSchema: map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"location": map[string]interface{}{"type": "string", "description": "Storage location to rescan (e.g. 'sd', 'usb0', 'usb1'). If omitted, rescans all locations."},
				},
			},
		},
		{
			Name:        "mister_osd_navigate",
			Description: "Navigate to a specific OSD menu item by name (experimental — not yet reliable for all cores). Opens the OSD (F12) and navigates to the target item using conf_str-based position calculation. Works for Reset, options, file mounts, triggers etc. Uses the currently loaded core. Note: cores with runtime-hidden items may cause incorrect positioning.",
			InputSchema: map[string]interface{}{
				"type":     "object",
				"required": []string{"target"},
				"properties": map[string]interface{}{
					"target": map[string]interface{}{"type": "string", "description": "Menu item name to navigate to (e.g. 'Reset', 'FDD0', 'Aspect ratio')"},
				},
			},
		},
		{
			Name:        "mister_system_info",
			Description: "Get detailed system information including core config, keyboard mapping notes, and full OSD menu structure parsed from conf_str. Useful for understanding core capabilities before interacting.",
			InputSchema: map[string]interface{}{
				"type":     "object",
				"required": []string{"system"},
				"properties": map[string]interface{}{
					"system": map[string]interface{}{"type": "string", "description": "System name (e.g. 'PC8801', 'SNES', 'Genesis')"},
				},
			},
		},
	}
}

func callTool(params json.RawMessage) MCPToolResult {
	var p struct {
		Name      string          `json:"name"`
		Arguments json.RawMessage `json:"arguments"`
	}
	if err := json.Unmarshal(params, &p); err != nil {
		return errorResult("invalid tool call parameters")
	}

	var args map[string]interface{}
	if len(p.Arguments) > 0 {
		json.Unmarshal(p.Arguments, &args)
	}
	if args == nil {
		args = map[string]interface{}{}
	}

	switch p.Name {
	case "mister_status":
		return doMisterCommand(map[string]interface{}{"mister": "status"}, formatStatus)

	case "mister_launch":
		req := map[string]interface{}{"mister": "launch"}
		if v, ok := args["query"].(string); ok && v != "" {
			req["query"] = v
		}
		if v, ok := args["system"].(string); ok && v != "" {
			req["system"] = v
		}
		if v, ok := args["path"].(string); ok && v != "" {
			req["path"] = v
		}
		return doMisterCommand(req, formatLaunch)

	case "mister_search":
		req := map[string]interface{}{"mister": "search"}
		if v, ok := args["query"].(string); ok {
			req["query"] = v
		}
		if v, ok := args["system"].(string); ok && v != "" {
			req["system"] = v
		}
		limit := 20
		if v, ok := args["limit"].(float64); ok && v > 0 {
			limit = int(v)
		}
		return doMisterCommandWithLimit(req, limit, formatSearch)

	case "mister_systems":
		return doMisterCommand(map[string]interface{}{"mister": "systems"}, formatSystems)

	case "mister_screenshot":
		return doScreenshot()

	case "mister_info":
		return doMisterCommand(map[string]interface{}{"mister": "info"}, formatInfo)

	case "mister_tailscale":
		req := map[string]interface{}{"mister": "tailscale"}
		if v, ok := args["action"].(string); ok {
			req["action"] = v
		}
		if v, ok := args["hostname"].(string); ok && v != "" {
			req["hostname"] = v
		}
		return doMisterCommand(req, formatTailscale)

	case "mister_input":
		req := map[string]interface{}{"mister": "input"}
		if v, ok := args["button"].(string); ok && v != "" {
			req["button"] = v
		} else if v, ok := args["dpad"].(string); ok && v != "" {
			req["dpad"] = v
		} else if v, ok := args["key"].(string); ok && v != "" {
			req["key"] = v
		} else if v, ok := args["raw"].(float64); ok {
			req["raw"] = int(v)
		} else if v, ok := args["combo"].([]interface{}); ok && len(v) > 0 {
			combo := make([]string, len(v))
			for i, k := range v {
				combo[i], _ = k.(string)
			}
			req["combo"] = combo
		} else {
			return errorResult("one of key, raw, combo, button, or dpad is required")
		}
		return doMisterCommand(req, formatInput)

	case "mister_shell":
		cmd, _ := args["command"].(string)
		if cmd == "" {
			return errorResult("command is required")
		}
		return doShellCommand(cmd)

	case "mister_osd_info":
		req := map[string]interface{}{"mister": "osd_info"}
		if v, ok := args["core"].(string); ok && v != "" {
			req["core"] = v
		}
		return doMisterCommand(req, formatOSDInfo)

	case "mister_osd_visible":
		req := map[string]interface{}{"mister": "osd_visible"}
		if v, ok := args["core"].(string); ok && v != "" {
			req["core"] = v
		}
		return doMisterCommand(req, formatOSDVisible)

	case "mister_cfg_read":
		req := map[string]interface{}{"mister": "cfg_read"}
		if v, ok := args["core"].(string); ok && v != "" {
			req["core"] = v
		}
		return doMisterCommand(req, formatCFGRead)

	case "mister_cfg_write":
		option, _ := args["option"].(string)
		value, _ := args["value"].(string)
		if option == "" || value == "" {
			return errorResult("option and value are required")
		}
		req := map[string]interface{}{
			"mister": "cfg_write",
			"option": option,
			"value":  value,
		}
		return doMisterCommand(req, formatCFGWrite)

	case "mister_reload":
		return doMisterCommand(map[string]interface{}{"mister": "reload"}, formatReload)

	case "mister_rescan":
		req := map[string]interface{}{"mister": "rescan"}
		if v, ok := args["location"].(string); ok && v != "" {
			req["location"] = v
		}
		return doMisterCommand(req, formatRescan)

	case "mister_osd_navigate":
		target, _ := args["target"].(string)
		if target == "" {
			return errorResult("target is required")
		}
		req := map[string]interface{}{
			"mister": "osd_navigate",
			"target": target,
		}
		return doMisterCommand(req, formatOSDNavigate)

	case "mister_system_info":
		system, _ := args["system"].(string)
		if system == "" {
			return errorResult("system is required")
		}
		req := map[string]interface{}{
			"mister": "system_info",
			"system": system,
		}
		return doMisterCommand(req, formatSystemInfo)

	default:
		return errorResult(fmt.Sprintf("unknown tool: %s", p.Name))
	}
}

// sendMisterCommand connects to the MiSTer, sends a JSON request, reads one response.
func sendMisterCommand(request map[string]interface{}) (map[string]interface{}, error) {
	addr := net.JoinHostPort(host, strconv.Itoa(port))
	conn, err := net.DialTimeout("tcp", addr, 10*time.Second)
	if err != nil {
		return nil, fmt.Errorf("connecting to %s: %w", addr, err)
	}
	defer conn.Close()

	conn.SetDeadline(time.Now().Add(30 * time.Second))

	if err := json.NewEncoder(conn).Encode(request); err != nil {
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

// sendShellCommand sends a shell command and collects streamed output until done.
func sendShellCommand(command string) (string, int, error) {
	addr := net.JoinHostPort(host, strconv.Itoa(port))
	conn, err := net.DialTimeout("tcp", addr, 10*time.Second)
	if err != nil {
		return "", 1, fmt.Errorf("connecting to %s: %w", addr, err)
	}
	defer conn.Close()

	req := map[string]interface{}{
		"cmd":     command,
		"session": "mcp",
		"pty":     false,
	}
	if err := json.NewEncoder(conn).Encode(req); err != nil {
		return "", 1, fmt.Errorf("sending request: %w", err)
	}

	var output strings.Builder
	exitCode := 0
	dec := json.NewDecoder(conn)

	for {
		conn.SetDeadline(time.Now().Add(30 * time.Second))
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

// Tool execution helpers

func doMisterCommand(req map[string]interface{}, format func(map[string]interface{}) MCPToolResult) MCPToolResult {
	resp, err := sendMisterCommand(req)
	if err != nil {
		return errorResult(err.Error())
	}
	return format(resp)
}

func doMisterCommandWithLimit(req map[string]interface{}, limit int, format func(map[string]interface{}, int) MCPToolResult) MCPToolResult {
	resp, err := sendMisterCommand(req)
	if err != nil {
		return errorResult(err.Error())
	}
	return format(resp, limit)
}

func doScreenshot() MCPToolResult {
	resp, err := sendMisterCommand(map[string]interface{}{"mister": "screenshot"})
	if err != nil {
		return errorResult(err.Error())
	}

	data, _ := resp["data"].(string)
	if data == "" {
		return errorResult("no screenshot data received")
	}

	core, _ := resp["core"].(string)
	content := []MCPContent{
		{Type: "image", Data: data, MimeType: "image/png"},
	}
	if core != "" {
		content = append(content, MCPContent{Type: "text", Text: fmt.Sprintf("Screenshot captured (core: %s)", core)})
	}
	return MCPToolResult{Content: content}
}

func doShellCommand(command string) MCPToolResult {
	output, exitCode, err := sendShellCommand(command)
	if err != nil {
		return errorResult(err.Error())
	}

	text := output
	if exitCode != 0 {
		text += fmt.Sprintf("\n[exit code: %d]", exitCode)
	}
	return textResult(text)
}

// Response formatters

func formatStatus(resp map[string]interface{}) MCPToolResult {
	coreName, _ := resp["core_name"].(string)
	gamePath, _ := resp["game_path"].(string)

	if coreName == "" {
		coreName = "(none)"
	}

	text := fmt.Sprintf("Core: %s", coreName)
	if gamePath != "" {
		text += fmt.Sprintf("\nGame: %s", gamePath)
	}
	return textResult(text)
}

func formatLaunch(resp map[string]interface{}) MCPToolResult {
	if success, ok := resp["success"].(bool); ok && !success {
		errMsg, _ := resp["error"].(string)
		return errorResult(fmt.Sprintf("Launch failed: %s", errMsg))
	}
	game, _ := resp["game"].(string)
	return textResult(fmt.Sprintf("Launched: %s", game))
}

func formatSearch(resp map[string]interface{}, limit int) MCPToolResult {
	if status, ok := resp["status"].(string); ok && status == "pending" {
		msg, _ := resp["message"].(string)
		return textResult(msg)
	}

	results, _ := resp["results"].([]interface{})
	total, _ := resp["total"].(float64)

	var sb strings.Builder
	shown := 0
	for i, r := range results {
		if shown >= limit {
			break
		}
		if game, ok := r.(map[string]interface{}); ok {
			name, _ := game["name"].(string)
			sys, _ := game["system"].(string)
			path, _ := game["path"].(string)
			loc, _ := game["location"].(string)
			sb.WriteString(fmt.Sprintf("%d. %s [%s, %s] %s\n", i+1, name, sys, loc, path))
			shown++
		}
	}
	sb.WriteString(fmt.Sprintf("Total: %d results", int(total)))
	return textResult(sb.String())
}

func formatSystems(resp map[string]interface{}) MCPToolResult {
	if status, ok := resp["status"].(string); ok && status == "pending" {
		msg, _ := resp["message"].(string)
		return textResult(msg)
	}

	systems, _ := resp["systems"].([]interface{})
	var sb strings.Builder
	for _, s := range systems {
		if sys, ok := s.(map[string]interface{}); ok {
			name, _ := sys["system"].(string)
			count, _ := sys["rom_count"].(float64)
			loc, _ := sys["location"].(string)
			sb.WriteString(fmt.Sprintf("%-16s %5d ROMs (%s)\n", name, int(count), loc))
		}
	}
	return textResult(sb.String())
}

func formatInfo(resp map[string]interface{}) MCPToolResult {
	hostname, _ := resp["hostname"].(string)
	ip, _ := resp["ip"].(string)
	temp, _ := resp["temp"].(float64)
	ramMb, _ := resp["ram_mb"].(float64)
	ramFree, _ := resp["ram_free_mb"].(float64)
	uptime, _ := resp["uptime"].(string)

	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("Hostname: %s\n", hostname))
	sb.WriteString(fmt.Sprintf("IP: %s\n", ip))
	sb.WriteString(fmt.Sprintf("Temp: %.1f°C\n", temp))
	sb.WriteString(fmt.Sprintf("RAM: %d/%d MB free\n", int(ramFree), int(ramMb)))

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
			sb.WriteString(fmt.Sprintf("Disk: %s — %d/%d MB free (%s used) [%s]\n", mount, int(freeMb), int(totalMb), usePct, dev))
		}
	}

	sb.WriteString(fmt.Sprintf("Uptime: %s", uptime))
	return textResult(sb.String())
}

func formatTailscale(resp map[string]interface{}) MCPToolResult {
	// Return the raw JSON for tailscale since responses vary by action
	data, _ := json.MarshalIndent(resp, "", "  ")
	return textResult(string(data))
}

func formatInput(resp map[string]interface{}) MCPToolResult {
	if success, ok := resp["success"].(bool); ok && !success {
		errMsg, _ := resp["error"].(string)
		return errorResult(fmt.Sprintf("Input failed: %s", errMsg))
	}
	device, _ := resp["device"].(string)
	if button, ok := resp["button"].(string); ok {
		return textResult(fmt.Sprintf("Pressed gamepad button: %s", button))
	}
	if dpad, ok := resp["dpad"].(string); ok {
		return textResult(fmt.Sprintf("Pressed gamepad d-pad: %s", dpad))
	}
	if key, ok := resp["key"].(string); ok {
		if device == "gamepad" {
			return textResult(fmt.Sprintf("Pressed gamepad button: %s", key))
		}
		return textResult(fmt.Sprintf("Pressed key: %s", key))
	}
	if raw, ok := resp["raw"].(float64); ok {
		return textResult(fmt.Sprintf("Pressed raw key: %d", int(raw)))
	}
	if combo, ok := resp["combo"].([]interface{}); ok {
		keys := make([]string, len(combo))
		for i, k := range combo {
			keys[i], _ = k.(string)
		}
		return textResult(fmt.Sprintf("Pressed combo: %s", strings.Join(keys, "+")))
	}
	return textResult("Input sent")
}

func formatOSDInfo(resp map[string]interface{}) MCPToolResult {
	if success, ok := resp["success"].(bool); ok && !success {
		errMsg, _ := resp["error"].(string)
		return errorResult(errMsg)
	}

	coreName, _ := resp["core_name"].(string)
	repo, _ := resp["repo"].(string)

	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("Core: %s (repo: %s)\n\nOSD Menu:\n", coreName, repo))

	menu, _ := resp["menu"].([]interface{})
	for _, m := range menu {
		item, ok := m.(map[string]interface{})
		if !ok {
			continue
		}
		typ, _ := item["type"].(string)
		name, _ := item["name"].(string)
		raw, _ := item["raw"].(string)

		switch typ {
		case "separator":
			sb.WriteString("  ────────────\n")
		case "label":
			sb.WriteString(fmt.Sprintf("  [%s]\n", name))
		case "option", "option_hidden":
			values, _ := item["values"].([]interface{})
			vals := make([]string, len(values))
			for i, v := range values {
				vals[i], _ = v.(string)
			}
			sb.WriteString(fmt.Sprintf("  Option: %s = [%s]\n", name, strings.Join(vals, ", ")))
		case "trigger", "trigger_hidden":
			sb.WriteString(fmt.Sprintf("  Trigger: %s\n", name))
		case "file_load", "file_load_core":
			label, _ := item["label"].(string)
			exts, _ := item["extensions"].([]interface{})
			extStrs := make([]string, len(exts))
			for i, e := range exts {
				extStrs[i], _ = e.(string)
			}
			sb.WriteString(fmt.Sprintf("  File: %s (%s)\n", label, strings.Join(extStrs, ", ")))
		case "mount":
			label, _ := item["label"].(string)
			sb.WriteString(fmt.Sprintf("  Mount: %s\n", label))
		case "sub_page":
			sb.WriteString(fmt.Sprintf("  Sub-page: %s\n", name))
		case "reset":
			sb.WriteString(fmt.Sprintf("  Reset: %s\n", name))
		case "joystick":
			sb.WriteString(fmt.Sprintf("  Joystick: %s\n", name))
		case "dip":
			sb.WriteString("  DIP switches\n")
		default:
			sb.WriteString(fmt.Sprintf("  %s: %s\n", typ, raw))
		}
	}

	return textResult(sb.String())
}

func formatOSDVisible(resp map[string]interface{}) MCPToolResult {
	if success, ok := resp["success"].(bool); ok && !success {
		errMsg, _ := resp["error"].(string)
		return errorResult(errMsg)
	}

	coreName, _ := resp["core_name"].(string)
	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("Core: %s (visible items)\n\n", coreName))

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
			sb.WriteString("  ────────────\n")
		case "label":
			sb.WriteString(fmt.Sprintf("  [%s]\n", name))
		case "option", "option_hidden":
			values, _ := item["values"].([]interface{})
			vals := make([]string, len(values))
			for i, v := range values {
				vals[i], _ = v.(string)
			}
			sb.WriteString(fmt.Sprintf("  Option: %s = [%s]\n", name, strings.Join(vals, ", ")))
		case "trigger", "trigger_hidden":
			sb.WriteString(fmt.Sprintf("  Trigger: %s\n", name))
		default:
			sb.WriteString(fmt.Sprintf("  %s: %s\n", typ, name))
		}
	}

	return textResult(sb.String())
}

func formatCFGRead(resp map[string]interface{}) MCPToolResult {
	if success, ok := resp["success"].(bool); ok && !success {
		errMsg, _ := resp["error"].(string)
		return errorResult(errMsg)
	}

	coreName, _ := resp["core_name"].(string)
	cfgHex, _ := resp["cfg_hex"].(string)
	cfgPath, _ := resp["cfg_path"].(string)

	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("Core: %s\nCFG: %s\nHex: %s\n\nOptions:\n", coreName, cfgPath, cfgHex))

	options, _ := resp["options"].([]interface{})
	for _, o := range options {
		opt, ok := o.(map[string]interface{})
		if !ok {
			continue
		}
		name, _ := opt["name"].(string)
		val, _ := opt["value"].(float64)
		valName, _ := opt["value_name"].(string)
		if valName != "" {
			sb.WriteString(fmt.Sprintf("  %-24s = %s (%d)\n", name, valName, int(val)))
		} else {
			sb.WriteString(fmt.Sprintf("  %-24s = %d\n", name, int(val)))
		}
	}

	return textResult(sb.String())
}

func formatCFGWrite(resp map[string]interface{}) MCPToolResult {
	if success, ok := resp["success"].(bool); ok && !success {
		errMsg, _ := resp["error"].(string)
		return errorResult(errMsg)
	}

	option, _ := resp["option"].(string)
	value, _ := resp["value"].(string)
	cfgPath, _ := resp["cfg_path"].(string)

	text := fmt.Sprintf("Set %s = %s\nWritten to: %s (backup created)", option, value, cfgPath)
	if reload, ok := resp["reload_required"].(bool); ok && reload {
		text += "\nReload required — use mister_reload to apply changes."
	}
	return textResult(text)
}

func formatReload(resp map[string]interface{}) MCPToolResult {
	if success, ok := resp["success"].(bool); ok && !success {
		errMsg, _ := resp["error"].(string)
		return errorResult(errMsg)
	}

	path, _ := resp["path"].(string)
	return textResult(fmt.Sprintf("Reloaded: %s", path))
}

func formatRescan(resp map[string]interface{}) MCPToolResult {
	if success, ok := resp["success"].(bool); ok && !success {
		errMsg, _ := resp["error"].(string)
		return errorResult(errMsg)
	}

	systemsFound, _ := resp["systems_found"].(float64)
	location, _ := resp["location"].(string)
	return textResult(fmt.Sprintf("Rescan complete: %d systems found (location: %s)", int(systemsFound), location))
}

func formatOSDNavigate(resp map[string]interface{}) MCPToolResult {
	if success, ok := resp["success"].(bool); ok && !success {
		errMsg, _ := resp["error"].(string)
		return errorResult(errMsg)
	}

	target, _ := resp["target"].(string)
	core, _ := resp["core"].(string)
	text := fmt.Sprintf("Navigated to: %s", target)
	if core != "" {
		text += fmt.Sprintf(" (core: %s)", core)
	}
	return textResult(text)
}

func formatSystemInfo(resp map[string]interface{}) MCPToolResult {
	if success, ok := resp["success"].(bool); ok && !success {
		errMsg, _ := resp["error"].(string)
		return errorResult(errMsg)
	}

	system, _ := resp["system"].(string)
	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("System: %s\n", system))

	if config, ok := resp["config"].(map[string]interface{}); ok {
		if core, ok := config["core"].(string); ok {
			sb.WriteString(fmt.Sprintf("Core: %s\n", core))
		}
		if romType, ok := config["type"].(string); ok {
			sb.WriteString(fmt.Sprintf("ROM type: %s\n", romType))
		}
		if index, ok := config["index"].(float64); ok {
			sb.WriteString(fmt.Sprintf("Index: %d\n", int(index)))
		}
		if exts, ok := config["extensions"].([]interface{}); ok && len(exts) > 0 {
			extStrs := make([]string, len(exts))
			for i, e := range exts {
				extStrs[i], _ = e.(string)
			}
			sb.WriteString(fmt.Sprintf("Extensions: %s\n", strings.Join(extStrs, ", ")))
		}
	}

	if notes, ok := resp["notes"].(string); ok && notes != "" {
		sb.WriteString(fmt.Sprintf("\nNotes:\n%s\n", notes))
	}

	if coreName, ok := resp["core_name"].(string); ok && coreName != "" {
		sb.WriteString(fmt.Sprintf("\nOSD Core: %s\n", coreName))
	}

	if menu, ok := resp["menu"].([]interface{}); ok && len(menu) > 0 {
		sb.WriteString("\nOSD Menu:\n")
		for _, m := range menu {
			item, ok := m.(map[string]interface{})
			if !ok {
				continue
			}
			typ, _ := item["type"].(string)
			name, _ := item["name"].(string)

			switch typ {
			case "separator":
				sb.WriteString("  ────────────\n")
			case "label":
				sb.WriteString(fmt.Sprintf("  [%s]\n", name))
			case "option", "option_hidden":
				values, _ := item["values"].([]interface{})
				vals := make([]string, len(values))
				for i, v := range values {
					vals[i], _ = v.(string)
				}
				sb.WriteString(fmt.Sprintf("  Option: %s = [%s]\n", name, strings.Join(vals, ", ")))
			case "trigger", "trigger_hidden":
				sb.WriteString(fmt.Sprintf("  Trigger: %s\n", name))
			case "file_load", "file_load_core":
				label, _ := item["label"].(string)
				exts, _ := item["extensions"].([]interface{})
				extStrs := make([]string, len(exts))
				for i, e := range exts {
					extStrs[i], _ = e.(string)
				}
				sb.WriteString(fmt.Sprintf("  File: %s (%s)\n", label, strings.Join(extStrs, ", ")))
			case "mount":
				label, _ := item["label"].(string)
				sb.WriteString(fmt.Sprintf("  Mount: %s\n", label))
			case "reset":
				sb.WriteString(fmt.Sprintf("  Reset: %s\n", name))
			default:
				sb.WriteString(fmt.Sprintf("  %s: %s\n", typ, name))
			}
		}
	}

	return textResult(sb.String())
}

// Result helpers

func textResult(text string) MCPToolResult {
	return MCPToolResult{
		Content: []MCPContent{{Type: "text", Text: text}},
	}
}

func errorResult(msg string) MCPToolResult {
	return MCPToolResult{
		Content: []MCPContent{{Type: "text", Text: msg}},
		IsError: true,
	}
}
