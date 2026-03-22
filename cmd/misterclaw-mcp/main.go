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

	case "mister_shell":
		cmd, _ := args["command"].(string)
		if cmd == "" {
			return errorResult("command is required")
		}
		return doShellCommand(cmd)

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
