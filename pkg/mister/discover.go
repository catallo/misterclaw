package mister

import (
	"encoding/json"
	"encoding/xml"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"
)

// DiscoveredSystem represents a system found by scanning ROM folders on disk.
type DiscoveredSystem struct {
	Name      string         `json:"name"`
	Folders   []SystemFolder `json:"folders"`
	Config    SystemConfig   `json:"config"`
	TotalROMs int            `json:"total_roms"`
	HasCore   bool           `json:"has_core"`
	NeedsScan bool           `json:"-"` // true if Phase 2 full scan is still needed
}

// SystemFolder represents a single ROM folder location.
type SystemFolder struct {
	Path     string `json:"path"`
	Location string `json:"location"`
	RomCount int    `json:"rom_count"`
}

// Configurable base paths (overridden in tests).
var (
	sdGamesPath       = "/media/fat/games"
	usbPathFormat     = "/media/usb%d"
	consoleCoresPath  = "/media/fat/_Console"
	computerCoresPath = "/media/fat/_Computer"
	arcadePath        = "/media/fat/_Arcade"
)

// CacheFilePath is the path to the persistent discovery cache file.
// Override in tests to use a temp directory.
var CacheFilePath = "/media/fat/config/misterclaw_cache.json"

// diskCache is the JSON structure written to/read from disk.
type diskCache struct {
	Version   int                          `json:"version"`
	Timestamp string                       `json:"timestamp"`
	Systems   map[string]*DiscoveredSystem `json:"systems"`
	Games     map[string][]GameInfo        `json:"games"` // nil when incomplete; key = lowercase system name
}

// Meta extensions excluded from ROM scanning.
var metaExtensions = map[string]bool{
	".txt": true, ".xml": true, ".jpg": true, ".jpeg": true,
	".png": true, ".gif": true, ".bmp": true, ".nfo": true,
	".dat": true, ".htm": true, ".html": true, ".pdf": true,
	".db": true, ".ini": true, ".cfg": true, ".log": true,
	".srm": true, ".sav": true, ".sta": true, ".ss": true,
}

// CD-based extensions that suggest type "s" for MGL launch.
var cdExtensions = map[string]bool{
	".chd": true, ".cue": true, ".iso": true,
}

// Discovery cache with two-phase background scanning.
// Phase 1 (fast): scan folder names, use systemDefaults extensions, top-level file counts.
// Phase 2 (full): scanFolderExtensions for unknown systems, accurate ROM counts.
// Phase 3 (games): collect all ROM file listings for instant search.
var (
	cachedSystems map[string]*DiscoveredSystem
	cachedGames   map[string][]GameInfo // key = lowercase system name
	cacheReady    bool                  // true after Phase 1 (commands work)
	cacheComplete bool                  // true after Phase 2 (accurate counts)
	gamesReady    bool                  // true after Phase 3 (game listings cached)
	cacheScanning bool
	cacheMu       sync.RWMutex
)

const discoveryCacheVersion = 3

// Serialize rescans and cache-file writes without holding cacheMu during I/O.
var rescanMu sync.Mutex
var cacheFileMu sync.Mutex

// cloneSystems returns an independent snapshot. Call under cacheMu when the
// input is the shared cache; callers may safely iterate the returned map.
func cloneSystems(systems map[string]*DiscoveredSystem) map[string]*DiscoveredSystem {
	if systems == nil {
		return nil
	}
	result := make(map[string]*DiscoveredSystem, len(systems))
	for key, ds := range systems {
		copyDS := *ds
		copyDS.Folders = append([]SystemFolder(nil), ds.Folders...)
		copyDS.Config.Extensions = append([]string(nil), ds.Config.Extensions...)
		if ds.Config.PostLaunch != nil {
			postLaunch := *ds.Config.PostLaunch
			copyDS.Config.PostLaunch = &postLaunch
		}
		copyDS.Config.FormatOverrides = append([]FormatOverride(nil), ds.Config.FormatOverrides...)
		for i, override := range copyDS.Config.FormatOverrides {
			copyDS.Config.FormatOverrides[i].Extensions = append([]string(nil), override.Extensions...)
			copyDS.Config.FormatOverrides[i].PostLaunchCombo = append([]string(nil), override.PostLaunchCombo...)
			copyDS.Config.FormatOverrides[i].PostLaunchKeys = append([]string(nil), override.PostLaunchKeys...)
		}
		result[key] = &copyDS
	}
	return result
}

