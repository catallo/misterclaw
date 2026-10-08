package mister

// Additional review-only regression checks. Not part of the upstream PR.
// All filesystem paths and command writes are redirected to t.TempDir().
// No MiSTer, /dev/uinput, real FIFO, or remote host is contacted.

import (
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

func reviewArcadeEnvironment(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	oldSD, oldUSB := sdGamesPath, usbPathFormat
	oldConsole, oldComputer, oldArcade := consoleCoresPath, computerCoresPath, arcadePath
	sdGamesPath = filepath.Join(root, "games")
	usbPathFormat = filepath.Join(root, "usb%d")
	consoleCoresPath = filepath.Join(root, "_Console")
	computerCoresPath = filepath.Join(root, "_Computer")
	arcadePath = filepath.Join(root, "_Arcade")
	for _, path := range []string{sdGamesPath, consoleCoresPath, computerCoresPath, arcadePath} {
		if err := os.MkdirAll(path, 0755); err != nil {
			t.Fatal(err)
		}
	}
	t.Cleanup(func() {
		sdGamesPath, usbPathFormat = oldSD, oldUSB
		consoleCoresPath, computerCoresPath, arcadePath = oldConsole, oldComputer, oldArcade
	})
	return root
}

func reviewWriteFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}
}

func reviewCommandSink(t *testing.T) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "MiSTer_cmd")
	reviewWriteFile(t, path, "")
	old := cmdPath
	cmdPath = path
	t.Cleanup(func() { cmdPath = old })
	return path
}

func TestPR3ReviewTopLevelDiscovery(t *testing.T) {
	reviewArcadeEnvironment(t)
	reviewWriteFile(t, filepath.Join(arcadePath, "Donkey Kong.MRA"), "<misterromdescription><rbf>DonkeyKong</rbf></misterromdescription>")
	reviewWriteFile(t, filepath.Join(arcadePath, "readme.txt"), "not a game")
	systems := discoverSystems()
	ds := systems["arcade"]
	if ds == nil || ds.Name != "Arcade" || ds.TotalROMs != 1 || !reflect.DeepEqual(ds.Config.Extensions, []string{".mra"}) {
		t.Fatalf("top-level Arcade discovery incorrect: %+v", ds)
	}
	if got := len(collectAllGames(systems)["arcade"]); got != 1 {
		t.Fatalf("indexed %d games, want 1", got)
	}
}

func TestPR3ReviewNestedOnlyDiscovery(t *testing.T) {
	reviewArcadeEnvironment(t)
	reviewWriteFile(t, filepath.Join(arcadePath, "_Organized", "1981", "Donkey Kong.mra"), "<misterromdescription/>")
	systems := discoverSystems()
	if systems["arcade"] == nil {
		t.Fatal("Arcade system missing although a nested MRA exists")
	}
	if got := len(collectAllGames(systems)["arcade"]); got != 1 {
		t.Fatalf("indexed %d games, want 1", got)
	}
}

func TestPR3ReviewRecursiveCountsMatchIndex(t *testing.T) {
	reviewArcadeEnvironment(t)
	reviewWriteFile(t, filepath.Join(arcadePath, "Root.mra"), "<misterromdescription/>")
	reviewWriteFile(t, filepath.Join(arcadePath, "_alternatives", "Nested.mra"), "<misterromdescription/>")
	systems := discoverSystems()
	ds := systems["arcade"]
	if ds == nil {
		t.Fatal("Arcade system missing")
	}
	indexed := len(collectAllGames(systems)["arcade"])
	if indexed != 2 {
		t.Fatalf("indexed %d games, want 2", indexed)
	}
	if ds.TotalROMs != indexed || ds.Folders[0].RomCount != indexed {
		t.Fatalf("recursive index has %d games but advertised total=%d folder_count=%d", indexed, ds.TotalROMs, ds.Folders[0].RomCount)
	}
}

func TestPR3ReviewValidMRACommand(t *testing.T) {
	path := filepath.Join(t.TempDir(), "Donkey Kong.MRA")
	reviewWriteFile(t, path, "<misterromdescription><rbf>DonkeyKong</rbf></misterromdescription>")
	sink := reviewCommandSink(t)
	if err := LaunchGame(GameInfo{Name: "Donkey Kong", System: "Arcade", Path: path}); err != nil {
		t.Fatal(err)
	}
	got, err := os.ReadFile(sink)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != "load_core "+path+"\n" {
		t.Fatalf("command=%q", got)
	}
}

func TestPR3ReviewRejectEmptyMRA(t *testing.T) {
	path := filepath.Join(t.TempDir(), "Empty.mra")
	reviewWriteFile(t, path, "")
	sink := reviewCommandSink(t)
	if err := ValidateCorePath(path); err == nil {
		t.Fatal("test precondition: shared guard should reject an empty descriptor")
	}
	if err := LaunchGame(GameInfo{Name: "Empty", System: "Arcade", Path: path}); err == nil {
		got, _ := os.ReadFile(sink)
		t.Fatalf("empty MRA accepted, command written: %q", got)
	}
}

