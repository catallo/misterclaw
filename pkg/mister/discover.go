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

// Discovery cache.
var (
	cachedSystems map[string]*DiscoveredSystem
	cacheInit     bool
	cacheMu       sync.Mutex
)

// getDiscoveredSystems returns the cached discovery results, running discovery on first call.
func getDiscoveredSystems() map[string]*DiscoveredSystem {
	cacheMu.Lock()
	defer cacheMu.Unlock()
	if !cacheInit {
		cachedSystems = discoverSystems()
		cacheInit = true
	}
	return cachedSystems
}

// InvalidateCache clears the discovery cache, forcing re-discovery on next access.
func InvalidateCache() {
	cacheMu.Lock()
	cacheInit = false
	cachedSystems = nil
	cacheMu.Unlock()
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

// scanFolderExtensions scans a directory for file extensions, excluding meta files.
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

// discoverSystems performs full system discovery by scanning ROM folders, cores, and MGL files.
func discoverSystems() map[string]*DiscoveredSystem {
	systems := make(map[string]*DiscoveredSystem)

	// 1. Scan ROM folders
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
			exts, count := scanFolderExtensions(dirPath)
			if count == 0 {
				continue
			}

			key := strings.ToLower(e.Name())
			ds, exists := systems[key]
			if !exists {
				ds = &DiscoveredSystem{Name: e.Name()}
				systems[key] = ds
			}
			ds.Folders = append(ds.Folders, SystemFolder{
				Path:     dirPath,
				Location: location,
				RomCount: count,
			})
			ds.TotalROMs += count

			// Merge extensions from this folder
			extSet := make(map[string]bool)
			for _, ext := range ds.Config.Extensions {
				extSet[ext] = true
			}
			for _, ext := range exts {
				if !extSet[ext] {
					ds.Config.Extensions = append(ds.Config.Extensions, ext)
				}
			}
		}
	}

	scanLocation(sdGamesPath, "sd")
	for i := 0; i <= 7; i++ {
		scanLocation(fmt.Sprintf(usbPathFormat, i), fmt.Sprintf("usb%d", i))
	}

	// 2. Scan cores and MGL files
	cores := scanCores()
	mglMappings := parseMGLFiles()

	// 3. Match folders to cores and set MGL parameters
	for key, ds := range systems {
		// Check systemDefaults first (most reliable source of MGL params)
		if cfg, ok := getDefaultConfig(ds.Name); ok {
			diskExts := ds.Config.Extensions
			ds.Config = cfg
			ds.Config.Extensions = diskExts
			ds.HasCore = true
			continue
		}

		// Try direct core name match (case-insensitive)
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

		// Set default MGL params for systems not in systemDefaults
		if ds.Config.Type == "" {
			isCDBased := false
			for _, ext := range ds.Config.Extensions {
				if cdExtensions[ext] {
					isCDBased = true
					break
				}
			}
			if isCDBased {
				ds.Config.Type = "s"
				ds.Config.Index = 0
				ds.Config.Delay = 1
			} else {
				ds.Config.Type = "f"
				ds.Config.Index = 1
				ds.Config.Delay = 2
			}
		}
	}

	return systems
}
