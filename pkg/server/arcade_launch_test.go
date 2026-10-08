package server

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/catallo/misterclaw/pkg/mister"
)

func TestArcadeLaunchResponseReportsDescriptorCore(t *testing.T) {
	path := filepath.Join(t.TempDir(), "1942 Alias.mra")
	if err := os.WriteFile(path, []byte("<misterromdescription><name>1942</name><rbf>jt1942</rbf></misterromdescription>"), 0644); err != nil {
		t.Fatal(err)
	}
	addr, srv := startTestServer(t)
	calls := make(chan mister.GameInfo, 1)
	srv.launchGame = func(game mister.GameInfo) error {
		calls <- game
		return nil // no /dev/MiSTer_cmd or uinput in this integration test
	}
	conn, scanner := dial(t, addr)
	payload, err := json.Marshal(Request{MiSTer: "launch", Path: path, System: "Arcade"})
	if err != nil {
		t.Fatal(err)
	}
	response := sendAndRead(t, conn, scanner, string(payload))
	if response["success"] != true || response["core_name"] != "jt1942" {
		t.Fatalf("launch response=%+v", response)
	}
	select {
	case game := <-calls:
		if game.Path != path || game.System != "Arcade" {
			t.Fatalf("launch args=%+v", game)
		}
	default:
		t.Fatal("launch function was not called")
	}
}

func TestArcadeLaunchFailureDoesNotReportSuccess(t *testing.T) {
	addr, srv := startTestServer(t)
	srv.launchGame = func(game mister.GameInfo) error { return fmt.Errorf("invalid arcade descriptor") }
	conn, scanner := dial(t, addr)
	response := sendAndRead(t, conn, scanner, `{"mister":"launch","path":"/nonexistent.mra","system":"Arcade"}`)
	if response["success"] != false || response["error"] != "invalid arcade descriptor" {
		t.Fatalf("launch failure response=%+v", response)
	}
	if _, exists := response["core_name"]; exists {
		t.Fatal("failed launch should not invent a core name")
	}
}
