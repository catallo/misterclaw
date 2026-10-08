package mister

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"sync"
	"testing"
)

func arcadeDiscoveryEnvironment(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	oldSD, oldUSB := sdGamesPath, usbPathFormat
	oldConsole, oldComputer, oldArcade := consoleCoresPath, computerCoresPath, arcadePath
	oldCachePath := CacheFilePath
	cacheMu.Lock()
	oldSystems, oldGames := cachedSystems, cachedGames
	oldReady, oldComplete, oldGamesReady, oldScanning := cacheReady, cacheComplete, gamesReady, cacheScanning
	cachedSystems, cachedGames = nil, nil
	cacheReady, cacheComplete, gamesReady, cacheScanning = false, false, false, false
	cacheMu.Unlock()
	sdGamesPath = filepath.Join(root, "games")
	usbPathFormat = filepath.Join(root, "usb%d")
	consoleCoresPath, computerCoresPath = filepath.Join(root, "_Console"), filepath.Join(root, "_Computer")
	arcadePath = filepath.Join(root, "_Arcade")
	CacheFilePath = filepath.Join(root, "config", "cache.json")
	t.Cleanup(func() {
		rescanMu.Lock()
		defer rescanMu.Unlock()
		sdGamesPath, usbPathFormat = oldSD, oldUSB
		consoleCoresPath, computerCoresPath, arcadePath = oldConsole, oldComputer, oldArcade
		CacheFilePath = oldCachePath
		cacheMu.Lock()
		cachedSystems, cachedGames = oldSystems, oldGames
		cacheReady, cacheComplete, gamesReady, cacheScanning = oldReady, oldComplete, oldGamesReady, oldScanning
		cacheMu.Unlock()
	})
	return root
}

func arcadeDiscoveryWrite(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}
}

func arcadeDiscoverySeed(t *testing.T) {
	t.Helper()
	systems := discoverSystems()
	games := collectAllGames(systems)
	cacheMu.Lock()
	cachedSystems, cachedGames = systems, games
	cacheReady, cacheComplete, gamesReady, cacheScanning = true, true, true, false
	cacheMu.Unlock()
}

func TestArcadeDiscoverySelectionAndCounts(t *testing.T) {
	cases := []struct {
		name  string
		files map[string]string
		count int
	}{
		{"canonical_skips_generated_views", map[string]string{
			"Game.mra": "same", "_Organized/1981/Game.mra": "same", "_Organized/Other/Other.mra": "different",
		}, 1},
		{"recursive_alternatives_and_real_subfolders", map[string]string{
			"_alternatives/Board/Variant.MRA": "variant", "Real/Deep/Board/Game.mra": "canonical", "Real/Deep/readme.txt": "ignored",
			"_Organized/1981/Game.mra": "canonical",
		}, 2},
		{"organized_only_fallback_retains_distinct_variants", map[string]string{
			"_Organized/1981/Game.mra": "original", "_Organized/Genre/Copy.mra": "original",
			"_Organized/Revision/Game.mra": "variant",
		}, 2},
		{"same_basename_different_canonical_content", map[string]string{
			"Game.mra": "original", "_alternatives/Game.mra": "variant",
		}, 2},
		{"canonical_not_content_deduplicated", map[string]string{
			"Game.mra": "same", "Real/Game.mra": "same",
		}, 2},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			arcadeDiscoveryEnvironment(t)
			for relative, content := range tc.files {
				arcadeDiscoveryWrite(t, filepath.Join(arcadePath, relative), content)
			}
			systems := discoverSystems()
			ds := systems["arcade"]
			if ds == nil || ds.TotalROMs != tc.count || len(ds.Folders) != 1 || ds.Folders[0].RomCount != tc.count {
				t.Fatalf("discovery count mismatch: %+v, want %d", ds, tc.count)
			}
			indexed := collectAllGames(systems)["arcade"]
			direct := scanArcadeFolder(arcadePath, "Arcade", "sd")
			if len(indexed) != tc.count || !reflect.DeepEqual(indexed, direct) {
				t.Fatalf("index mismatch: collected=%+v direct=%+v", indexed, direct)
			}
			if got := scanDir(arcadePath, "Arcade", "sd", map[string]bool{".mra": true}); !reflect.DeepEqual(got, direct) {
				t.Fatalf("scanDir must use Arcade selection: %+v", got)
			}
		})
	}
}

