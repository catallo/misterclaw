package mister

import (
	"encoding/xml"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
)

// DiscoveredSystem represents a system found by scanning ROM folders on disk.
type DiscoveredSystem struct {
	Name      string         `json:"name"`
	Folders   []SystemFolder `json:"folders"`
	Config    SystemConfig   `json:"-"`
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
)

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
var (
	cachedSystems map[string]*DiscoveredSystem
	cacheReady    bool // true after Phase 1 (commands work)
	cacheComplete bool // true after Phase 2 (accurate counts)
	cacheScanning bool
	cacheMu       sync.RWMutex
)

// StartDiscovery begins two-phase system discovery. Call once at server startup.
// Phase 1 runs synchronously (fast), Phase 2 runs in background.
func StartDiscovery() {
	cacheMu.Lock()
	if cacheScanning {
		cacheMu.Unlock()
		return
	}
	cacheScanning = true
	cacheReady = false
	cacheComplete = false
	cacheMu.Unlock()

	// Phase 1: fast discovery (folder names + systemDefaults)
	systems := discoverSystemsFast()
	cacheMu.Lock()
	cachedSystems = systems
	cacheReady = true
	cacheMu.Unlock()

	// Phase 2: full discovery in background
	go func() {
		discoverSystemsFull(systems)
		cacheMu.Lock()
		cacheComplete = true
		cacheScanning = false
		cacheMu.Unlock()
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
	return cachedSystems
}

// InvalidateCache clears the discovery cache and triggers re-discovery.
func InvalidateCache() {
	cacheMu.Lock()
	cacheReady = false
	cacheComplete = false
	cachedSystems = nil
	cacheScanning = false
	cacheMu.Unlock()
	StartDiscovery()
}

// RescanLocation rescans a single location (e.g. "sd", "usb0") and merges
// results into the existing cache. Returns the number of systems found at
// that location.
func RescanLocation(location string) int {
	parent := locationToPath(location)
	if parent == "" {
		return 0
	}

	cacheMu.RLock()
	existing := cachedSystems
	cacheMu.RUnlock()
	if existing == nil {
		// No cache yet — do a full discovery instead
		InvalidateCache()
		cacheMu.RLock()
		defer cacheMu.RUnlock()
		count := 0
		for _, ds := range cachedSystems {
			for _, f := range ds.Folders {
				if f.Location == location {
					count++
					break
				}
			}
		}
		return count
	}

	// Remove old folders for this location from cache
	cacheMu.Lock()
	for key, ds := range cachedSystems {
		var kept []SystemFolder
		for _, f := range ds.Folders {
			if f.Location != location {
				kept = append(kept, f)
			}
		}
		if len(kept) == 0 {
			delete(cachedSystems, key)
		} else {
			ds.Folders = kept
			ds.TotalROMs = 0
			for _, f := range kept {
				ds.TotalROMs += f.RomCount
			}
		}
	}
	cacheMu.Unlock()

	// Scan the location
	entries, err := os.ReadDir(parent)
	if err != nil {
		return 0
	}

	cores := scanCores()
	mglMappings := parseMGLFiles()
	systemsFound := 0

	cacheMu.Lock()
	defer cacheMu.Unlock()

	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		dirPath := filepath.Join(parent, e.Name())
		key := strings.ToLower(e.Name())

		count := countTopLevelFiles(dirPath)
		if count == 0 {
			continue
		}

		ds, exists := cachedSystems[key]
		if !exists {
			ds = &DiscoveredSystem{Name: e.Name()}
			if cfg, ok := getDefaultConfig(e.Name()); ok {
				ds.Config = cfg
				ds.HasCore = true
			} else {
				ds.NeedsScan = true
				// Try core/MGL matching
				if corePath, ok := cores[key]; ok {
					ds.Config.Core = corePath
					ds.HasCore = true
					ds.NeedsScan = false
				} else if corePath, ok := mglMappings[e.Name()]; ok {
					ds.Config.Core = corePath
					ds.Config.SetName = e.Name()
					ds.HasCore = true
					ds.NeedsScan = false
				}
			}
			cachedSystems[key] = ds
		}

		ds.Folders = append(ds.Folders, SystemFolder{
			Path:     dirPath,
			Location: location,
			RomCount: count,
		})
		ds.TotalROMs += count
		systemsFound++
	}

	return systemsFound
}