func TestPR3ReviewRejectDirectoryMRA(t *testing.T) {
	path := filepath.Join(t.TempDir(), "Directory.mra")
	if err := os.Mkdir(path, 0755); err != nil {
		t.Fatal(err)
	}
	sink := reviewCommandSink(t)
	if err := ValidateCorePath(path); err == nil {
		t.Fatal("test precondition: shared guard should reject a directory")
	}
	if err := LaunchGame(GameInfo{Name: "Directory", System: "Arcade", Path: path}); err == nil {
		got, _ := os.ReadFile(sink)
		t.Fatalf("directory accepted as MRA, command written: %q", got)
	}
}

type reviewClosingKeyboard struct {
	mockKeyboard
	closed int
}

func (k *reviewClosingKeyboard) Close() error { k.closed++; return nil }

type reviewClosingGamepad struct {
	mockGamepad
	closed int
}

func (g *reviewClosingGamepad) Close() error { g.closed++; return nil }

func TestPR3ReviewRecycleInputOnMRALaunch(t *testing.T) {
	path := filepath.Join(t.TempDir(), "Valid.mra")
	reviewWriteFile(t, path, "<misterromdescription/>")
	reviewCommandSink(t)
	keyboard := &reviewClosingKeyboard{}
	gamepad := &reviewClosingGamepad{}
	oldKB, oldGP := kbInst, gpInst
	kbInst, gpInst = keyboard, gamepad
	t.Cleanup(func() { kbInst, gpInst = oldKB, oldGP })
	if err := LaunchGame(GameInfo{Name: "Valid", System: "Arcade", Path: path}); err != nil {
		t.Fatal(err)
	}
	if keyboard.closed != 1 || gamepad.closed != 1 || kbInst != nil || gpInst != nil {
		t.Fatalf("input recycling bypassed: keyboard_closes=%d gamepad_closes=%d keyboard_retained=%t gamepad_retained=%t", keyboard.closed, gamepad.closed, kbInst != nil, gpInst != nil)
	}
}

// Upgrading a v2 cache refreshes Arcade while preserving unrelated game lists.
func TestPR3ReviewExistingCacheMigratesArcade(t *testing.T) {
	reviewArcadeEnvironment(t)
	reviewWriteFile(t, filepath.Join(arcadePath, "New.mra"), "<misterromdescription/>")
	oldCachePath := CacheFilePath
	CacheFilePath = filepath.Join(t.TempDir(), "old-cache.json")
	cacheMu.Lock()
	oldSystems, oldGames := cachedSystems, cachedGames
	oldReady, oldComplete, oldGamesReady, oldScanning := cacheReady, cacheComplete, gamesReady, cacheScanning
	cachedSystems, cachedGames = nil, nil
	cacheReady, cacheComplete, gamesReady, cacheScanning = false, false, false, false
	cacheMu.Unlock()
	t.Cleanup(func() {
		CacheFilePath = oldCachePath
		cacheMu.Lock()
		cachedSystems, cachedGames = oldSystems, oldGames
		cacheReady, cacheComplete, gamesReady, cacheScanning = oldReady, oldComplete, oldGamesReady, oldScanning
		cacheMu.Unlock()
	})
	cache := diskCache{Version: 2, Systems: map[string]*DiscoveredSystem{
		"snes": {Name: "SNES", Config: SystemConfig{Extensions: []string{".sfc"}}, HasCore: true},
		"mame": {Name: "MAME"},
	}, Games: map[string][]GameInfo{
		"snes": {{Name: "Preserved", Path: "/old-cache/Preserved.sfc", System: "SNES"}},
		"mame": {{Name: "Old ROM ZIP", System: "MAME"}},
	}}
	data, err := json.Marshal(cache)
	if err != nil {
		t.Fatal(err)
	}
	reviewWriteFile(t, CacheFilePath, string(data))
	StartDiscovery()
	cacheMu.RLock()
	_, discovered := cachedSystems["arcade"]
	_, mameSystem := cachedSystems["mame"]
	_, mameGames := cachedGames["mame"]
	preserved := len(cachedGames["snes"]) == 1 && cachedGames["snes"][0].Name == "Preserved"
	arcadeCount := len(cachedGames["arcade"])
	done := gamesReady && !cacheScanning
	cacheMu.RUnlock()
	if !done {
		t.Fatal("v2 migration unexpectedly started an unrelated full scan")
	}
	if !discovered || arcadeCount != 1 {
		t.Fatal("v2 upgrade did not refresh Arcade")
	}
	if !preserved || mameSystem || mameGames {
		t.Fatal("migration lost SNES or retained stale MAME entries")
	}
	saved, err := os.ReadFile(CacheFilePath)
	if err != nil {
		t.Fatal(err)
	}
	var migrated diskCache
	if err := json.Unmarshal(saved, &migrated); err != nil {
		t.Fatal(err)
	}
	if migrated.Version != discoveryCacheVersion {
		t.Fatalf("saved cache version=%d", migrated.Version)
	}
}
