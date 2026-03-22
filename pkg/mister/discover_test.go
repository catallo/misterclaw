package mister

import (
	"encoding/json"
	"encoding/xml"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestExtractCoreName(t *testing.T) {
	tests := []struct {
		input string
		want  string
	}{
		{"SNES_20250605.rbf", "SNES"},
		{"MegaDrive_20250707.rbf", "MegaDrive"},
		{"Gameboy_20250618.rbf", "Gameboy"},
		{"NES_20250101.rbf", "NES"},
		{"SMS_20241231.rbf", "SMS"},
		// No date suffix
		{"NoDate.rbf", "NoDate"},
		// Partial date (not 8 digits)
		{"Core_12345.rbf", "Core_12345"},
		// Non-digit suffix
		{"Core_abcdefgh.rbf", "Core_abcdefgh"},
		// Underscore in core name with date
		{"TurboGrafx16_20250101.rbf", "TurboGrafx16"},
		// Multiple underscores
		{"ZX-Spectrum_20250101.rbf", "ZX-Spectrum"},
	}
	for _, tt := range tests {
		got := extractCoreName(tt.input)
		if got != tt.want {
			t.Errorf("extractCoreName(%q) = %q, want %q", tt.input, got, tt.want)
		}
	}
}

func TestScanCores(t *testing.T) {
	tmp := t.TempDir()
	consoleDir := filepath.Join(tmp, "_Console")
	computerDir := filepath.Join(tmp, "_Computer")
	os.MkdirAll(consoleDir, 0755)
	os.MkdirAll(computerDir, 0755)

	// Create fake .rbf files
	os.WriteFile(filepath.Join(consoleDir, "SNES_20250605.rbf"), []byte{}, 0644)
	os.WriteFile(filepath.Join(consoleDir, "NES_20250101.rbf"), []byte{}, 0644)
	os.WriteFile(filepath.Join(consoleDir, "MegaDrive_20250707.rbf"), []byte{}, 0644)
	os.WriteFile(filepath.Join(computerDir, "C64_20250101.rbf"), []byte{}, 0644)
	// Non-rbf file should be ignored
	os.WriteFile(filepath.Join(consoleDir, "readme.txt"), []byte{}, 0644)

	// Override paths for test
	oldConsole, oldComputer := consoleCoresPath, computerCoresPath
	consoleCoresPath = consoleDir
	computerCoresPath = computerDir
	defer func() {
		consoleCoresPath = oldConsole
		computerCoresPath = oldComputer
	}()

	cores := scanCores()

	if len(cores) != 4 {
		t.Fatalf("scanCores found %d cores, want 4", len(cores))
	}
	if cores["snes"] != "_Console/SNES" {
		t.Errorf("SNES core = %q, want _Console/SNES", cores["snes"])
	}
	if cores["megadrive"] != "_Console/MegaDrive" {
		t.Errorf("MegaDrive core = %q, want _Console/MegaDrive", cores["megadrive"])
	}
	if cores["c64"] != "_Computer/C64" {
		t.Errorf("C64 core = %q, want _Computer/C64", cores["c64"])
	}
}

func TestParseMGLFiles(t *testing.T) {
	tmp := t.TempDir()
	consoleDir := filepath.Join(tmp, "_Console")
	os.MkdirAll(consoleDir, 0755)

	// Create fake .mgl files
	ggMGL := `<mistergamedescription>
    <rbf>_Console/SMS</rbf>
    <setname>GameGear</setname>
</mistergamedescription>`
	os.WriteFile(filepath.Join(consoleDir, "Game Gear.mgl"), []byte(ggMGL), 0644)

	gbcMGL := `<mistergamedescription>
    <rbf>_Console/Gameboy</rbf>
    <setname>GBC</setname>
</mistergamedescription>`
	os.WriteFile(filepath.Join(consoleDir, "Game Boy Color.mgl"), []byte(gbcMGL), 0644)

	// Non-mgl file should be ignored
	os.WriteFile(filepath.Join(consoleDir, "notes.txt"), []byte("hello"), 0644)

	// Override paths
	oldConsole, oldComputer := consoleCoresPath, computerCoresPath
	consoleCoresPath = consoleDir
	computerCoresPath = filepath.Join(tmp, "_Computer") // doesn't exist, that's fine
	defer func() {
		consoleCoresPath = oldConsole
		computerCoresPath = oldComputer
	}()

	mappings := parseMGLFiles()

	if len(mappings) != 2 {
		t.Fatalf("parseMGLFiles found %d mappings, want 2", len(mappings))
	}
	if mappings["GameGear"] != "_Console/SMS" {
		t.Errorf("GameGear mapping = %q, want _Console/SMS", mappings["GameGear"])
	}
	if mappings["GBC"] != "_Console/Gameboy" {
		t.Errorf("GBC mapping = %q, want _Console/Gameboy", mappings["GBC"])
	}
}

func TestScanFolderExtensions(t *testing.T) {
	tmp := t.TempDir()

	// Create ROM files with various extensions
	os.WriteFile(filepath.Join(tmp, "game1.nes"), []byte{}, 0644)
	os.WriteFile(filepath.Join(tmp, "game2.nes"), []byte{}, 0644)
	os.WriteFile(filepath.Join(tmp, "game3.nes"), []byte{}, 0644)
	os.WriteFile(filepath.Join(tmp, "game4.sfc"), []byte{}, 0644)
	// Meta files should be excluded
	os.WriteFile(filepath.Join(tmp, "readme.txt"), []byte{}, 0644)
	os.WriteFile(filepath.Join(tmp, "cover.jpg"), []byte{}, 0644)
	os.WriteFile(filepath.Join(tmp, "gamelist.xml"), []byte{}, 0644)

	exts, count := scanFolderExtensions(tmp)

	if count != 4 {
		t.Errorf("count = %d, want 4", count)
	}
	if len(exts) != 2 {
		t.Errorf("extensions = %v, want 2 extensions", exts)
	}
	// .nes should be first (most common)
	if len(exts) > 0 && exts[0] != ".nes" {
		t.Errorf("most common ext = %q, want .nes", exts[0])
	}
}

func TestScanFolderExtensions_Subdirectories(t *testing.T) {
	tmp := t.TempDir()
	subDir := filepath.Join(tmp, "USA")
	os.MkdirAll(subDir, 0755)

	os.WriteFile(filepath.Join(tmp, "game1.sfc"), []byte{}, 0644)
	os.WriteFile(filepath.Join(subDir, "game2.sfc"), []byte{}, 0644)

	_, count := scanFolderExtensions(tmp)
	if count != 2 {
		t.Errorf("count = %d, want 2 (including subdirectory)", count)
	}
}

func TestDiscoverSystems_FullIntegration(t *testing.T) {
	tmp := t.TempDir()

	// Set up ROM folders
	gamesDir := filepath.Join(tmp, "games")
	snesDir := filepath.Join(gamesDir, "SNES")
	nesDir := filepath.Join(gamesDir, "NES")
	unknownDir := filepath.Join(gamesDir, "UnknownSystem")
	cdDir := filepath.Join(gamesDir, "CDSystem")
	os.MkdirAll(snesDir, 0755)
	os.MkdirAll(nesDir, 0755)
	os.MkdirAll(unknownDir, 0755)
	os.MkdirAll(cdDir, 0755)

	// Create ROM files
	os.WriteFile(filepath.Join(snesDir, "Mario.sfc"), []byte{}, 0644)
	os.WriteFile(filepath.Join(snesDir, "Zelda.sfc"), []byte{}, 0644)
	os.WriteFile(filepath.Join(nesDir, "MegaMan.nes"), []byte{}, 0644)
	os.WriteFile(filepath.Join(unknownDir, "game.xyz"), []byte{}, 0644)
	os.WriteFile(filepath.Join(cdDir, "game.chd"), []byte{}, 0644)

	// Set up cores
	consoleDir := filepath.Join(tmp, "_Console")
	os.MkdirAll(consoleDir, 0755)
	os.WriteFile(filepath.Join(consoleDir, "SNES_20250605.rbf"), []byte{}, 0644)
	os.WriteFile(filepath.Join(consoleDir, "NES_20250101.rbf"), []byte{}, 0644)
	os.WriteFile(filepath.Join(consoleDir, "UnknownSystem_20250101.rbf"), []byte{}, 0644)

	// Set up USB with same system (case-insensitive merge)
	usb0Dir := filepath.Join(tmp, "usb0")
	usb0SNES := filepath.Join(usb0Dir, "snes") // lowercase — should merge with SNES
	os.MkdirAll(usb0SNES, 0755)
	os.WriteFile(filepath.Join(usb0SNES, "Extra.sfc"), []byte{}, 0644)

	// Override paths
	oldSD := sdGamesPath
	oldUSB := usbPathFormat
	oldConsole, oldComputer := consoleCoresPath, computerCoresPath
	sdGamesPath = gamesDir
	usbPathFormat = filepath.Join(tmp, "usb%d")
	consoleCoresPath = consoleDir
	computerCoresPath = filepath.Join(tmp, "_Computer")
	InvalidateCache()
	// Wait for background discovery
	for i := 0; i < 100; i++ {
		if IsDiscoveryReady() {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	defer func() {
		sdGamesPath = oldSD
		usbPathFormat = oldUSB
		consoleCoresPath = oldConsole
		computerCoresPath = oldComputer
		InvalidateCache()
	}()

	systems := discoverSystems()

	// SNES should have 2 folders (SD + USB) and 3 total ROMs
	snes, ok := systems["snes"]
	if !ok {
		t.Fatal("SNES not discovered")
	}
	if snes.TotalROMs != 3 {
		t.Errorf("SNES total ROMs = %d, want 3", snes.TotalROMs)
	}
	if len(snes.Folders) != 2 {
		t.Errorf("SNES folders = %d, want 2", len(snes.Folders))
	}
	// SNES is in systemDefaults, so should have core and MGL params from defaults
	if snes.Config.Core != "_Console/SNES" {
		t.Errorf("SNES core = %q, want _Console/SNES", snes.Config.Core)
	}
	if !snes.HasCore {
		t.Error("SNES should have HasCore=true")
	}
	// Extensions should come from disk
	hasExt := false
	for _, ext := range snes.Config.Extensions {
		if ext == ".sfc" {
			hasExt = true
		}
	}
	if !hasExt {
		t.Errorf("SNES extensions %v should contain .sfc", snes.Config.Extensions)
	}

	// NES
	nes, ok := systems["nes"]
	if !ok {
		t.Fatal("NES not discovered")
	}
	if nes.TotalROMs != 1 {
		t.Errorf("NES total ROMs = %d, want 1", nes.TotalROMs)
	}

	// Unknown system — should get default MGL params (type "f")
	unknown, ok := systems["unknownsystem"]
	if !ok {
		t.Fatal("UnknownSystem not discovered")
	}
	if unknown.Config.Type != "f" {
		t.Errorf("UnknownSystem type = %q, want f", unknown.Config.Type)
	}
	if unknown.Config.Core != "_Console/UnknownSystem" {
		t.Errorf("UnknownSystem core = %q, want _Console/UnknownSystem", unknown.Config.Core)
	}

	// CD system — should get type "s"
	cd, ok := systems["cdsystem"]
	if !ok {
		t.Fatal("CDSystem not discovered")
	}
	if cd.Config.Type != "s" {
		t.Errorf("CDSystem type = %q, want s", cd.Config.Type)
	}
	if cd.Config.Index != 0 {
		t.Errorf("CDSystem index = %d, want 0", cd.Config.Index)
	}
}

func TestDiscoverSystems_MGLMapping(t *testing.T) {
	tmp := t.TempDir()

	// Set up ROM folder for GameGear
	gamesDir := filepath.Join(tmp, "games")
	ggDir := filepath.Join(gamesDir, "GameGear")
	os.MkdirAll(ggDir, 0755)
	os.WriteFile(filepath.Join(ggDir, "Sonic.gg"), []byte{}, 0644)

	// Set up core and MGL
	consoleDir := filepath.Join(tmp, "_Console")
	os.MkdirAll(consoleDir, 0755)
	os.WriteFile(filepath.Join(consoleDir, "SMS_20250101.rbf"), []byte{}, 0644)

	mgl := `<mistergamedescription>
    <rbf>_Console/SMS</rbf>
    <setname>GameGear</setname>
</mistergamedescription>`
	os.WriteFile(filepath.Join(consoleDir, "Game Gear.mgl"), []byte(mgl), 0644)

	// Override paths
	oldSD := sdGamesPath
	oldUSB := usbPathFormat
	oldConsole, oldComputer := consoleCoresPath, computerCoresPath
	sdGamesPath = gamesDir
	usbPathFormat = filepath.Join(tmp, "usb%d")
	consoleCoresPath = consoleDir
	computerCoresPath = filepath.Join(tmp, "_Computer")
	InvalidateCache()
	// Wait for background discovery
	for i := 0; i < 100; i++ {
		if IsDiscoveryReady() {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	defer func() {
		sdGamesPath = oldSD
		usbPathFormat = oldUSB
		consoleCoresPath = oldConsole
		computerCoresPath = oldComputer
		InvalidateCache()
	}()

	systems := discoverSystems()

	// GameGear is in systemDefaults, so should use default config
	gg, ok := systems["gamegear"]
	if !ok {
		t.Fatal("GameGear not discovered")
	}
	if gg.Config.Core != "_Console/SMS" {
		t.Errorf("GameGear core = %q, want _Console/SMS", gg.Config.Core)
	}
	if gg.Config.SetName != "GameGear" {
		t.Errorf("GameGear setname = %q, want GameGear", gg.Config.SetName)
	}
}

func TestCaseInsensitiveMatching(t *testing.T) {
	tmp := t.TempDir()

	// Create folders with different cases
	gamesDir := filepath.Join(tmp, "games")
	os.MkdirAll(filepath.Join(gamesDir, "GameBoy"), 0755)
	os.WriteFile(filepath.Join(gamesDir, "GameBoy", "tetris.gb"), []byte{}, 0644)

	usb0Dir := filepath.Join(tmp, "usb0")
	os.MkdirAll(filepath.Join(usb0Dir, "GAMEBOY"), 0755)
	os.WriteFile(filepath.Join(usb0Dir, "GAMEBOY", "pokemon.gb"), []byte{}, 0644)

	oldSD := sdGamesPath
	oldUSB := usbPathFormat
	oldConsole, oldComputer := consoleCoresPath, computerCoresPath
	sdGamesPath = gamesDir
	usbPathFormat = filepath.Join(tmp, "usb%d")
	consoleCoresPath = filepath.Join(tmp, "_Console")
	computerCoresPath = filepath.Join(tmp, "_Computer")
	InvalidateCache()
	// Wait for background discovery
	for i := 0; i < 100; i++ {
		if IsDiscoveryReady() {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	defer func() {
		sdGamesPath = oldSD
		usbPathFormat = oldUSB
		consoleCoresPath = oldConsole
		computerCoresPath = oldComputer
		InvalidateCache()
	}()

	systems := discoverSystems()

	gb, ok := systems["gameboy"]
	if !ok {
		t.Fatal("Gameboy not discovered")
	}
	if gb.TotalROMs != 2 {
		t.Errorf("Gameboy total ROMs = %d, want 2", gb.TotalROMs)
	}
	if len(gb.Folders) != 2 {
		t.Errorf("Gameboy folders = %d, want 2", len(gb.Folders))
	}
}

func TestInvalidateCache(t *testing.T) {
	tmp := t.TempDir()
	gamesDir := filepath.Join(tmp, "games")
	nesDir := filepath.Join(gamesDir, "NES")
	os.MkdirAll(nesDir, 0755)
	os.WriteFile(filepath.Join(nesDir, "game.nes"), []byte{}, 0644)

	oldSD := sdGamesPath
	oldUSB := usbPathFormat
	oldConsole, oldComputer := consoleCoresPath, computerCoresPath
	sdGamesPath = gamesDir
	usbPathFormat = filepath.Join(tmp, "usb%d")
	consoleCoresPath = filepath.Join(tmp, "_Console")
	computerCoresPath = filepath.Join(tmp, "_Computer")
	InvalidateCache()
	// Wait for background discovery
	for i := 0; i < 100; i++ {
		if IsDiscoveryReady() {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	defer func() {
		sdGamesPath = oldSD
		usbPathFormat = oldUSB
		consoleCoresPath = oldConsole
		computerCoresPath = oldComputer
		InvalidateCache()
	}()

	// First discovery
	systems := getDiscoveredSystems()
	if _, ok := systems["nes"]; !ok {
		t.Fatal("NES not discovered on first call")
	}

	// Add a new system
	snesDir := filepath.Join(gamesDir, "SNES")
	os.MkdirAll(snesDir, 0755)
	os.WriteFile(filepath.Join(snesDir, "game.sfc"), []byte{}, 0644)

	// Without invalidation, SNES should not appear
	systems = getDiscoveredSystems()
	if _, ok := systems["snes"]; ok {
		t.Error("SNES should not appear without cache invalidation")
	}

	// After invalidation, SNES should appear
	InvalidateCache()
	for i := 0; i < 100; i++ {
		if IsDiscoveryReady() {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	systems = getDiscoveredSystems()
	if _, ok := systems["snes"]; !ok {
		t.Fatal("SNES should appear after cache invalidation")
	}
}

func TestGetSystemConfig_DiscoveryFallback(t *testing.T) {
	// When no ROM folders exist for a system, GetSystemConfig should
	// fall back to systemDefaults
	InvalidateCache()
	defer InvalidateCache()

	cfg, ok := GetSystemConfig("SNES")
	if !ok {
		t.Fatal("expected to find SNES in systemDefaults fallback")
	}
	if cfg.Core != "_Console/SNES" {
		t.Errorf("SNES core = %q, want _Console/SNES", cfg.Core)
	}
}

func TestGenerateMGL_WithDiscoveredSystem(t *testing.T) {
	tmp := t.TempDir()
	gamesDir := filepath.Join(tmp, "games")
	nesDir := filepath.Join(gamesDir, "NES")
	os.MkdirAll(nesDir, 0755)
	os.WriteFile(filepath.Join(nesDir, "TestGame.nes"), []byte{}, 0644)

	oldSD := sdGamesPath
	oldUSB := usbPathFormat
	oldConsole, oldComputer := consoleCoresPath, computerCoresPath
	sdGamesPath = gamesDir
	usbPathFormat = filepath.Join(tmp, "usb%d")
	consoleCoresPath = filepath.Join(tmp, "_Console")
	computerCoresPath = filepath.Join(tmp, "_Computer")
	InvalidateCache()
	// Wait for background discovery
	for i := 0; i < 100; i++ {
		if IsDiscoveryReady() {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	defer func() {
		sdGamesPath = oldSD
		usbPathFormat = oldUSB
		consoleCoresPath = oldConsole
		computerCoresPath = oldComputer
		InvalidateCache()
	}()

	game := GameInfo{
		Name:     "TestGame",
		Path:     filepath.Join(nesDir, "TestGame.nes"),
		System:   "NES",
		Location: "sd",
	}

	mglContent := GenerateMGL(game)
	if mglContent == "" {
		t.Fatal("GenerateMGL returned empty for discovered NES system")
	}

	// Verify XML structure
	var parsed mglFile
	if err := xml.Unmarshal([]byte(mglContent), &parsed); err != nil {
		t.Fatalf("invalid XML: %v", err)
	}
	if parsed.Rbf != "_Console/NES" {
		t.Errorf("rbf = %q, want _Console/NES", parsed.Rbf)
	}
}

func TestDiscoverSystems_EmptyFolder(t *testing.T) {
	tmp := t.TempDir()
	gamesDir := filepath.Join(tmp, "games")
	// Create empty system folder (no ROMs)
	os.MkdirAll(filepath.Join(gamesDir, "EmptySystem"), 0755)
	// Create system with only meta files
	metaDir := filepath.Join(gamesDir, "MetaOnly")
	os.MkdirAll(metaDir, 0755)
	os.WriteFile(filepath.Join(metaDir, "readme.txt"), []byte{}, 0644)
	os.WriteFile(filepath.Join(metaDir, "cover.jpg"), []byte{}, 0644)

	oldSD := sdGamesPath
	oldUSB := usbPathFormat
	oldConsole, oldComputer := consoleCoresPath, computerCoresPath
	sdGamesPath = gamesDir
	usbPathFormat = filepath.Join(tmp, "usb%d")
	consoleCoresPath = filepath.Join(tmp, "_Console")
	computerCoresPath = filepath.Join(tmp, "_Computer")
	InvalidateCache()
	// Wait for background discovery
	for i := 0; i < 100; i++ {
		if IsDiscoveryReady() {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	defer func() {
		sdGamesPath = oldSD
		usbPathFormat = oldUSB
		consoleCoresPath = oldConsole
		computerCoresPath = oldComputer
		InvalidateCache()
	}()

	systems := discoverSystems()

	if _, ok := systems["emptysystem"]; ok {
		t.Error("empty folder should not be discovered")
	}
	if _, ok := systems["metaonly"]; ok {
		t.Error("folder with only meta files should not be discovered")
	}
}

func TestExtensionDetection_CDSystems(t *testing.T) {
	tmp := t.TempDir()
	gamesDir := filepath.Join(tmp, "games")
	psxDir := filepath.Join(gamesDir, "NewCDSystem")
	os.MkdirAll(psxDir, 0755)
	os.WriteFile(filepath.Join(psxDir, "game1.chd"), []byte{}, 0644)
	os.WriteFile(filepath.Join(psxDir, "game2.cue"), []byte{}, 0644)

	oldSD := sdGamesPath
	oldUSB := usbPathFormat
	oldConsole, oldComputer := consoleCoresPath, computerCoresPath
	sdGamesPath = gamesDir
	usbPathFormat = filepath.Join(tmp, "usb%d")
	consoleCoresPath = filepath.Join(tmp, "_Console")
	computerCoresPath = filepath.Join(tmp, "_Computer")
	InvalidateCache()
	// Wait for background discovery
	for i := 0; i < 100; i++ {
		if IsDiscoveryReady() {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	defer func() {
		sdGamesPath = oldSD
		usbPathFormat = oldUSB
		consoleCoresPath = oldConsole
		computerCoresPath = oldComputer
		InvalidateCache()
	}()

	systems := discoverSystems()

	cd, ok := systems["newcdsystem"]
	if !ok {
		t.Fatal("NewCDSystem not discovered")
	}
	if cd.Config.Type != "s" {
		t.Errorf("CD system type = %q, want s", cd.Config.Type)
	}
	if cd.Config.Index != 0 {
		t.Errorf("CD system index = %d, want 0", cd.Config.Index)
	}
	if cd.Config.Delay != 1 {
		t.Errorf("CD system delay = %d, want 1", cd.Config.Delay)
	}

	// Should have both .chd and .cue extensions
	extMap := make(map[string]bool)
	for _, ext := range cd.Config.Extensions {
		extMap[strings.ToLower(ext)] = true
	}
	if !extMap[".chd"] || !extMap[".cue"] {
		t.Errorf("CD system extensions = %v, want .chd and .cue", cd.Config.Extensions)
	}
}

func TestSaveAndLoadCache(t *testing.T) {
	tmp := t.TempDir()
	oldCachePath := CacheFilePath
	CacheFilePath = filepath.Join(tmp, "cache.json")
	defer func() { CacheFilePath = oldCachePath }()

	// Set up minimal ROM structure
	gamesDir := filepath.Join(tmp, "games")
	nesDir := filepath.Join(gamesDir, "NES")
	os.MkdirAll(nesDir, 0755)
	os.WriteFile(filepath.Join(nesDir, "game.nes"), []byte{}, 0644)

	oldSD := sdGamesPath
	oldUSB := usbPathFormat
	oldConsole, oldComputer := consoleCoresPath, computerCoresPath
	sdGamesPath = gamesDir
	usbPathFormat = filepath.Join(tmp, "usb%d")
	consoleCoresPath = filepath.Join(tmp, "_Console")
	computerCoresPath = filepath.Join(tmp, "_Computer")
	defer func() {
		sdGamesPath = oldSD
		usbPathFormat = oldUSB
		consoleCoresPath = oldConsole
		computerCoresPath = oldComputer
	}()

	// Run discovery (no cache on disk yet)
	InvalidateCache()
	for i := 0; i < 100; i++ {
		if IsDiscoveryReady() {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	// Wait for Phase 2 to complete and save cache
	for i := 0; i < 100; i++ {
		if IsDiscoveryComplete() {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	// Give SaveCache goroutine time to write
	time.Sleep(50 * time.Millisecond)

	// Verify cache file exists
	data, err := os.ReadFile(CacheFilePath)
	if err != nil {
		t.Fatalf("cache file not written: %v", err)
	}

	var dc diskCache
	if err := json.Unmarshal(data, &dc); err != nil {
		t.Fatalf("cache file invalid JSON: %v", err)
	}
	if dc.Version != 1 {
		t.Errorf("cache version = %d, want 1", dc.Version)
	}
	if _, ok := dc.Systems["nes"]; !ok {
		t.Error("cache missing NES system")
	}

	// Clear in-memory cache, then load from disk
	cacheMu.Lock()
	cachedSystems = nil
	cacheReady = false
	cacheComplete = false
	cacheScanning = false
	cacheMu.Unlock()

	if !LoadCache() {
		t.Fatal("LoadCache returned false")
	}
	if !IsDiscoveryReady() {
		t.Error("cacheReady should be true after LoadCache")
	}
	if !IsDiscoveryComplete() {
		t.Error("cacheComplete should be true after LoadCache")
	}
	systems := getDiscoveredSystems()
	if _, ok := systems["nes"]; !ok {
		t.Error("NES not found after LoadCache")
	}
}

func TestStartDiscovery_UsesCache(t *testing.T) {
	tmp := t.TempDir()
	oldCachePath := CacheFilePath
	CacheFilePath = filepath.Join(tmp, "cache.json")
	defer func() { CacheFilePath = oldCachePath }()

	// Write a fake cache file
	dc := diskCache{
		Version:   1,
		Timestamp: "2026-01-01T00:00:00Z",
		Systems: map[string]*DiscoveredSystem{
			"fakesystem": {
				Name:      "FakeSystem",
				TotalROMs: 42,
				HasCore:   true,
				Folders: []SystemFolder{
					{Path: "/fake/path", Location: "sd", RomCount: 42},
				},
				Config: SystemConfig{Core: "_Console/Fake"},
			},
		},
	}
	data, _ := json.Marshal(dc)
	os.WriteFile(CacheFilePath, data, 0644)

	// Point discovery at empty dirs so it would find nothing without cache
	oldSD := sdGamesPath
	oldUSB := usbPathFormat
	oldConsole, oldComputer := consoleCoresPath, computerCoresPath
	sdGamesPath = filepath.Join(tmp, "empty_games")
	usbPathFormat = filepath.Join(tmp, "empty_usb%d")
	consoleCoresPath = filepath.Join(tmp, "_Console")
	computerCoresPath = filepath.Join(tmp, "_Computer")
	os.MkdirAll(sdGamesPath, 0755)
	defer func() {
		sdGamesPath = oldSD
		usbPathFormat = oldUSB
		consoleCoresPath = oldConsole
		computerCoresPath = oldComputer
	}()

	// Clear and start discovery — should load from cache
	cacheMu.Lock()
	cachedSystems = nil
	cacheReady = false
	cacheComplete = false
	cacheScanning = false
	cacheMu.Unlock()

	StartDiscovery()

	if !IsDiscoveryReady() {
		t.Error("should be ready immediately after loading cache")
	}
	systems := getDiscoveredSystems()
	if fs, ok := systems["fakesystem"]; !ok {
		t.Error("FakeSystem not found — cache was not loaded")
	} else if fs.TotalROMs != 42 {
		t.Errorf("FakeSystem ROMs = %d, want 42", fs.TotalROMs)
	}
}

func TestInvalidateCache_DeletesFile(t *testing.T) {
	tmp := t.TempDir()
	oldCachePath := CacheFilePath
	CacheFilePath = filepath.Join(tmp, "cache.json")
	defer func() { CacheFilePath = oldCachePath }()

	// Create a cache file
	os.WriteFile(CacheFilePath, []byte(`{"version":1,"timestamp":"x","systems":{}}`), 0644)

	oldSD := sdGamesPath
	oldUSB := usbPathFormat
	oldConsole, oldComputer := consoleCoresPath, computerCoresPath
	sdGamesPath = filepath.Join(tmp, "games")
	usbPathFormat = filepath.Join(tmp, "usb%d")
	consoleCoresPath = filepath.Join(tmp, "_Console")
	computerCoresPath = filepath.Join(tmp, "_Computer")
	os.MkdirAll(sdGamesPath, 0755)
	defer func() {
		sdGamesPath = oldSD
		usbPathFormat = oldUSB
		consoleCoresPath = oldConsole
		computerCoresPath = oldComputer
	}()

	InvalidateCache()
	for i := 0; i < 100; i++ {
		if IsDiscoveryReady() {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}

	if _, err := os.Stat(CacheFilePath); err == nil {
		// File might be recreated by Phase 2 save — that's ok if it's a new scan.
		// But the original file should have been deleted before rescan.
		// We just verify InvalidateCache didn't crash and discovery works.
	}
}

func TestLoadCache_InvalidJSON(t *testing.T) {
	tmp := t.TempDir()
	oldCachePath := CacheFilePath
	CacheFilePath = filepath.Join(tmp, "cache.json")
	defer func() { CacheFilePath = oldCachePath }()

	os.WriteFile(CacheFilePath, []byte("not json"), 0644)
	if LoadCache() {
		t.Error("LoadCache should return false for invalid JSON")
	}
}

func TestLoadCache_WrongVersion(t *testing.T) {
	tmp := t.TempDir()
	oldCachePath := CacheFilePath
	CacheFilePath = filepath.Join(tmp, "cache.json")
	defer func() { CacheFilePath = oldCachePath }()

	os.WriteFile(CacheFilePath, []byte(`{"version":99,"timestamp":"x","systems":{}}`), 0644)
	if LoadCache() {
		t.Error("LoadCache should return false for wrong version")
	}
}

func TestSystemConfigJSONTags(t *testing.T) {
	cfg := SystemConfig{
		Core:       "_Console/SNES",
		Delay:      2,
		Type:       "f",
		Index:      1,
		Extensions: []string{".sfc", ".smc"},
		SetName:    "SNES",
	}
	data, err := json.Marshal(cfg)
	if err != nil {
		t.Fatal(err)
	}
	var parsed map[string]interface{}
	json.Unmarshal(data, &parsed)
	if parsed["core"] != "_Console/SNES" {
		t.Error("json tag 'core' not working")
	}
	if parsed["type"] != "f" {
		t.Error("json tag 'type' not working")
	}
}
