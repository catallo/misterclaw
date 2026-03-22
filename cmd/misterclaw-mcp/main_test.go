package main

import (
	"encoding/json"
	"testing"
)

func TestParseJSONRPCRequest(t *testing.T) {
	input := `{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2024-11-05","capabilities":{},"clientInfo":{"name":"claude","version":"1.0"}}}`

	var req JSONRPCRequest
	if err := json.Unmarshal([]byte(input), &req); err != nil {
		t.Fatal(err)
	}

	if req.JSONRPC != "2.0" {
		t.Errorf("expected jsonrpc 2.0, got %s", req.JSONRPC)
	}
	if req.Method != "initialize" {
		t.Errorf("expected method initialize, got %s", req.Method)
	}
	if req.ID == nil {
		t.Fatal("expected non-nil id")
	}
}

func TestParseNotification(t *testing.T) {
	input := `{"jsonrpc":"2.0","method":"notifications/initialized"}`

	var req JSONRPCRequest
	if err := json.Unmarshal([]byte(input), &req); err != nil {
		t.Fatal(err)
	}

	if req.ID != nil {
		t.Error("notification should have no id")
	}
	if req.Method != "notifications/initialized" {
		t.Errorf("expected notifications/initialized, got %s", req.Method)
	}
}

func TestHandleInitialize(t *testing.T) {
	idRaw := json.RawMessage(`1`)
	req := JSONRPCRequest{
		JSONRPC: "2.0",
		ID:      &idRaw,
		Method:  "initialize",
	}

	resp := handleRequest(req)
	if resp == nil {
		t.Fatal("expected response")
	}
	if resp.JSONRPC != "2.0" {
		t.Errorf("expected jsonrpc 2.0, got %s", resp.JSONRPC)
	}

	result, ok := resp.Result.(map[string]interface{})
	if !ok {
		t.Fatal("expected result map")
	}

	if result["protocolVersion"] != "2024-11-05" {
		t.Errorf("unexpected protocol version: %v", result["protocolVersion"])
	}

	serverInfo, ok := result["serverInfo"].(map[string]interface{})
	if !ok {
		t.Fatal("expected serverInfo map")
	}
	if serverInfo["name"] != "misterclaw-mcp" {
		t.Errorf("unexpected server name: %v", serverInfo["name"])
	}
}

func TestHandleNotificationReturnsNil(t *testing.T) {
	req := JSONRPCRequest{
		JSONRPC: "2.0",
		Method:  "notifications/initialized",
	}

	resp := handleRequest(req)
	if resp != nil {
		t.Error("notification should return nil")
	}
}

func TestHandleToolsList(t *testing.T) {
	idRaw := json.RawMessage(`2`)
	req := JSONRPCRequest{
		JSONRPC: "2.0",
		ID:      &idRaw,
		Method:  "tools/list",
	}

	resp := handleRequest(req)
	if resp == nil {
		t.Fatal("expected response")
	}

	result, ok := resp.Result.(map[string]interface{})
	if !ok {
		t.Fatal("expected result map")
	}

	tools, ok := result["tools"].([]ToolDef)
	if !ok {
		t.Fatal("expected tools array")
	}

	expectedTools := map[string]bool{
		"mister_status":     false,
		"mister_launch":     false,
		"mister_search":     false,
		"mister_systems":    false,
		"mister_screenshot": false,
		"mister_info":       false,
		"mister_input":      false,
		"mister_tailscale":  false,
		"mister_shell":      false,
		"mister_osd_info":   false,
		"mister_osd_visible": false,
		"mister_cfg_read":    false,
		"mister_cfg_write":   false,
		"mister_reload":      false,
	}

	for _, tool := range tools {
		if _, exists := expectedTools[tool.Name]; !exists {
			t.Errorf("unexpected tool: %s", tool.Name)
		}
		expectedTools[tool.Name] = true

		if tool.Description == "" {
			t.Errorf("tool %s has empty description", tool.Name)
		}
		if tool.InputSchema == nil {
			t.Errorf("tool %s has nil inputSchema", tool.Name)
		}
	}

	for name, found := range expectedTools {
		if !found {
			t.Errorf("missing tool: %s", name)
		}
	}
}

func TestToolParameterExtraction(t *testing.T) {
	tests := []struct {
		name     string
		params   string
		wantTool string
	}{
		{
			name:     "status no args",
			params:   `{"name":"mister_status","arguments":{}}`,
			wantTool: "mister_status",
		},
		{
			name:     "launch with query",
			params:   `{"name":"mister_launch","arguments":{"query":"sonic 2","system":"MegaDrive"}}`,
			wantTool: "mister_launch",
		},
		{
			name:     "search with limit",
			params:   `{"name":"mister_search","arguments":{"query":"zelda","limit":5}}`,
			wantTool: "mister_search",
		},
		{
			name:     "shell command",
			params:   `{"name":"mister_shell","arguments":{"command":"ls /media/fat"}}`,
			wantTool: "mister_shell",
		},
		{
			name:     "tailscale setup",
			params:   `{"name":"mister_tailscale","arguments":{"action":"setup","hostname":"my-mister"}}`,
			wantTool: "mister_tailscale",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var p struct {
				Name      string          `json:"name"`
				Arguments json.RawMessage `json:"arguments"`
			}
			if err := json.Unmarshal([]byte(tt.params), &p); err != nil {
				t.Fatal(err)
			}
			if p.Name != tt.wantTool {
				t.Errorf("expected tool %s, got %s", tt.wantTool, p.Name)
			}
		})
	}
}