func cloneGames(games map[string][]GameInfo) map[string][]GameInfo {
	if games == nil {
		return nil
	}
	result := make(map[string][]GameInfo, len(games))
	for key, entries := range games {
		result[key] = append([]GameInfo(nil), entries...)
	}
	return result
}

// LoadCache migrates v1/v2 Arcade entries without rereading unrelated ROM
// systems. Unknown schemas are invalidated. A v1 cache still needs the usual
// background game-list collection because it never contained game listings.
func LoadCache() bool {
	cacheFileMu.Lock()
	data, err := os.ReadFile(CacheFilePath)
	cacheFileMu.Unlock()
	if err != nil {
		return false
	}
	var dc diskCache
	if err := json.Unmarshal(data, &dc); err != nil {
		log.Printf("discovery: invalid cache file, will rescan: %v", err)
		return false
	}
	if dc.Version < 1 || dc.Version > discoveryCacheVersion {
		log.Printf("discovery: unsupported cache schema v%d (current v%d); rebuilding discovery", dc.Version, discoveryCacheVersion)
		return false
	}
	if dc.Systems == nil {
		log.Printf("discovery: cache has no systems; will rescan")
		return false
	}
	migrated := dc.Version != discoveryCacheVersion
	if migrated {
		log.Printf("discovery: migrating cache v%d to v%d: rebuilding only Arcade and removing MAME ROM entries", dc.Version, discoveryCacheVersion)
		delete(dc.Systems, "arcade")
		delete(dc.Systems, "mame")
		arcadeSystems := discoverArcadeSystems()
		if ds := arcadeSystems["arcade"]; ds != nil {
			dc.Systems["arcade"] = ds
		}
		if dc.Version == 1 {
			dc.Games = nil
		} else if dc.Games != nil {
			delete(dc.Games, "arcade")
			delete(dc.Games, "mame")
			if games := collectAllGames(arcadeSystems)["arcade"]; len(games) > 0 {
				dc.Games["arcade"] = games
			}
		}
	}
	cacheMu.Lock()
	cachedSystems = dc.Systems
	cachedGames = dc.Games
	cacheReady = true
	cacheComplete = true
	gamesReady = dc.Games != nil
	cacheMu.Unlock()
	log.Printf("discovery: loaded %d systems from cache (%s)", len(dc.Systems), dc.Timestamp)
	if migrated {
		if err := SaveCache(); err != nil {
			log.Printf("discovery: failed to persist migrated cache: %v", err)
		}
	}
	return true
}

// SaveCache snapshots under the read lock and atomically replaces the disk
// cache, so readers never encounter partially written JSON.
func SaveCache() error {
	cacheFileMu.Lock()
	defer cacheFileMu.Unlock()
	cacheMu.RLock()
	dc := diskCache{
		Version:   discoveryCacheVersion,
		Timestamp: time.Now().UTC().Format(time.RFC3339),
		Systems:   cloneSystems(cachedSystems),
	}
	if gamesReady {
		dc.Games = cloneGames(cachedGames)
	}
	cacheMu.RUnlock()
	if dc.Systems == nil {
		return nil
	}
	data, err := json.Marshal(dc)
	if err != nil {
		return err
	}
	dir := filepath.Dir(CacheFilePath)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return err
	}
	file, err := os.CreateTemp(dir, ".misterclaw-cache-*")
	if err != nil {
		return err
	}
	defer os.Remove(file.Name())
	if err := file.Chmod(0644); err != nil {
		file.Close()
		return err
	}
	if _, err := file.Write(data); err != nil {
		file.Close()
		return err
	}
	if err := file.Sync(); err != nil {
		file.Close()
		return err
	}
	if err := file.Close(); err != nil {
		return err
	}
	if err := os.Rename(file.Name(), CacheFilePath); err != nil {
		return err
	}
	log.Printf("discovery: saved %d systems to v%d cache", len(dc.Systems), discoveryCacheVersion)
	return nil
}

