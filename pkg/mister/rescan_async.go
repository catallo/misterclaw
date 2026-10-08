package mister

import "log"

// StartRescanLocation reserves discovery synchronously and runs a location
// refresh in the background. False means invalid location, no initial cache,
// or an already running discovery job. Existing readers retain a coherent old
// snapshot until the refreshed snapshot has been published and saved.
func StartRescanLocation(location string) bool {
	if locationToPath(location) == "" || !rescanMu.TryLock() {
		return false
	}
	cacheMu.Lock()
	if cacheScanning || cachedSystems == nil {
		cacheMu.Unlock()
		rescanMu.Unlock()
		return false
	}
	cacheScanning = true
	cacheComplete = false
	cacheMu.Unlock()
	// A Go mutex may be unlocked by another goroutine. Keep the reservation
	// until publishing both the refreshed cache and the completion flags.
	go func() {
		defer rescanMu.Unlock()
		count := rescanLocationLocked(location)
		cacheMu.Lock()
		cacheComplete = true
		cacheScanning = false
		gamesReady = cachedGames != nil
		cacheMu.Unlock()
		log.Printf("discovery: background rescan complete (%s, %d systems)", location, count)
	}()
	return true
}

// StartFullRescan refuses to invalidate a currently active discovery job.
// It reserves the existing rescan mutex before clearing the cache.
func StartFullRescan() bool {
	if !rescanMu.TryLock() {
		return false
	}
	defer rescanMu.Unlock()
	cacheMu.RLock()
	busy := cacheScanning
	cacheMu.RUnlock()
	if busy {
		return false
	}
	invalidateCacheLocked()
	return true
}