// locationToPath converts a location name to its filesystem path.
func locationToPath(location string) string {
	if location == "sd" {
		return sdGamesPath
	}
	if strings.HasPrefix(location, "usb") {
		numStr := strings.TrimPrefix(location, "usb")
		if n, err := fmt.Sscanf(numStr, "%d", new(int)); err == nil && n == 1 {
			var idx int
			fmt.Sscanf(numStr, "%d", &idx)
			if idx >= 0 && idx <= 7 {
				return fmt.Sprintf(usbPathFormat, idx)
			}
		}
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

// countTopLevelFiles counts non-directory, non-meta entries in a directory (non-recursive).
func countTopLevelFiles(dir string) int {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return 0
	}
	count := 0
	for _, e := range entries {
		if e.IsDir() {
			// Count subdirectories as potential ROM containers (e.g. PSX game folders)
			count++
			continue
		}
		ext := strings.ToLower(filepath.Ext(e.Name()))
		if ext != "" && !metaExtensions[ext] {
			count++
		}
	}
	return count
}

// discoverSystemsFast performs Phase 1 discovery: scan folder names, use systemDefaults
// extensions for known systems, count top-level entries only (approximate counts).
// This is fast (<2s even with many USB drives) because it never recurses into ROM folders.
func discoverSystemsFast() map[string]*DiscoveredSystem {
	systems := make(map[string]*DiscoveredSystem)

	scanLocation := func(parent, location string) {
		entries, err := os.ReadDir(parent)
		if err != nil {
			return
		}
		for _, e := range entries {
			if !e.IsDir() {
				continue
			}
			dirPath := filepath.Join(parent, e.Name())
			key := strings.ToLower(e.Name())

			// For known systems, use systemDefaults extensions; count top-level only
			if cfg, ok := getDefaultConfig(e.Name()); ok {
				count := countTopLevelFiles(dirPath)
				if count == 0 {
					continue
				}
				ds, exists := systems[key]
				if !exists {
					ds = &DiscoveredSystem{Name: e.Name(), Config: cfg, HasCore: true}
					systems[key] = ds
				}
				ds.Folders = append(ds.Folders, SystemFolder{
					Path:     dirPath,
					Location: location,
					RomCount: count,
				})
				ds.TotalROMs += count
				continue
			}

			// Unknown system: just check it has any non-meta files at top level
			count := countTopLevelFiles(dirPath)
			if count == 0 {
				continue
			}
			ds, exists := systems[key]
			if !exists {
				ds = &DiscoveredSystem{Name: e.Name(), NeedsScan: true}
				systems[key] = ds
			}
			ds.Folders = append(ds.Folders, SystemFolder{
				Path:     dirPath,
				Location: location,
				RomCount: count,
			})
			ds.TotalROMs += count
		}
	}

	scanLocation(sdGamesPath, "sd")
	for i := 0; i <= 7; i++ {
		scanLocation(fmt.Sprintf(usbPathFormat, i), fmt.Sprintf("usb%d", i))
	}

	// Match unknown systems to cores and MGL files
	cores := scanCores()
	mglMappings := parseMGLFiles()

	for key, ds := range systems {
		if ds.HasCore {
			continue // Already matched via systemDefaults
		}

		// Try direct core name match
		if corePath, ok := cores[key]; ok {
			ds.Config.Core = corePath
			ds.HasCore = true
		}

		// Try MGL setname match
		if !ds.HasCore {
			if corePath, ok := mglMappings[ds.Name]; ok {
				ds.Config.Core = corePath
				ds.Config.SetName = ds.Name
				ds.HasCore = true
			}
		}
	}

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

// discoverSystems performs full system discovery (used by tests that call it directly).
func discoverSystems() map[string]*DiscoveredSystem {
	systems := discoverSystemsFast()
	discoverSystemsFull(systems)
	return systems
}