// DeleteCacheFile removes the persistent cache file from disk.
func DeleteCacheFile() {
	cacheFileMu.Lock()
	defer cacheFileMu.Unlock()
	if err := os.Remove(CacheFilePath); err != nil && !os.IsNotExist(err) {
		log.Printf("discovery: failed to delete cache: %v", err)
	}
}

// StartDiscovery begins system discovery. Call once at server startup.
// If a valid cache exists on disk, it is loaded and no scanning occurs.
// Otherwise, Phase 1 runs synchronously (fast), Phase 2 runs in background
// and saves the cache to disk when complete.
func StartDiscovery() {
	cacheMu.Lock()
	if cacheScanning {
		cacheMu.Unlock()
		return
	}
	cacheScanning = true
	cacheReady = false
	cacheComplete = false
	gamesReady = false
	cacheMu.Unlock()

	// Try loading from disk cache first
	if LoadCache() {
		cacheMu.RLock()
		hasGames := gamesReady
		cacheMu.RUnlock()
		if hasGames {
			cacheMu.Lock()
			cacheScanning = false
			cacheMu.Unlock()
			return
		}
		// Current-schema cache without game listings: collect in background.
		go func() {
			rescanMu.Lock()
			defer rescanMu.Unlock()
			cacheMu.RLock()
			systems := cloneSystems(cachedSystems)
			cacheMu.RUnlock()
			games := collectAllGames(systems)
			cacheMu.Lock()
			cachedGames = games
			gamesReady = true
			cacheScanning = false
			cacheMu.Unlock()
			if err := SaveCache(); err != nil {
				log.Printf("discovery: failed to save cache: %v", err)
			}
			log.Printf("discovery: collected game listings for %d systems", len(games))
		}()
		return
	}

	// All phases run in background so server can start accepting connections immediately
	go func() {
		rescanMu.Lock()
		defer rescanMu.Unlock()
		// Phase 1: fast discovery (folder names + systemDefaults)
		systems := discoverSystemsFast()
		cacheMu.Lock()
		cachedSystems = systems
		cacheReady = true
		cacheMu.Unlock()
		log.Printf("discovery: phase 1 complete (%d systems)", len(systems))

		// Phase 2: full discovery
		discoverSystemsFull(systems)
		cacheMu.Lock()
		cacheComplete = true
		cacheMu.Unlock()

		// Phase 3: collect all game file listings
		games := collectAllGames(systems)
		cacheMu.Lock()
		cachedGames = games
		gamesReady = true
		cacheScanning = false
		cacheMu.Unlock()
		if err := SaveCache(); err != nil {
			log.Printf("discovery: failed to save cache: %v", err)
		}
		log.Printf("discovery: collected game listings for %d systems", len(games))
	}()
}

// IsDiscoveryReady returns true after Phase 1 (fast scan). Commands can work.
func IsDiscoveryReady() bool {
	cacheMu.RLock()
	defer cacheMu.RUnlock()
	return cacheReady
}

// IsDiscoveryComplete returns true after Phase 2 (full scan with accurate counts).
func IsDiscoveryComplete() bool {
	cacheMu.RLock()
	defer cacheMu.RUnlock()
	return cacheComplete
}

// getDiscoveredSystems returns the cached discovery results.
// Returns nil if discovery hasn't completed Phase 1 yet.
func getDiscoveredSystems() map[string]*DiscoveredSystem {
	cacheMu.RLock()
	defer cacheMu.RUnlock()
	return cloneSystems(cachedSystems)
}

