package main

import (
	"bytes"
	"context"
	"encoding/json"
	"flag"
	"os"
	"os/exec"
	"strings"
	"testing"
	"time"
)

// Exercise the real stdio loop on each native test platform, including Windows.
func TestPortableMCPProcess(t *testing.T) {
	if os.Getenv("MISTERCLAW_MCP_TEST_PROCESS") != "1" {
		t.Skip("child-process entry point")
	}
	os.Args = []string{os.Args[0]}
	flag.CommandLine = flag.NewFlagSet(os.Args[0], flag.ExitOnError)
	main()
	os.Exit(0)
}

func TestPortableMCPStdio(t *testing.T) {
	executable, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, executable, "-test.run=^TestPortableMCPProcess$")
	cmd.Env = append(os.Environ(), "MISTERCLAW_MCP_TEST_PROCESS=1")
	cmd.Dir = t.TempDir()
	// A piped MCP client may use either CRLF or LF; notifications have no reply.
	cmd.Stdin = strings.NewReader("\r\n" +
		`{"jsonrpc":"2.0","id":"Grüße 日本語","method":"initialize"}` + "\r\n" +
		`{"jsonrpc":"2.0","method":"notifications/initialized"}` + "\r\n" +
		"invalid JSON\r\n" +
		`{"jsonrpc":"2.0","id":2,"method":"tools/list"}` + "\n")
	var stdout, stderr bytes.Buffer
	cmd.Stdout, cmd.Stderr = &stdout, &stderr
	if err := cmd.Run(); err != nil {
		t.Fatalf("stdio process: %v, stderr=%q", err, stderr.Bytes())
	}
	if stderr.Len() != 0 {
		t.Fatalf("unexpected stderr: %q", stderr.Bytes())
	}
	lines := bytes.Split(bytes.TrimSuffix(stdout.Bytes(), []byte("\n")), []byte("\n"))
	if len(lines) != 3 {
		t.Fatalf("expected three JSON responses, got %q", stdout.Bytes())
	}
	responses := make([]map[string]interface{}, len(lines))
	for i, line := range lines {
		if err := json.Unmarshal(line, &responses[i]); err != nil {
			t.Fatalf("stdout line %d is not JSON: %q, error=%v", i, line, err)
		}
		if responses[i]["jsonrpc"] != "2.0" {
			t.Fatalf("invalid JSON-RPC response: %v", responses[i])
		}
	}
	if responses[0]["id"] != "Grüße 日本語" || responses[0]["result"] == nil {
		t.Fatalf("initialization did not preserve the Unicode id: %v", responses[0])
	}
	parseError, ok := responses[1]["error"].(map[string]interface{})
	if !ok || parseError["code"] != float64(-32700) || responses[1]["id"] != nil {
		t.Fatalf("invalid parse-error response: %v", responses[1])
	}
	result, ok := responses[2]["result"].(map[string]interface{})
	if !ok || responses[2]["id"] != float64(2) {
		t.Fatalf("invalid tools/list response: %v", responses[2])
	}
	tools, ok := result["tools"].([]interface{})
	if !ok || len(tools) == 0 {
		t.Fatalf("tools/list returned no tools: %v", result)
	}
}
