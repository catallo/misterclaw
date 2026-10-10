package server

import (
	"archive/zip"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/catallo/misterclaw/pkg/mister"
)

func TestZIPExplicitLaunchValidatesBeforeCallback(t *testing.T) {
	root := t.TempDir()
	archivePath := filepath.Join(root, "games.zip")
	f, err := os.Create(archivePath)
	if err != nil {
		t.Fatal(err)
	}
	w := zip.NewWriter(f)
	member := "USA/Golden Sun.gba"
	rom, err := w.Create(member)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := rom.Write([]byte("test ROM payload")); err != nil {
		t.Fatal(err)
	}
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}
	if err := f.Close(); err != nil {
		t.Fatal(err)
	}
	addr, srv := startTestServer(t)
	calls := make(chan mister.GameInfo, 8)
	srv.launchGame = func(game mister.GameInfo) error {
		calls <- game
		return nil // Hardware-free replacement for the native launch callback.
	}
	conn, scanner := dial(t, addr)
	for _, tc := range []struct {
		path, system string
		valid        bool
	}{
		{archivePath + "/" + member, "GBA", true},
		{archivePath + "/missing.gba", "GBA", false},
		{archivePath + "/usa/Golden Sun.gba", "GBA", false},
		{archivePath + "/../Golden Sun.gba", "GBA", false},
		{archivePath + "/" + member, "S32X", false},
		{archivePath + "/disk.chd", "PSX", false},
		{filepath.Join(root, "missing.zip/game.gba"), "GBA", false},
	} {
		payload, err := json.Marshal(Request{MiSTer: "launch", Path: tc.path, System: tc.system})
		if err != nil {
			t.Fatal(err)
		}
		response := sendAndRead(t, conn, scanner, string(payload))
		if response["success"] != tc.valid {
			t.Fatalf("path=%q response=%+v", tc.path, response)
		}
		if tc.valid {
			select {
			case game := <-calls:
				if game.Path != tc.path || game.System != tc.system || game.Name != "Golden Sun" {
					t.Fatalf("callback metadata changed: %+v", game)
				}
			default:
				t.Fatal("valid member did not reach callback")
			}
		} else {
			select {
			case game := <-calls:
				t.Fatalf("invalid member reached callback: %+v", game)
			default:
			}
			if response["error"] == nil {
				t.Fatal("invalid ZIP did not report an error")
			}
		}
	}
}