// InvalidateCache clears the discovery cache, deletes the cache file, and triggers re-discovery.
func InvalidateCache() {
	rescanMu.Lock()
	defer rescanMu.Unlock()
	invalidateCacheLocked()
}

// invalidateCacheLocked is called with rescanMu held, preventing an older
// location scan from publishing into a cache that has just been cleared.
func invalidateCacheLocked() {
	DeleteCacheFile()
	cacheMu.Lock()
	cacheReady = false
	cacheComplete = false
	gamesReady = false
	cachedSystems = nil
	cachedGames = nil
	cacheScanning = false
	cacheMu.Unlock()
	StartDiscovery()
}

// RescanLocation rescans a single location (e.g. "sd", "usb0") and merges
// results into the existing cache. Returns the number of systems found at
// that location.
func RescanLocation(location string) int {
	if locationToPath(location) == "" {
		return 0
	}
	rescanMu.Lock()
	defer rescanMu.Unlock()
	return rescanLocationLocked(location)
}

// rescanLocationLocked runs with rescanMu held by either the synchronous
// wrapper or its asynchronous job owner.
func rescanLocationLocked(location string) int {
	parent := locationToPath(location)
	cacheMu.RLock()
	hasCache := cachedSystems != nil
	cacheMu.RUnlock()
	if !hasCache {
		// Startup owns the full initial scan; do not invent a partial cache.
		StartDiscovery()
		return 0
	}

	// Scan only the requested location, including the separate SD _Arcade
	// root. Never rebuild unrelated USB systems or reread their game files.
	scanned := make(map[string]*DiscoveredSystem)
	scanDiscoveryLocation(scanned, parent, location)
	if location == "sd" {
		addArcadeFolder(scanned, arcadePath, "sd")
	}
	matchDiscoveredCores(scanned)
	discoverSystemsFull(scanned)
	newGames := collectAllGames(scanned)

	cacheMu.Lock()
	mergedSystems := cloneSystems(cachedSystems)
	mergedGames := cloneGames(cachedGames)
	if mergedGames == nil {
		mergedGames = make(map[string][]GameInfo)
	}
	affected := make(map[string]bool)
	for key, ds := range mergedSystems {
		var kept []SystemFolder
		for _, folder := range ds.Folders {
			if folder.Location == location {
				affected[key] = true
			} else {
				kept = append(kept, folder)
			}
		}
		if !affected[key] {
			continue
		}
		if len(kept) == 0 {
			delete(mergedSystems, key)
		} else {
			ds.Folders = kept
			ds.TotalROMs = 0
			for _, folder := range kept {
				ds.TotalROMs += folder.RomCount
			}
		}
	}
	for key, ds := range scanned {
		affected[key] = true
		if existing := mergedSystems[key]; existing != nil {
			existing.Folders = append(existing.Folders, ds.Folders...)
			existing.TotalROMs += ds.TotalROMs
			existing.Config = mergeRefreshedConfig(existing.Name, existing.Config, ds.Config)
			existing.HasCore = existing.HasCore || ds.HasCore
		} else {
			mergedSystems[key] = ds
		}
	}
	for key := range affected {
		var kept []GameInfo
		for _, game := range mergedGames[key] {
			if game.Location != location {
				kept = append(kept, game)
			}
		}
		kept = append(kept, newGames[key]...)
		if len(kept) == 0 {
			delete(mergedGames, key)
		} else {
			mergedGames[key] = kept
		}
	}
	cachedSystems = mergedSystems
	cachedGames = mergedGames
	cacheMu.Unlock()

	if err := SaveCache(); err != nil {
		log.Printf("discovery: failed to save cache after rescan: %v", err)
	}
	return len(scanned)
}

// locationToPath converts a location name to its filesystem path.
func locationToPath(location string) string {
	if location == "sd" {
		return sdGamesPath
	}
	if len(location) == 4 && strings.HasPrefix(location, "usb") && location[3] >= '0' && location[3] <= '7' {
		return fmt.Sprintf(usbPathFormat, int(location[3]-'0'))
	}
	return ""
}

