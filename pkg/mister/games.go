package mister

import (
	"encoding/xml"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// SystemConfig defines how to launch games for a given system.
type SystemConfig struct {
	Core       string
	Delay      int
	Type       string // "f" or "s"
	Index      int
	Extensions []string
	SetName    string // optional, for systems sharing a core (GBC, GameGear, etc.)
}

// GameInfo represents a single ROM file.
type GameInfo struct {
	Name     string `json:"name"`     // filename without extension
	Path     string `json:"path"`     // full absolute path
	System   string `json:"system"`   // system key from systemMap
	Location string `json:"location"` // "sd" or "usb0" etc.
}

// SystemStats summarizes ROM counts per system.
type SystemStats struct {
	System   string `json:"system"`
	RomCount int    `json:"rom_count"`
	Location string `json:"location"`
}

var systemMap = map[string]SystemConfig{
	// === Consoles ===
	"Gameboy":         {Core: "_Console/Gameboy", Delay: 2, Type: "f", Index: 1, Extensions: []string{".gb"}},
	"GBC":             {Core: "_Console/Gameboy", Delay: 2, Type: "f", Index: 1, Extensions: []string{".gbc"}, SetName: "GBC"},
	"GBA":             {Core: "_Console/GBA", Delay: 2, Type: "f", Index: 1, Extensions: []string{".gba"}},
	"NES":             {Core: "_Console/NES", Delay: 2, Type: "f", Index: 1, Extensions: []string{".nes"}},
	"SNES":            {Core: "_Console/SNES", Delay: 2, Type: "f", Index: 0, Extensions: []string{".sfc", ".smc"}},
	"Genesis":         {Core: "_Console/MegaDrive", Delay: 1, Type: "f", Index: 1, Extensions: []string{".md", ".gen", ".bin"}},
	"MegaDrive":       {Core: "_Console/MegaDrive", Delay: 1, Type: "f", Index: 1, Extensions: []string{".md", ".gen", ".bin"}},
	"SMS":             {Core: "_Console/SMS", Delay: 3, Type: "f", Index: 1, Extensions: []string{".sms"}},
	"MasterSystem":    {Core: "_Console/SMS", Delay: 3, Type: "f", Index: 1, Extensions: []string{".sms"}},
	"GameGear":        {Core: "_Console/SMS", Delay: 3, Type: "f", Index: 1, Extensions: []string{".gg"}, SetName: "GameGear"},
	"TurboGrafx16":    {Core: "_Console/TurboGrafx16", Delay: 1, Type: "f", Index: 0, Extensions: []string{".pce"}},
	"PCEngine":        {Core: "_Console/TurboGrafx16", Delay: 1, Type: "f", Index: 0, Extensions: []string{".pce", ".sgx"}},
	"TGFX16CD":        {Core: "_Console/TurboGrafx16", Delay: 1, Type: "s", Index: 0, Extensions: []string{".chd", ".cue", ".bin"}},
	"SuperGrafx":      {Core: "_Console/TurboGrafx16", Delay: 1, Type: "f", Index: 0, Extensions: []string{".sgx"}},
	"PSX":             {Core: "_Console/PSX", Delay: 1, Type: "s", Index: 1, Extensions: []string{".chd", ".cue", ".bin"}},
	"N64":             {Core: "_Console/N64", Delay: 1, Type: "f", Index: 1, Extensions: []string{".z64", ".n64", ".v64"}},
	"S32X":            {Core: "_Console/S32X", Delay: 2, Type: "f", Index: 1, Extensions: []string{".32x"}},
	"WonderSwan":      {Core: "_Console/WonderSwan", Delay: 2, Type: "f", Index: 1, Extensions: []string{".ws"}},
	"WonderSwanColor": {Core: "_Console/WonderSwan", Delay: 2, Type: "f", Index: 1, Extensions: []string{".wsc"}, SetName: "WonderSwanColor"},
	"Atari2600":       {Core: "_Console/Atari2600", Delay: 1, Type: "f", Index: 1, Extensions: []string{".a26", ".bin"}},
	"Atari5200":       {Core: "_Console/Atari5200", Delay: 1, Type: "f", Index: 1, Extensions: []string{".a52", ".bin", ".car"}},
	"Atari7800":       {Core: "_Console/Atari7800", Delay: 1, Type: "f", Index: 1, Extensions: []string{".a78", ".bin"}},
	"AtariLynx":       {Core: "_Console/AtariLynx", Delay: 1, Type: "f", Index: 1, Extensions: []string{".lnx"}},
	"ColecoVision":    {Core: "_Console/ColecoVision", Delay: 1, Type: "f", Index: 1, Extensions: []string{".col", ".bin", ".rom"}},
	"Intellivision":   {Core: "_Console/Intellivision", Delay: 1, Type: "f", Index: 1, Extensions: []string{".int", ".bin", ".rom"}},
	"Odyssey2":        {Core: "_Console/Odyssey2", Delay: 1, Type: "f", Index: 1, Extensions: []string{".bin"}},
	"Vectrex":         {Core: "_Console/Vectrex", Delay: 1, Type: "f", Index: 1, Extensions: []string{".vec", ".bin"}},
	"ChannelF":        {Core: "_Console/ChannelF", Delay: 1, Type: "f", Index: 1, Extensions: []string{".bin", ".rom"}},
	"MegaCD":          {Core: "_Console/MegaCD", Delay: 1, Type: "s", Index: 0, Extensions: []string{".chd", ".cue", ".bin"}},
	"NeoGeo":          {Core: "_Console/NeoGeo", Delay: 1, Type: "f", Index: 1, Extensions: []string{".neo"}},
	"SGB":             {Core: "_Console/SGB", Delay: 2, Type: "f", Index: 1, Extensions: []string{".gb", ".gbc"}},
	"Saturn":          {Core: "_Console/Saturn", Delay: 1, Type: "s", Index: 0, Extensions: []string{".chd", ".cue"}},
	"Jaguar":          {Core: "_Console/Jaguar", Delay: 1, Type: "f", Index: 1, Extensions: []string{".j64", ".rom"}},
	"NGP":             {Core: "_Console/NeoGeo", Delay: 1, Type: "f", Index: 1, Extensions: []string{".ngp", ".ngc"}},
	"PokemonMini":     {Core: "_Console/PokemonMini", Delay: 1, Type: "f", Index: 1, Extensions: []string{".min"}},
	"SG1000":          {Core: "_Console/ColecoVision", Delay: 1, Type: "f", Index: 1, Extensions: []string{".sg"}},

	// === Computers ===
	"Amiga":       {Core: "_Computer/Minimig", Delay: 1, Type: "s", Index: 0, Extensions: []string{".adf", ".hdf"}},
	"C64":         {Core: "_Computer/C64", Delay: 1, Type: "f", Index: 1, Extensions: []string{".prg", ".crt", ".d64", ".t64"}},
	"C128":        {Core: "_Computer/C128", Delay: 1, Type: "f", Index: 1, Extensions: []string{".prg", ".crt", ".d64"}},
	"VIC20":       {Core: "_Computer/VIC20", Delay: 1, Type: "f", Index: 1, Extensions: []string{".prg", ".crt", ".d64"}},
	"AtariST":     {Core: "_Computer/AtariST", Delay: 1, Type: "s", Index: 0, Extensions: []string{".st", ".msa", ".stx"}},
	"MSX":         {Core: "_Computer/MSX", Delay: 1, Type: "f", Index: 1, Extensions: []string{".rom", ".mx1", ".mx2"}},
	"ZXSpectrum":  {Core: "_Computer/ZX-Spectrum", Delay: 1, Type: "f", Index: 1, Extensions: []string{".tap", ".tzx", ".z80", ".sna"}},
	"ZX81":        {Core: "_Computer/ZX81", Delay: 1, Type: "f", Index: 1, Extensions: []string{".p", ".0"}},
	"Amstrad":     {Core: "_Computer/Amstrad", Delay: 1, Type: "s", Index: 0, Extensions: []string{".dsk", ".cdt"}},
	"AmstradPCW":  {Core: "_Computer/Amstrad-PCW", Delay: 1, Type: "s", Index: 0, Extensions: []string{".dsk"}},
	"BBCMicro":    {Core: "_Computer/BBCMicro", Delay: 1, Type: "s", Index: 0, Extensions: []string{".ssd", ".dsd"}},
	"ao486":       {Core: "_Computer/ao486", Delay: 1, Type: "s", Index: 0, Extensions: []string{".img", ".vhd"}},
	"PCXT":        {Core: "_Computer/PCXT", Delay: 1, Type: "s", Index: 0, Extensions: []string{".img", ".vhd"}},
	"X68000":      {Core: "_Computer/X68000", Delay: 1, Type: "s", Index: 0, Extensions: []string{".dim", ".hdf", ".d88"}},
	"MacPlus":     {Core: "_Computer/MacPlus", Delay: 1, Type: "s", Index: 0, Extensions: []string{".dsk", ".img"}},
	"Archimedes":  {Core: "_Computer/Archimedes", Delay: 1, Type: "s", Index: 0, Extensions: []string{".vhd"}},
	"AppleI":      {Core: "_Computer/Apple-I", Delay: 1, Type: "f", Index: 1, Extensions: []string{".txt"}},
	"AppleII":     {Core: "_Computer/Apple-II", Delay: 1, Type: "s", Index: 0, Extensions: []string{".dsk", ".nib"}},
	"SAMCoupe":    {Core: "_Computer/SAMCoupe", Delay: 1, Type: "s", Index: 0, Extensions: []string{".dsk", ".mgt"}},
	"Altair8800":  {Core: "_Computer/Altair8800", Delay: 1, Type: "f", Index: 1, Extensions: []string{".bin"}},
	"PDP1":        {Core: "_Computer/PDP1", Delay: 1, Type: "f", Index: 1, Extensions: []string{".bin", ".rim"}},
	"PET":         {Core: "_Computer/PET2001", Delay: 1, Type: "f", Index: 1, Extensions: []string{".prg"}},
}

// extensionToSystems maps lowercase extensions to the system names that use them.
// Built once at init from systemMap.
var extensionToSystems map[string][]string

func init() {
	extensionToSystems = make(map[string][]string)
	for name, cfg := range systemMap {
		for _, ext := range cfg.Extensions {
			extensionToSystems[ext] = append(extensionToSystems[ext], name)
		}
	}
}

// GetSystemConfig returns the config for a system name (case-insensitive).
func GetSystemConfig(system string) (SystemConfig, bool) {
	for name, cfg := range systemMap {
		if strings.EqualFold(name, system) {
			return cfg, true
		}
	}
	return SystemConfig{}, false
}

// romSearchPaths returns all directories to scan for a given system.
func romSearchPaths(system string) []struct {
	Dir      string
	Location string
} {
	var paths []struct {
		Dir      string
		Location string
	}

	// SD card
	paths = append(paths, struct {
		Dir      string
		Location string
	}{filepath.Join("/media/fat/games", system), "sd"})

	// USB drives 0-7
	for i := 0; i <= 7; i++ {
		loc := fmt.Sprintf("usb%d", i)
		paths = append(paths, struct {
			Dir      string
			Location string
		}{filepath.Join(fmt.Sprintf("/media/%s", loc), system), loc})
	}

	return paths
}

// findSystemDir does a case-insensitive search for a system's ROM directory
// under a given parent path. Returns the actual path found, or empty string.
func findSystemDir(parent, system string) string {
	entries, err := os.ReadDir(parent)
	if err != nil {
		return ""
	}
	for _, e := range entries {
		if e.IsDir() && strings.EqualFold(e.Name(), system) {
			return filepath.Join(parent, e.Name())
		}
	}
	return ""
}

// scanDir walks a directory and collects ROM files matching the given extensions.
func scanDir(dir, system, location string, extensions map[string]bool) []GameInfo {
	var games []GameInfo
	filepath.Walk(dir, func(path string, info os.FileInfo, err error) error {
		if err != nil || info.IsDir() {
			return nil
		}
		ext := strings.ToLower(filepath.Ext(path))
		if extensions[ext] {
			name := strings.TrimSuffix(filepath.Base(path), filepath.Ext(path))
			games = append(games, GameInfo{
				Name:     name,
				Path:     path,
				System:   system,
				Location: location,
			})
		}
		return nil
	})
	return games
}

// ScanSystem scans ROM directories for a single system.
func ScanSystem(system string) []GameInfo {
	cfg, ok := GetSystemConfig(system)
	if !ok {
		return nil
	}

	extSet := make(map[string]bool, len(cfg.Extensions))
	for _, ext := range cfg.Extensions {
		extSet[ext] = true
	}

	var all []GameInfo

	// SD card - case-insensitive lookup
	if dir := findSystemDir("/media/fat/games", system); dir != "" {
		all = append(all, scanDir(dir, system, "sd", extSet)...)
	}

	// USB drives 0-7
	for i := 0; i <= 7; i++ {
		parent := fmt.Sprintf("/media/usb%d", i)
		if dir := findSystemDir(parent, system); dir != "" {
			loc := fmt.Sprintf("usb%d", i)
			all = append(all, scanDir(dir, system, loc, extSet)...)
		}
	}

	return all
}

// ScanROMs scans all known systems across all ROM locations.
func ScanROMs() []GameInfo {
	var all []GameInfo
	for system := range systemMap {
		all = append(all, ScanSystem(system)...)
	}
	return all
}

// GetSystemStats returns ROM counts per system.
func GetSystemStats() []SystemStats {
	var stats []SystemStats
	for system := range systemMap {
		games := ScanSystem(system)
		if len(games) == 0 {
			continue
		}
		// Group by location
		locCounts := make(map[string]int)
		for _, g := range games {
			locCounts[g.Location] += 1
		}
		for loc, count := range locCounts {
			stats = append(stats, SystemStats{
				System:   system,
				RomCount: count,
				Location: loc,
			})
		}
	}
	return stats
}

// SearchGames searches for games by name. Query terms are AND-matched (case-insensitive).
// If system is non-empty, results are limited to that system.
func SearchGames(query string, system string) []GameInfo {
	terms := strings.Fields(strings.ToLower(query))
	if len(terms) == 0 {
		return nil
	}

	var source []GameInfo
	if system != "" {
		source = ScanSystem(system)
	} else {
		source = ScanROMs()
	}

	var results []GameInfo
	for _, g := range source {
		nameLower := strings.ToLower(g.Name)
		match := true
		for _, term := range terms {
			if !strings.Contains(nameLower, term) {
				match = false
				break
			}
		}
		if match {
			results = append(results, g)
		}
	}
	return results
}

// mglFile and related types for XML generation.
type mglFile struct {
	XMLName xml.Name   `xml:"mistergamedescription"`
	Rbf     string     `xml:"rbf"`
	SetName *string    `xml:"setname,omitempty"`
	File    mglFileRef `xml:"file"`
}

type mglFileRef struct {
	Delay int    `xml:"delay,attr"`
	Type  string `xml:"type,attr"`
	Index int    `xml:"index,attr"`
	Path  string `xml:"path,attr"`
}

// GenerateMGL generates MGL XML content for launching a game.
func GenerateMGL(game GameInfo) string {
	cfg, ok := GetSystemConfig(game.System)
	if !ok {
		return ""
	}

	// MGL paths are relative to the MGL file location (/tmp/).
	// We go up 5 levels to be safe (mrext convention), then use absolute path.
	mglPath := "../../../../.." + game.Path

	m := mglFile{
		Rbf: cfg.Core,
		File: mglFileRef{
			Delay: cfg.Delay,
			Type:  cfg.Type,
			Index: cfg.Index,
			Path:  mglPath,
		},
	}

	if cfg.SetName != "" {
		m.SetName = &cfg.SetName
	}

	output, err := xml.MarshalIndent(m, "", "  ")
	if err != nil {
		return ""
	}
	return xml.Header + string(output)
}

var mglLaunchPath = "/tmp/clawexec_launch.mgl"

// LaunchGame writes an MGL file and tells MiSTer to load it.
func LaunchGame(game GameInfo) error {
	// Verify ROM file exists before attempting to launch
	if _, err := os.Stat(game.Path); err != nil {
		return fmt.Errorf("ROM not found: %s", game.Path)
	}

	mglContent := GenerateMGL(game)
	if mglContent == "" {
		return fmt.Errorf("unknown system: %s", game.System)
	}

	if err := os.WriteFile(mglLaunchPath, []byte(mglContent), 0644); err != nil {
		return fmt.Errorf("writing MGL: %w", err)
	}

	// NOTE: Splash/OSD notification disabled.
	// The Linux framebuffer (/dev/fb0) is only visible when the Menu core is active.
	// When a game core runs, the FPGA takes over video output completely and
	// /dev/fb0 is invisible. Switching to Menu core first causes monitor re-sync
	// delays (especially on CRTs like the IBM C170) and the splash still doesn't
	// reliably show. The MiSTer OSD (F12 menu) is rendered in FPGA hardware and
	// cannot be controlled from Linux userspace.
	// Keeping the OSD code in osd.go for future use if a solution is found.
	//
	// Was:
	//   writeCmd("load_core /media/fat/menu.rbf")
	//   time.Sleep(500 * time.Millisecond)
	//   GetOSD().ShowSplash("Loading...", game.Name)
	//   time.Sleep(5 * time.Second)

	return writeCmd("load_core " + mglLaunchPath)
}