func TestResponseFormatting(t *testing.T) {
	t.Run("text result", func(t *testing.T) {
		result := textResult("hello world")
		if len(result.Content) != 1 {
			t.Fatalf("expected 1 content block, got %d", len(result.Content))
		}
		if result.Content[0].Type != "text" {
			t.Errorf("expected type text, got %s", result.Content[0].Type)
		}
		if result.Content[0].Text != "hello world" {
			t.Errorf("expected hello world, got %s", result.Content[0].Text)
		}
		if result.IsError {
			t.Error("should not be error")
		}
	})

	t.Run("error result", func(t *testing.T) {
		result := errorResult("something broke")
		if !result.IsError {
			t.Error("should be error")
		}
		if result.Content[0].Text != "something broke" {
			t.Errorf("expected error message, got %s", result.Content[0].Text)
		}
	})

	t.Run("response serialization", func(t *testing.T) {
		resp := JSONRPCResponse{
			JSONRPC: "2.0",
			ID:      float64(3),
			Result:  textResult("Launched: Sonic The Hedgehog 2 [MegaDrive]"),
		}
		data, err := json.Marshal(resp)
		if err != nil {
			t.Fatal(err)
		}

		var parsed map[string]interface{}
		json.Unmarshal(data, &parsed)

		if parsed["jsonrpc"] != "2.0" {
			t.Errorf("expected jsonrpc 2.0")
		}
		if parsed["id"].(float64) != 3 {
			t.Errorf("expected id 3")
		}
	})
}

func TestFormatStatus(t *testing.T) {
	resp := map[string]interface{}{
		"core_name": "SNES",
		"game_path": "/media/fat/games/SNES/Super Mario World.sfc",
	}
	result := formatStatus(resp)
	if result.IsError {
		t.Error("should not be error")
	}
	if result.Content[0].Text != "Core: SNES\nGame: /media/fat/games/SNES/Super Mario World.sfc" {
		t.Errorf("unexpected text: %s", result.Content[0].Text)
	}
}

func TestFormatStatusEmpty(t *testing.T) {
	resp := map[string]interface{}{}
	result := formatStatus(resp)
	if result.Content[0].Text != "Core: (none)" {
		t.Errorf("unexpected text: %s", result.Content[0].Text)
	}
}

func TestFormatLaunch(t *testing.T) {
	resp := map[string]interface{}{
		"success": true,
		"game":    "Sonic The Hedgehog 2",
	}
	result := formatLaunch(resp)
	if result.IsError {
		t.Error("should not be error")
	}
	if result.Content[0].Text != "Launched: Sonic The Hedgehog 2" {
		t.Errorf("unexpected text: %s", result.Content[0].Text)
	}
}

func TestFormatLaunchError(t *testing.T) {
	resp := map[string]interface{}{
		"success": false,
		"error":   "no game found",
	}
	result := formatLaunch(resp)
	if !result.IsError {
		t.Error("should be error")
	}
}

func TestUnknownMethodError(t *testing.T) {
	idRaw := json.RawMessage(`99`)
	req := JSONRPCRequest{
		JSONRPC: "2.0",
		ID:      &idRaw,
		Method:  "nonexistent/method",
	}

	resp := handleRequest(req)
	if resp == nil {
		t.Fatal("expected response")
	}
	if resp.Error == nil {
		t.Error("expected error for unknown method")
	}
	errMap, ok := resp.Error.(map[string]interface{})
	if !ok {
		t.Fatal("expected error map")
	}
	if errMap["code"].(int) != -32601 {
		t.Errorf("expected -32601, got %v", errMap["code"])
	}
}

func TestUnknownTool(t *testing.T) {
	params := json.RawMessage(`{"name":"nonexistent_tool","arguments":{}}`)
	result := callTool(params)
	if !result.IsError {
		t.Error("expected error for unknown tool")
	}
}

func TestCallToolShellMissingCommand(t *testing.T) {
	params := json.RawMessage(`{"name":"mister_shell","arguments":{}}`)
	result := callTool(params)
	if !result.IsError {
		t.Error("expected error for missing command")
	}
}

func TestFullRoundTrip(t *testing.T) {
	// Simulate a full JSON-RPC round trip: initialize -> tools/list -> verify
	initJSON := `{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2024-11-05","capabilities":{},"clientInfo":{"name":"test","version":"1.0"}}}`

	var req JSONRPCRequest
	json.Unmarshal([]byte(initJSON), &req)
	resp := handleRequest(req)

	data, _ := json.Marshal(resp)
	var parsed map[string]interface{}
	json.Unmarshal(data, &parsed)

	result, _ := parsed["result"].(map[string]interface{})
	if result["protocolVersion"] != "2024-11-05" {
		t.Errorf("protocol version mismatch in round trip")
	}

	// tools/list
	listJSON := `{"jsonrpc":"2.0","id":2,"method":"tools/list"}`
	json.Unmarshal([]byte(listJSON), &req)
	resp = handleRequest(req)

	data, _ = json.Marshal(resp)
	json.Unmarshal(data, &parsed)
	result, _ = parsed["result"].(map[string]interface{})

	tools, _ := result["tools"].([]interface{})
	if len(tools) != 14 {
		t.Errorf("expected 14 tools, got %d", len(tools))
	}
}
