package mister

import (
	"os"
	"path/filepath"
	"testing"
)

func TestPR3ReviewSDRescanPreservesArcade(t *testing.T) {
	reviewArcadeEnvironment(t)
	reviewWriteFile(t, filepath.Join(arcadePath, "1942.mra"), "<misterromdescription/>")
	systems := discoverSystems()
	if systems["arcade"] == nil {
		t.Fatal("precondition: fresh discovery must find Arcade")
	}
	oldCachePath := CacheFilePath
	CacheFilePath = filepath.Join(t.TempDir(), "review-cache.json")
	cacheMu.Lock()
	oldSystems, oldGames := cachedSystems, cachedGames
	oldReady, oldComplete, oldGamesReady, oldScanning := cacheReady, cacheComplete, gamesReady, cacheScanning
	cachedSystems, cachedGames = systems, nil
	cacheReady, cacheComplete, gamesReady, cacheScanning = true, true, true, false
	cacheMu.Unlock()
	t.Cleanup(func() {
		CacheFilePath = oldCachePath
		cacheMu.Lock()
		cachedSystems, cachedGames = oldSystems, oldGames
		cacheReady, cacheComplete, gamesReady, cacheScanning = oldReady, oldComplete, oldGamesReady, oldScanning
		cacheMu.Unlock()
	})
	RescanLocation("sd")
	cacheMu.RLock()
	_, stillPresent := cachedSystems["arcade"]
	cacheMu.RUnlock()
	if !stillPresent {
		t.Fatal("SD-only rescan removed the already indexed Arcade system")
	}
}

func TestPR3ReviewMergeExistingArcadeFolders(t *testing.T) {
	reviewArcadeEnvironment(t)
	usbArcade := filepath.Join(filepath.Dir(arcadePath), "usb0", "Arcade")
	if err := os.MkdirAll(usbArcade, 0755); err != nil {
		t.Fatal(err)
	}
	reviewWriteFile(t, filepath.Join(usbArcade, "USB Game.mra"), "<misterromdescription/>")
	reviewWriteFile(t, filepath.Join(arcadePath, "SD Game.mra"), "<misterromdescription/>")
	systems := discoverSystems()
	ds := systems["arcade"]
	if ds == nil {
		t.Fatal("Arcade system missing")
	}
	games := collectAllGames(systems)["arcade"]
	if len(ds.Folders) != 2 || len(games) != 2 {
		t.Fatalf("expected SD and USB folders/games, got folders=%+v games=%+v", ds.Folders, games)
	}
}
