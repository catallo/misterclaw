package mister

import (
	"testing"
	"time"
)

// Tests override global filesystem paths. Wait for async discovery, including
// persistence, before restoring a fixture or letting another test reuse them.
func waitForDiscoveryJobs(t *testing.T) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for {
		cacheMu.RLock()
		busy := cacheScanning
		cacheMu.RUnlock()
		if !busy {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("discovery job did not finish before fixture cleanup")
		}
		time.Sleep(time.Millisecond)
	}
	rescanMu.Lock()
	rescanMu.Unlock()
}

func resetDiscoveryTestState(t *testing.T) {
	t.Helper()
	waitForDiscoveryJobs(t)
	cacheMu.Lock()
	cachedSystems, cachedGames = nil, nil
	cacheReady, cacheComplete, gamesReady, cacheScanning = false, false, false, false
	cacheMu.Unlock()
}