func TestArcadeDiscoveryMergesAllRootsAndSkipsMAME(t *testing.T) {
	root := arcadeDiscoveryEnvironment(t)
	for _, path := range []string{
		filepath.Join(arcadePath, "Board/SD.mra"), filepath.Join(sdGamesPath, "Arcade/SD Games.mra"),
		filepath.Join(root, "usb0/Arcade/Deep/USB.mra"),
	} {
		arcadeDiscoveryWrite(t, path, "identical content is not collapsed across physical roots")
	}
	arcadeDiscoveryWrite(t, filepath.Join(sdGamesPath, "MAME/rom.zip"), "zip")
	arcadeDiscoveryWrite(t, filepath.Join(root, "usb0/mAmE/rom.zip"), "zip")
	systems := discoverSystems()
	ds := systems["arcade"]
	if ds == nil || len(ds.Folders) != 3 || ds.TotalROMs != 3 {
		t.Fatalf("expected three merged Arcade roots: %+v", ds)
	}
	if systems["mame"] != nil {
		t.Fatal("MAME archives must not be discovered as a playable system")
	}
	games := collectAllGames(systems)["arcade"]
	if len(games) != 3 {
		t.Fatalf("index has %d games, want 3", len(games))
	}
}

func TestArcadeDiscoverySDRescanPreservesUnchangedUSB(t *testing.T) {
	root := arcadeDiscoveryEnvironment(t)
	oldSD := filepath.Join(arcadePath, "Old.mra")
	usbArcade := filepath.Join(root, "usb0/Arcade/USB.mra")
	usbNES := filepath.Join(root, "usb0/NES/USB.nes")
	usbGBA := filepath.Join(root, "usb1/GBA/Unrelated.gba")
	for _, path := range []string{oldSD, usbArcade, usbNES, usbGBA, filepath.Join(sdGamesPath, "NES/SD.nes")} {
		arcadeDiscoveryWrite(t, path, "game")
	}
	arcadeDiscoverySeed(t)
	before := getCachedGames()
	// Deleted USB files prove SD rescan reuses untouched listings even for
	// systems present on both locations, rather than rereading USB folders.
	for _, path := range []string{oldSD, usbArcade, usbNES, usbGBA} {
		if err := os.Remove(path); err != nil {
			t.Fatal(err)
		}
	}
	newSD := filepath.Join(arcadePath, "Real/Deep/New.mra")
	arcadeDiscoveryWrite(t, newSD, "new")
	arcadeDiscoveryWrite(t, filepath.Join(sdGamesPath, "Arcade/Other.mra"), "other")
	arcadeDiscoveryWrite(t, filepath.Join(sdGamesPath, "MAME/rom.zip"), "rom")
	if count := RescanLocation("sd"); count != 2 {
		t.Fatalf("rescan found %d systems, want Arcade and NES", count)
	}
	systems, games := getDiscoveredSystems(), getCachedGames()
	if ds := systems["arcade"]; ds == nil || ds.TotalROMs != 3 || len(ds.Folders) != 3 {
		t.Fatalf("SD rescan lost merged Arcade: %+v", ds)
	}
	if systems["mame"] != nil || games["mame"] != nil {
		t.Fatal("SD rescan indexed MAME archives")
	}
	if !reflect.DeepEqual(games["gba"], before["gba"]) {
		t.Fatal("unrelated USB GBA listing changed")
	}
	for _, key := range []string{"arcade", "nes"} {
		var oldUSB, newUSB []GameInfo
		for _, game := range before[key] {
			if game.Location != "sd" {
				oldUSB = append(oldUSB, game)
			}
		}
		for _, game := range games[key] {
			if game.Location != "sd" {
				newUSB = append(newUSB, game)
			}
		}
		if !reflect.DeepEqual(oldUSB, newUSB) {
			t.Fatalf("%s USB entries changed", key)
		}
	}
	for _, game := range games["arcade"] {
		if game.Path == oldSD {
			t.Fatal("removed SD MRA remained cached")
		}
	}
	if len(games["arcade"]) != systems["arcade"].TotalROMs {
		t.Fatal("Arcade count/index mismatch after rescan")
	}
}

