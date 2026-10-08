package mister

import (
	"path/filepath"
	"testing"
	"time"
)

func TestLocationRejectsTrailingGarbage(t *testing.T) {
	for _, location := range []string{"usb0garbage", "usb00", "usb-1", "usb8", "SD"} {
		if locationToPath(location) != "" {
			t.Fatalf("invalid location accepted: %q", location)
		}
	}
}

func TestBackgroundLocationRescanPendingAndCompletion(t *testing.T) {
	arcadeDiscoveryEnvironment(t)
	arcadeDiscoveryWrite(t, filepath.Join(arcadePath, "1942.mra"), "<misterromdescription/>")
	arcadeDiscoverySeed(t)
	// Keep persistence blocked so the pending-state assertions cannot race a
	// very fast test scan. Release before waiting for the job to finish.
	cacheFileMu.Lock()
	released := false
	t.Cleanup(func() {
		if !released {
			cacheFileMu.Unlock()
		}
	})
	if !StartRescanLocation("sd") {
		t.Fatal("location rescan not accepted")
	}
	if IsDiscoveryComplete() {
		t.Fatal("pending flag must be set before acceptance returns")
	}
	if StartRescanLocation("sd") || StartFullRescan() {
		t.Fatal("overlapping scan was accepted")
	}
	cacheFileMu.Unlock()
	released = true
	deadline := time.Now().Add(5 * time.Second)
	for {
		cacheMu.RLock()
		finished := !cacheScanning && cacheComplete
		arcade := cachedSystems["arcade"]
		cacheMu.RUnlock()
		if finished {
			if arcade == nil {
				t.Fatal("background SD refresh removed Arcade")
			}
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("background location refresh did not complete")
		}
		time.Sleep(time.Millisecond)
	}
}

func TestBackgroundRescanRejectsMissingInitialCache(t *testing.T) {
	arcadeDiscoveryEnvironment(t)
	if StartRescanLocation("sd") {
		t.Fatal("partial scan accepted without initial discovery")
	}
	if StartRescanLocation("usb0garbage") {
		t.Fatal("invalid location accepted")
	}
}