// extractCoreName strips the date suffix and .rbf extension from a core filename.
// "SNES_20250605.rbf" -> "SNES", "MegaDrive_20250707.rbf" -> "MegaDrive"
func extractCoreName(filename string) string {
	name := strings.TrimSuffix(filename, ".rbf")
	if idx := strings.LastIndex(name, "_"); idx > 0 {
		suffix := name[idx+1:]
		if len(suffix) == 8 {
			allDigits := true
			for _, c := range suffix {
				if c < '0' || c > '9' {
					allDigits = false
					break
				}
			}
			if allDigits {
				return name[:idx]
			}
		}
	}
	return name
}

// scanCores returns a map of lowercase core name -> "category/CoreName" for all installed cores.
func scanCores() map[string]string {
	cores := make(map[string]string)
	for _, dir := range []string{consoleCoresPath, computerCoresPath} {
		entries, err := os.ReadDir(dir)
		if err != nil {
			continue
		}
		category := filepath.Base(dir)
		for _, e := range entries {
			if e.IsDir() || !strings.HasSuffix(strings.ToLower(e.Name()), ".rbf") {
				continue
			}
			coreName := extractCoreName(e.Name())
			cores[strings.ToLower(coreName)] = category + "/" + coreName
		}
	}
	return cores
}

// mglDescription is used to parse .mgl files from disk.
type mglDescription struct {
	XMLName xml.Name `xml:"mistergamedescription"`
	Rbf     string   `xml:"rbf"`
	SetName string   `xml:"setname"`
}

// parseMGLFiles returns setname -> core path mappings from .mgl files in core directories.
func parseMGLFiles() map[string]string {
	result := make(map[string]string)
	for _, dir := range []string{consoleCoresPath, computerCoresPath} {
		entries, err := os.ReadDir(dir)
		if err != nil {
			continue
		}
		for _, e := range entries {
			if e.IsDir() || !strings.HasSuffix(strings.ToLower(e.Name()), ".mgl") {
				continue
			}
			data, err := os.ReadFile(filepath.Join(dir, e.Name()))
			if err != nil {
				continue
			}
			var desc mglDescription
			if err := xml.Unmarshal(data, &desc); err != nil {
				continue
			}
			if desc.SetName != "" && desc.Rbf != "" {
				result[desc.SetName] = desc.Rbf
			}
		}
	}
	return result
}

// scanFolderExtensions scans a directory recursively for file extensions, excluding meta files.
// Runs in background at startup so performance is not an issue.
// Returns extensions sorted by frequency (most common first) and total file count.
func scanFolderExtensions(dir string) (extensions []string, count int) {
	extCounts := make(map[string]int)
	filepath.Walk(dir, func(path string, info os.FileInfo, err error) error {
		if err != nil || info.IsDir() {
			return nil
		}
		ext := strings.ToLower(filepath.Ext(path))
		if ext == "" || metaExtensions[ext] {
			return nil
		}
		extCounts[ext]++
		count++
		return nil
	})
	for ext := range extCounts {
		extensions = append(extensions, ext)
	}
	sort.Slice(extensions, func(i, j int) bool {
		return extCounts[extensions[i]] > extCounts[extensions[j]]
	})
	return
}

// getDefaultConfig checks systemDefaults for a system name (case-insensitive).
func getDefaultConfig(system string) (SystemConfig, bool) {
	for name, cfg := range systemDefaults {
		if strings.EqualFold(name, system) {
			return cfg, true
		}
	}
	return SystemConfig{}, false
}

