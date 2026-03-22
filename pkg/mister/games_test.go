package mister

import (
	"encoding/xml"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestSystemMapCompleteness(t *testing.T) {
	// Every system must have a core, at least one extension, and valid type
	for name, cfg := range systemMap {
		if cfg.Core == "" {
			t.Errorf("system %s has empty Core", name)
		}
		if len(cfg.Extensions) == 0 {
			t.Errorf("system %s has no extensions", name)
		}
		if cfg.Type != "f" && cfg.Type != "s" {
			t.Errorf("system %s has invalid Type %q (want \"f\" or \"s\")", name, cfg.Type)
		}
		for _, ext := range cfg.Extensions {
			if !strings.HasPrefix(ext, ".") {
				t.Errorf("system %s extension %q missing leading dot", name, ext)
			}
		}
	}
}

func TestGetSystemConfig(t *testing.T) {
	cfg, ok := GetSystemConfig("SNES")
	if !ok {
		t.Fatal("expected to find SNES")
	}
	if cfg.Core != "_Console/SNES" {
		t.Errorf("SNES core = %q, want _Console/SNES", cfg.Core)
	}

	// Case-insensitive
	cfg, ok = GetSystemConfig("genesis")
	if !ok {
		t.Fatal("expected to find genesis (case-insensitive)")
	}
	if cfg.Core != "_Console/MegaDrive" {
		t.Errorf("Genesis core = %q, want _Console/MegaDrive", cfg.Core)
	}

	// Unknown system
	_, ok = GetSystemConfig("Atari2600")
	if ok {
		t.Error("expected Atari2600 to not be found")
	}
}

func TestGenerateMGL_SD(t *testing.T) {
	game := GameInfo{
		Name:     "Sonic The Hedgehog (World)",
		Path:     "/media/fat/games/Genesis/Sonic The Hedgehog (World).md",
		System:   "Genesis",
		Location: "sd",
	}

	mgl := GenerateMGL(game)
	if mgl == "" {
		t.Fatal("GenerateMGL returned empty")
	}

	// Verify XML is well-formed
	var parsed mglFile
	if err := xml.Unmarshal([]byte(mgl), &parsed); err != nil {
		t.Fatalf("invalid XML: %v\n%s", err, mgl)
	}

	if parsed.Rbf != "_Console/MegaDrive" {
		t.Errorf("rbf = %q, want _Console/MegaDrive", parsed.Rbf)
	}
	if parsed.File.Delay != 1 {
		t.Errorf("delay = %d, want 1", parsed.File.Delay)
	}
	if parsed.File.Type != "f" {
		t.Errorf("type = %q, want f", parsed.File.Type)
	}
	if parsed.File.Index != 1 {
		t.Errorf("index = %d, want 1", parsed.File.Index)
	}

	expectedPath := "../../../../../media/fat/games/Genesis/Sonic The Hedgehog (World).md"
	if parsed.File.Path != expectedPath {
		t.Errorf("path = %q, want %q", parsed.File.Path, expectedPath)
	}

	// Genesis should NOT have setname
	if parsed.SetName != nil {
		t.Errorf("Genesis should not have setname, got %q", *parsed.SetName)
	}
}

func TestGenerateMGL_USB(t *testing.T) {
	game := GameInfo{
		Name:     "Tetris (World) (Rev 1)",
		Path:     "/media/usb0/GameBoy/Tetris (World) (Rev 1).gb",
		System:   "Gameboy",
		Location: "usb0",
	}

	mgl := GenerateMGL(game)
	var parsed mglFile
	if err := xml.Unmarshal([]byte(mgl), &parsed); err != nil {
		t.Fatalf("invalid XML: %v", err)
	}

	expectedPath := "../../../../../media/usb0/GameBoy/Tetris (World) (Rev 1).gb"
	if parsed.File.Path != expectedPath {
		t.Errorf("path = %q, want %q", parsed.File.Path, expectedPath)
	}
	if parsed.Rbf != "_Console/Gameboy" {
		t.Errorf("rbf = %q, want _Console/Gameboy", parsed.Rbf)
	}
}

func TestGenerateMGL_SetName(t *testing.T) {
	// GBC shares core with Gameboy, needs setname
	game := GameInfo{
		Name:     "Pokemon Crystal",
		Path:     "/media/fat/games/GBC/Pokemon Crystal.gbc",
		System:   "GBC",
		Location: "sd",
	}

	mgl := GenerateMGL(game)
	var parsed mglFile
	if err := xml.Unmarshal([]byte(mgl), &parsed); err != nil {
		t.Fatalf("invalid XML: %v", err)
	}

	if parsed.SetName == nil {
		t.Fatal("GBC should have setname")
	}
	if *parsed.SetName != "GBC" {
		t.Errorf("setname = %q, want GBC", *parsed.SetName)
	}
}

func TestGenerateMGL_PSX(t *testing.T) {
	// PSX uses type "s" not "f"
	game := GameInfo{
		Name:     "Crash Bandicoot",
		Path:     "/media/fat/games/PSX/Crash Bandicoot.chd",
		System:   "PSX",
		Location: "sd",
	}

	mgl := GenerateMGL(game)
	var parsed mglFile
	if err := xml.Unmarshal([]byte(mgl), &parsed); err != nil {
		t.Fatalf("invalid XML: %v", err)
	}

	if parsed.File.Type != "s" {
		t.Errorf("PSX type = %q, want s", parsed.File.Type)
	}
}

func TestGenerateMGL_UnknownSystem(t *testing.T) {
	game := GameInfo{System: "Unknown"}
	if mgl := GenerateMGL(game); mgl != "" {
		t.Error("expected empty string for unknown system")
	}
}

func TestSearchGames(t *testing.T) {
	// Create temp filesystem structure
	tmp := t.TempDir()
	sdGames := filepath.Join(tmp, "games", "Genesis")
	os.MkdirAll(sdGames, 0755)

	// Create test ROM files
	files := []string{
		"Sonic The Hedgehog (World).md",
		"Sonic The Hedgehog 2 (World).md",
		"Streets of Rage (USA, Europe).md",
		"Streets of Rage 2 (USA).md",
		"Golden Axe (World).md",
	}
	for _, f := range files {
		os.WriteFile(filepath.Join(sdGames, f), []byte{}, 0644)
	}

	// Test scanDir directly
	extSet := map[string]bool{".md": true, ".gen": true, ".bin": true}
	games := scanDir(sdGames, "Genesis", "sd", extSet)

	if len(games) != 5 {
		t.Fatalf("scanDir found %d games, want 5", len(games))
	}

	// Test search logic on the scanned results
	results := filterGames(games, "sonic", "")
	if len(results) != 2 {
		t.Errorf("search 'sonic' found %d, want 2", len(results))
	}

	results = filterGames(games, "streets rage", "")
	if len(results) != 2 {
		t.Errorf("search 'streets rage' found %d, want 2", len(results))
	}

	results = filterGames(games, "sonic 2", "")
	if len(results) != 1 {
		t.Errorf("search 'sonic 2' found %d, want 1", len(results))
	}

	results = filterGames(games, "zelda", "")
	if len(results) != 0 {
		t.Errorf("search 'zelda' found %d, want 0", len(results))
	}
}

// filterGames applies SearchGames logic to a pre-scanned list (for testing without filesystem).
func filterGames(games []GameInfo, query string, system string) []GameInfo {
	terms := strings.Fields(strings.ToLower(query))
	if len(terms) == 0 {
		return nil
	}
	var results []GameInfo
	for _, g := range games {
		if system != "" && !strings.EqualFold(g.System, system) {
			continue
		}
		nameLower := strings.ToLower(g.Name)
		match := true
		for _, term := range terms {
			if !strings.Contains(nameLower, term) {
				match = false
				break
			}
		}
		if match {
			results = append(results, g)
		}
	}
	return results
}

func TestScanSystem_Filesystem(t *testing.T) {
	// This tests the actual filesystem scanning with a temp directory.
	// We can't easily redirect the scan paths, so we test scanDir directly.
	tmp := t.TempDir()
	gbDir := filepath.Join(tmp, "Gameboy")
	os.MkdirAll(gbDir, 0755)

	os.WriteFile(filepath.Join(gbDir, "Tetris.gb"), []byte{}, 0644)
	os.WriteFile(filepath.Join(gbDir, "readme.txt"), []byte{}, 0644) // should be skipped
	os.WriteFile(filepath.Join(gbDir, "Pokemon.gbc"), []byte{}, 0644) // wrong ext for Gameboy

	extSet := map[string]bool{".gb": true}
	games := scanDir(gbDir, "Gameboy", "sd", extSet)

	if len(games) != 1 {
		t.Fatalf("got %d games, want 1", len(games))
	}
	if games[0].Name != "Tetris" {
		t.Errorf("name = %q, want Tetris", games[0].Name)
	}
	if games[0].System != "Gameboy" {
		t.Errorf("system = %q, want Gameboy", games[0].System)
	}
}

func TestScanDir_Subdirectories(t *testing.T) {
	tmp := t.TempDir()
	subDir := filepath.Join(tmp, "USA")
	os.MkdirAll(subDir, 0755)

	os.WriteFile(filepath.Join(tmp, "Game1.nes"), []byte{}, 0644)
	os.WriteFile(filepath.Join(subDir, "Game2.nes"), []byte{}, 0644)

	extSet := map[string]bool{".nes": true}
	games := scanDir(tmp, "NES", "sd", extSet)

	if len(games) != 2 {
		t.Fatalf("got %d games, want 2 (including subdirectory)", len(games))
	}
}

func TestMGLXMLHeader(t *testing.T) {
	game := GameInfo{
		Name:   "Test",
		Path:   "/media/fat/games/NES/Test.nes",
		System: "NES",
	}
	mgl := GenerateMGL(game)
	if !strings.HasPrefix(mgl, "<?xml") {
		t.Error("MGL should start with XML declaration")
	}
}
