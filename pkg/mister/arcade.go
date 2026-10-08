package mister

import (
	"crypto/sha256"
	"fmt"
	"io"
	"io/fs"
	"log"
	"os"
	"path/filepath"
	"strings"
)

// arcadeMRAPaths is shared by discovery counts and game indexing. Canonical
// MRAs (including _alternatives and arbitrary real subdirectories) take
// precedence over generated _Organized views within each folder root.
func arcadeMRAPaths(dir string) []string {
	var canonical, organized []string
	walkMRAs := func(root string, skipOrganized bool) []string {
		var paths []string
		err := filepath.WalkDir(root, func(path string, entry fs.DirEntry, err error) error {
			if err != nil {
				if !os.IsNotExist(err) {
					log.Printf("discovery: cannot scan arcade path %s: %v", path, err)
				}
				return nil
			}
			if entry.IsDir() {
				if skipOrganized && strings.EqualFold(entry.Name(), "_Organized") {
					organized = append(organized, path)
					return filepath.SkipDir
				}
				return nil
			}
			// Do not follow symlinks or index devices as launchable MRAs.
			if entry.Type().IsRegular() && strings.EqualFold(filepath.Ext(entry.Name()), ".mra") {
				paths = append(paths, path)
			}
			return nil
		})
		if err != nil && !os.IsNotExist(err) {
			log.Printf("discovery: cannot scan arcade folder %s: %v", root, err)
		}
		return paths
	}
	canonical = walkMRAs(dir, true)
	if len(canonical) > 0 {
		return canonical
	}

	// An organized-only installation must remain usable. Deduplicate only
	// byte-identical generated views; different variants with the same title
	// are distinct games. Canonical files are never deduplicated by basename.
	seen := make(map[[sha256.Size]byte]bool)
	var fallback []string
	for _, root := range organized {
		for _, path := range walkMRAs(root, false) {
			file, err := os.Open(path)
			if err != nil {
				log.Printf("discovery: cannot read organized MRA %s: %v", path, err)
				continue
			}
			hash := sha256.New()
			_, readErr := io.Copy(hash, file)
			closeErr := file.Close()
			if readErr != nil || closeErr != nil {
				log.Printf("discovery: cannot hash organized MRA %s: read=%v close=%v", path, readErr, closeErr)
				continue
			}
			var sum [sha256.Size]byte
			copy(sum[:], hash.Sum(nil))
			if !seen[sum] {
				seen[sum] = true
				fallback = append(fallback, path)
			}
		}
	}
	return fallback
}

// scanArcadeFolder returns the same MRA selection used for discovery counts.
// scanDir delegates here for Arcade; no MAME ROM archives are indexed.
func scanArcadeFolder(dir, system, location string) []GameInfo {
	paths := arcadeMRAPaths(dir)
	games := make([]GameInfo, 0, len(paths))
	for _, path := range paths {
		games = append(games, GameInfo{
			Name: strings.TrimSuffix(filepath.Base(path), filepath.Ext(path)),
			Path: path, System: system, Location: location,
		})
	}
	return games
}

// discoverArcadeSystems is the targeted upgrade scan. It never reads game
// files belonging to ordinary systems, preserving their cached listings.
func discoverArcadeSystems() map[string]*DiscoveredSystem {
	systems := make(map[string]*DiscoveredSystem)
	scanLocation := func(parent, location string) {
		entries, err := os.ReadDir(parent)
		if err != nil {
			if !os.IsNotExist(err) {
				log.Printf("discovery: cannot migrate arcade location %s: %v", location, err)
			}
			return
		}
		for _, entry := range entries {
			if entry.IsDir() && strings.EqualFold(entry.Name(), "Arcade") {
				addArcadeFolder(systems, filepath.Join(parent, entry.Name()), location)
			}
		}
	}
	scanLocation(sdGamesPath, "sd")
	for i := 0; i <= 7; i++ {
		scanLocation(fmt.Sprintf(usbPathFormat, i), fmt.Sprintf("usb%d", i))
	}
	addArcadeFolder(systems, arcadePath, "sd")
	return systems
}

// addArcadeFolder merges SD games/Arcade, USB Arcade and the SD _Arcade root.
func addArcadeFolder(systems map[string]*DiscoveredSystem, dir, location string) {
	count := len(arcadeMRAPaths(dir))
	if count == 0 {
		return
	}
	ds := systems["arcade"]
	if ds == nil {
		ds = &DiscoveredSystem{Name: "Arcade", HasCore: true, Config: SystemConfig{Extensions: []string{".mra"}}}
		systems["arcade"] = ds
	}
	for _, folder := range ds.Folders {
		if folder.Path == dir && folder.Location == location {
			return
		}
	}
	ds.Folders = append(ds.Folders, SystemFolder{Path: dir, Location: location, RomCount: count})
	ds.TotalROMs += count
}