func TestArcadeDiscoveryUSBRescanPreservesSD(t *testing.T) {
	root := arcadeDiscoveryEnvironment(t)
	sdMRA := filepath.Join(arcadePath, "SD.mra")
	oldUSB := filepath.Join(root, "usb0/Arcade/Old.mra")
	usbOther := filepath.Join(root, "usb1/Arcade/Other.mra")
	for _, path := range []string{sdMRA, oldUSB, usbOther} {
		arcadeDiscoveryWrite(t, path, "game")
	}
	arcadeDiscoverySeed(t)
	for _, path := range []string{sdMRA, oldUSB, usbOther} {
		if err := os.Remove(path); err != nil {
			t.Fatal(err)
		}
	}
	arcadeDiscoveryWrite(t, filepath.Join(root, "usb0/Arcade/_alternatives/Board/New.mra"), "new")
	arcadeDiscoveryWrite(t, filepath.Join(root, "usb0/MAME/rom.zip"), "rom")
	if RescanLocation("usb0") != 1 {
		t.Fatal("USB rescan did not find Arcade")
	}
	systems, games := getDiscoveredSystems(), getCachedGames()
	if ds := systems["arcade"]; ds == nil || ds.TotalROMs != 3 || len(ds.Folders) != 3 || len(games["arcade"]) != 3 {
		t.Fatalf("USB rescan lost untouched locations: %+v", ds)
	}
	if systems["mame"] != nil {
		t.Fatal("USB rescan indexed MAME archives")
	}
	for _, game := range games["arcade"] {
		if game.Path == oldUSB {
			t.Fatal("removed USB MRA remained cached")
		}
	}
}

func TestArcadeDiscoveryCacheMigrationPreservesOtherSystems(t *testing.T) {
	for _, version := range []int{1, 2} {
		t.Run(fmt.Sprintf("v%d", version), func(t *testing.T) {
			root := arcadeDiscoveryEnvironment(t)
			arcadeDiscoveryWrite(t, filepath.Join(arcadePath, "Real/Deep/New.mra"), "new")
			arcadeDiscoveryWrite(t, filepath.Join(root, "usb0/Arcade/USB.mra"), "usb")
			oldGame := GameInfo{Name: "Untouched", Path: "/absent/NES/Untouched.nes", System: "NES", Location: "usb7"}
			oldSystem := &DiscoveredSystem{Name: "NES", TotalROMs: 42, HasCore: true, Config: SystemConfig{Extensions: []string{".nes"}}, Folders: []SystemFolder{{Path: "/absent/NES", Location: "usb7", RomCount: 42}}}
			dc := diskCache{Version: version, Systems: map[string]*DiscoveredSystem{
				"nes": oldSystem, "arcade": {Name: "Arcade", TotalROMs: 0}, "mame": {Name: "MAME", TotalROMs: 100},
			}, Games: map[string][]GameInfo{"nes": {oldGame}, "mame": {{Name: "ROM archive"}}}}
			data, err := json.Marshal(dc)
			if err != nil {
				t.Fatal(err)
			}
			arcadeDiscoveryWrite(t, CacheFilePath, string(data))
			if !LoadCache() {
				t.Fatal("known old cache should migrate successfully")
			}
			systems, games := getDiscoveredSystems(), getCachedGames()
			if !reflect.DeepEqual(systems["nes"], oldSystem) {
				t.Fatal("migration changed unrelated NES discovery")
			}
			if ds := systems["arcade"]; ds == nil || ds.TotalROMs != 2 || len(ds.Folders) != 2 {
				t.Fatalf("migration failed to merge Arcade: %+v", ds)
			}
			if systems["mame"] != nil || games["mame"] != nil {
				t.Fatal("migration retained MAME")
			}
			if version == 2 {
				if !IsGamesReady() || !reflect.DeepEqual(games["nes"], []GameInfo{oldGame}) || len(games["arcade"]) != 2 {
					t.Fatalf("v2 game migration incorrect: %+v", games)
				}
				StartDiscovery()
				cacheMu.RLock()
				scanning := cacheScanning
				cacheMu.RUnlock()
				if scanning {
					t.Fatal("migrated v2 cache triggered a full background scan")
				}
			} else if IsGamesReady() || games != nil {
				t.Fatal("v1 has no authoritative game listings")
			}
			written, err := os.ReadFile(CacheFilePath)
			if err != nil {
				t.Fatal(err)
			}
			var saved diskCache
			if err := json.Unmarshal(written, &saved); err != nil || saved.Version != discoveryCacheVersion {
				t.Fatalf("migration did not persist v3: %s (%v)", written, err)
			}
		})
	}
}