// countFiles counts non-meta file entries in a directory (max 2 levels deep).
func countFiles(dir string) int {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return 0
	}
	count := 0
	for _, e := range entries {
		if e.IsDir() {
			// Count files in immediate subdirectories (e.g. GamesHDF/, Genre folders)
			subEntries, err := os.ReadDir(filepath.Join(dir, e.Name()))
			if err != nil {
				continue
			}
			for _, se := range subEntries {
				if se.IsDir() {
					count++ // PSX-style game folders
					continue
				}
				ext := strings.ToLower(filepath.Ext(se.Name()))
				if ext != "" && !metaExtensions[ext] {
					count++
				}
			}
			continue
		}
		ext := strings.ToLower(filepath.Ext(e.Name()))
		if ext != "" && !metaExtensions[ext] {
			count++
		}
	}
	return count
}

// scanDiscoveryLocation shares startup/rescan folder policies. Normal ROM
// systems retain shallow counts; only Arcade requires recursive MRA selection.
func scanDiscoveryLocation(systems map[string]*DiscoveredSystem, parent, location string) {
	entries, err := os.ReadDir(parent)
	if err != nil {
		if !os.IsNotExist(err) {
			log.Printf("discovery: cannot scan location %s: %v", location, err)
		}
		return
	}
	for _, entry := range entries {
		if !entry.IsDir() || strings.EqualFold(entry.Name(), "mame") {
			continue
		}
		dirPath := filepath.Join(parent, entry.Name())
		key := strings.ToLower(entry.Name())
		if key == "arcade" {
			addArcadeFolder(systems, dirPath, location)
			continue
		}
		count := countFiles(dirPath)
		if count == 0 {
			continue
		}
		ds := systems[key]
		if ds == nil {
			ds = &DiscoveredSystem{Name: entry.Name(), NeedsScan: true}
			if cfg, ok := getDefaultConfig(entry.Name()); ok {
				ds.Config = cfg
				ds.HasCore = true
				ds.NeedsScan = false
			}
			systems[key] = ds
		}
		ds.Folders = append(ds.Folders, SystemFolder{Path: dirPath, Location: location, RomCount: count})
		ds.TotalROMs += count
	}
}

func matchDiscoveredCores(systems map[string]*DiscoveredSystem) {
	cores := scanCores()
	mglMappings := parseMGLFiles()
	for key, ds := range systems {
		if ds.HasCore {
			continue
		}
		if corePath, ok := cores[key]; ok {
			ds.Config.Core = corePath
			ds.HasCore = true
		} else if corePath, ok := mglMappings[ds.Name]; ok {
			ds.Config.Core = corePath
			ds.Config.SetName = ds.Name
			ds.HasCore = true
		}
	}
}

// discoverSystemsFast uses shallow counts for ordinary ROM systems. Arcade
// uses its full recursive policy, skipping generated views whenever possible.
func discoverSystemsFast() map[string]*DiscoveredSystem {
	systems := make(map[string]*DiscoveredSystem)
	scanDiscoveryLocation(systems, sdGamesPath, "sd")
	for i := 0; i <= 7; i++ {
		scanDiscoveryLocation(systems, fmt.Sprintf(usbPathFormat, i), fmt.Sprintf("usb%d", i))
	}
	addArcadeFolder(systems, arcadePath, "sd")
	matchDiscoveredCores(systems)
	return systems
}

