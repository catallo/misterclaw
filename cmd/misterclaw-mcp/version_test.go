package main

import (
	"context"
	"encoding/json"
	"flag"
	"os"
	"os/exec"
	"testing"
	"time"

	"github.com/catallo/misterclaw/internal/version"
)

func TestMCPVersionProcess(t *testing.T) {
	if os.Getenv("MISTERCLAW_MCP_VERSION_PROCESS") != "1" {
		t.Skip("child-process entry point")
	}
	os.Args = []string{os.Args[0], "--version"}
	flag.CommandLine = flag.NewFlagSet(os.Args[0], flag.ExitOnError)
	main()
	os.Exit(0)
}

func TestMCPVersionFlag(t *testing.T) {
	executable, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	command := exec.CommandContext(ctx, executable, "-test.run=^TestMCPVersionProcess$")
	command.Env = append(os.Environ(), "MISTERCLAW_MCP_VERSION_PROCESS=1")
	output, err := command.CombinedOutput()
	if err != nil || string(output) != "misterclaw-mcp v"+version.Current+"\n" {
		t.Fatalf("MCP --version output=%q error=%v", output, err)
	}
}

func TestInitializeReportsSharedSoftwareVersion(t *testing.T) {
	id := json.RawMessage(`1`)
	response := handleRequest(JSONRPCRequest{JSONRPC: "2.0", ID: &id, Method: "initialize"})
	result := response.Result.(map[string]interface{})
	info := result["serverInfo"].(map[string]interface{})
	if info["version"] != version.Current {
		t.Fatalf("MCP version = %v, want %q", info["version"], version.Current)
	}
}