func TestArcadeDiscoveryCacheSnapshotAndAtomicSave(t *testing.T) {
	arcadeDiscoveryEnvironment(t)
	arcadeDiscoveryWrite(t, filepath.Join(arcadePath, "Game.mra"), "game")
	arcadeDiscoverySeed(t)
	systems, games := getDiscoveredSystems(), getCachedGames()
	systems["arcade"].Folders[0].RomCount = 999
	systems["arcade"].Config.Extensions[0] = ".zip"
	games["arcade"][0].Name = "mutated"
	if getDiscoveredSystems()["arcade"].Folders[0].RomCount != 1 || getCachedGames()["arcade"][0].Name == "mutated" {
		t.Fatal("cache snapshots share mutable data")
	}
	if err := SaveCache(); err != nil {
		t.Fatal(err)
	}
	var workers sync.WaitGroup
	for i := 0; i < 4; i++ {
		workers.Add(1)
		go func() {
			defer workers.Done()
			for j := 0; j < 4; j++ {
				if err := SaveCache(); err != nil {
					t.Error(err)
					return
				}
				data, err := os.ReadFile(CacheFilePath)
				var dc diskCache
				if err != nil || json.Unmarshal(data, &dc) != nil || dc.Version != discoveryCacheVersion {
					t.Errorf("non-atomic cache read: %v", err)
					return
				}
				_ = getDiscoveredSystems()
				_ = getCachedGames()
			}
		}()
	}
	if RescanLocation("sd") != 1 {
		t.Error("rescan failed")
	}
	workers.Wait()
	if !LoadCache() || !IsGamesReady() {
		t.Fatal("saved v3 cache did not round-trip")
	}
	matches, err := filepath.Glob(filepath.Join(filepath.Dir(CacheFilePath), ".misterclaw-cache-*"))
	if err != nil || len(matches) != 0 {
		t.Fatalf("cache temporary files leaked: %v (%v)", matches, err)
	}
}

func TestArcadeDiscoveryEmptyGamesCacheRoundTrip(t *testing.T) {
	arcadeDiscoveryEnvironment(t)
	cacheMu.Lock()
	cachedSystems, cachedGames = make(map[string]*DiscoveredSystem), make(map[string][]GameInfo)
	cacheReady, cacheComplete, gamesReady = true, true, true
	cacheMu.Unlock()
	if err := SaveCache(); err != nil {
		t.Fatal(err)
	}
	if !LoadCache() || !IsGamesReady() {
		t.Fatal("empty but complete v3 listings were treated as incomplete")
	}
}