// discoverSystemsFull performs Phase 2 discovery: full recursive scan for unknown systems,
// accurate ROM counts for all systems. Mutates the systems map in place (under lock).
func discoverSystemsFull(systems map[string]*DiscoveredSystem) {
	type folderResult struct {
		key      string
		folder   int
		romCount int
	}
	type systemResult struct {
		key       string
		totalROMs int
		exts      []string
		mglType   string
		mglIndex  int
		mglDelay  int
	}

	// Snapshot what needs scanning (read under lock)
	type scanWork struct {
		key     string
		folders []SystemFolder
	}
	var work []scanWork
	cacheMu.RLock()
	for key, ds := range systems {
		if !ds.NeedsScan {
			continue
		}
		// Copy folder list so we don't hold the lock during I/O
		folders := make([]SystemFolder, len(ds.Folders))
		copy(folders, ds.Folders)
		work = append(work, scanWork{key: key, folders: folders})
	}
	cacheMu.RUnlock()

	// Do all I/O without any lock held
	var folderResults []folderResult
	var systemResults []systemResult

	for _, w := range work {
		allExts := make(map[string]bool)
		totalCount := 0

		for i, folder := range w.folders {
			exts, count := scanFolderExtensions(folder.Path)
			totalCount += count
			for _, ext := range exts {
				allExts[ext] = true
			}
			folderResults = append(folderResults, folderResult{
				key:      w.key,
				folder:   i,
				romCount: count,
			})
		}

		isCDBased := false
		var extSlice []string
		for ext := range allExts {
			extSlice = append(extSlice, ext)
			if cdExtensions[ext] {
				isCDBased = true
			}
		}

		sr := systemResult{
			key:       w.key,
			totalROMs: totalCount,
			exts:      extSlice,
		}
		if isCDBased {
			sr.mglType = "s"
			sr.mglIndex = 0
			sr.mglDelay = 1
		} else {
			sr.mglType = "f"
			sr.mglIndex = 1
			sr.mglDelay = 2
		}
		systemResults = append(systemResults, sr)
	}

	// Apply results under write lock
	cacheMu.Lock()
	for _, fr := range folderResults {
		if ds, ok := systems[fr.key]; ok && fr.folder < len(ds.Folders) {
			ds.Folders[fr.folder].RomCount = fr.romCount
		}
	}
	for _, sr := range systemResults {
		if ds, ok := systems[sr.key]; ok {
			ds.TotalROMs = sr.totalROMs
			ds.Config.Extensions = sr.exts
			if ds.Config.Type == "" {
				ds.Config.Type = sr.mglType
				ds.Config.Index = sr.mglIndex
				ds.Config.Delay = sr.mglDelay
			}
			ds.NeedsScan = false
		}
	}
	cacheMu.Unlock()
}

// IsGamesReady returns true after game listings have been collected and cached.
func IsGamesReady() bool {
	cacheMu.RLock()
	defer cacheMu.RUnlock()
	return gamesReady
}

// getCachedGames returns the cached game listings.
func getCachedGames() map[string][]GameInfo {
	cacheMu.RLock()
	defer cacheMu.RUnlock()
	return cloneGames(cachedGames)
}

// collectAllGames scans all discovered systems and collects GameInfo entries.
func collectAllGames(systems map[string]*DiscoveredSystem) map[string][]GameInfo {
	games := make(map[string][]GameInfo)
	cacheMu.RLock()
	// Snapshot systems to avoid holding lock during I/O
	type sysInfo struct {
		key     string
		name    string
		folders []SystemFolder
		exts    []string
	}
	var work []sysInfo
	for key, ds := range systems {
		folders := make([]SystemFolder, len(ds.Folders))
		copy(folders, ds.Folders)
		exts := make([]string, len(ds.Config.Extensions))
		copy(exts, ds.Config.Extensions)
		work = append(work, sysInfo{key: key, name: ds.Name, folders: folders, exts: exts})
	}
	cacheMu.RUnlock()

	for _, w := range work {
		if len(w.exts) == 0 {
			continue
		}
		extSet := make(map[string]bool, len(w.exts))
		for _, ext := range w.exts {
			extSet[ext] = true
		}
		var sysGames []GameInfo
		for _, folder := range w.folders {
			if strings.EqualFold(w.name, "Arcade") {
				sysGames = append(sysGames, scanArcadeFolder(folder.Path, w.name, folder.Location)...)
			} else {
				sysGames = append(sysGames, scanDir(folder.Path, w.name, folder.Location, extSet)...)
			}
		}
		if len(sysGames) > 0 {
			games[w.key] = sysGames
		}
	}
	return games
}

// discoverSystems performs full system discovery (used by tests that call it directly).
func discoverSystems() map[string]*DiscoveredSystem {
	systems := discoverSystemsFast()
	discoverSystemsFull(systems)
	return systems
}
