package mister

import (
	"encoding/xml"
	"log"
	"fmt"
	"os"
	"time"
	"path/filepath"
	"strings"
)

// PostLaunchConfig defines actions to perform after launching a game.
type PostLaunchConfig struct {
	OSDReset bool   `json:"osd_reset"`          // perform OSD Reset after launch (floppy-disk cores)
	DelayMs  int    `json:"delay_ms,omitempty"`  // delay before post-launch actions (ms)
	Notes    string `json:"notes,omitempty"`     // usage notes for this system
}

// SystemConfig defines how to launch games for a given system.
type SystemConfig struct {
	Core       string           `json:"core"`
	Delay      int              `json:"delay"`
	Type       string           `json:"type"`       // "f" or "s"
	Index      int              `json:"index"`
	Extensions []string         `json:"extensions"`
	SetName    string           `json:"set_name,omitempty"`    // optional, for systems sharing a core (GBC, GameGear, etc.)
	PostLaunch *PostLaunchConfig `json:"post_launch,omitempty"` // post-launch actions (OSD reset, notes)
}

// GameInfo represents a single ROM file.
type GameInfo struct {
	Name     string `json:"name"`     // filename without extension
	Path     string `json:"path"`     // full absolute path
	System   string `json:"system"`   // system name (from discovery or systemDefaults)
	Location string `json:"location"` // "sd" or "usb0" etc.
}

// SystemStats summarizes ROM counts per system.
type SystemStats struct {
	System   string `json:"system"`
	RomCount int    `json:"rom_count"`
	Location string `json:"location"`
}

var systemDefaults = map[string]SystemConfig{
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
	"PC8801":      {Core: "_Computer/PC88", Delay: 2, Type: "s", Index: 0, Extensions: []string{".d88"}, PostLaunch: &PostLaunchConfig{OSDReset: true, DelayMs: 4000, Notes: "STOP=End, CLR=Home, GRPH=Alt, HELP=F11. 2 FDD drives (D88)."}},

}

// GetSystemConfig returns the config for a system name (case-insensitive).
// Checks discovered systems first, then falls back to systemDefaults.
func GetSystemConfig(system string) (SystemConfig, bool) {
	// Check discovered systems first
	discovered := getDiscoveredSystems()
	if ds, ok := discovered[strings.ToLower(system)]; ok {
		cfg := ds.Config
		// Merge PostLaunch from systemDefaults if not set by discovery
		if cfg.PostLaunch == nil {
			if defCfg, ok := getDefaultConfig(system); ok && defCfg.PostLaunch != nil {
				cfg.PostLaunch = defCfg.PostLaunch
			}
		}
		return cfg, true
	}
	// Fall back to systemDefaults
	return getDefaultConfig(system)
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

// ScanSystem scans ROM directories for a single system using discovered folders.
func ScanSystem(system string) []GameInfo {
	discovered := getDiscoveredSystems()
	ds, ok := discovered[strings.ToLower(system)]
	if !ok {
		return nil
	}

	extSet := make(map[string]bool, len(ds.Config.Extensions))
	for _, ext := range ds.Config.Extensions {
		extSet[ext] = true
	}

	var all []GameInfo
	for _, folder := range ds.Folders {
		all = append(all, scanDir(folder.Path, ds.Name, folder.Location, extSet)...)
	}
	return all
}

// ScanROMs scans all discovered systems across all ROM locations.
func ScanROMs() []GameInfo {
	discovered := getDiscoveredSystems()
	var all []GameInfo
	for _, ds := range discovered {
		extSet := make(map[string]bool, len(ds.Config.Extensions))
		for _, ext := range ds.Config.Extensions {
			extSet[ext] = true
		}
		for _, folder := range ds.Folders {
			all = append(all, scanDir(folder.Path, ds.Name, folder.Location, extSet)...)
		}
	}
	return all
}

// GetSystemStats returns ROM counts per system from the discovery cache.
func GetSystemStats() []SystemStats {
	discovered := getDiscoveredSystems()
	var stats []SystemStats
	for _, ds := range discovered {
		if ds.TotalROMs == 0 {
			continue
		}
		for _, folder := range ds.Folders {
			if folder.RomCount == 0 {
				continue
			}
			stats = append(stats, SystemStats{
				System:   ds.Name,
				RomCount: folder.RomCount,
				Location: folder.Location,
			})
		}
	}
	return stats
}

// SearchGames searches for games by name using the cached game listings.
// Query terms are AND-matched (case-insensitive).
// If system is non-empty, results are limited to that system.
// Returns nil if the game cache is not yet ready.
func SearchGames(query string, system string) []GameInfo {
	terms := strings.Fields(strings.ToLower(query))
	if len(terms) == 0 {
		return nil
	}

	games := getCachedGames()
	if games == nil {
		return nil
	}

	var source []GameInfo
	if system != "" {
		source = games[strings.ToLower(system)]
	} else {
		for _, sysGames := range games {
			source = append(source, sysGames...)
		}
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
	if !ok || cfg.Core == "" {
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

// mglPath returns the MGL launch path using the core's base name.
// MiSTer derives CFG config filenames from the MGL filename, so using the
// core name (e.g. PSX.mgl) ensures MiSTer loads PSX.CFG instead of _launch.CFG.
// The MGL is placed in the core's parent directory so the OSD file browser
// shows the correct category (e.g. _Console/, _Computer/).
func mglPath(corePath string) string {
	base := filepath.Base(corePath)
	parent := filepath.Dir(corePath)
	if parent == "." || parent == "" {
		return "/tmp/" + base + ".mgl"
	}
	return filepath.Join("/media/fat", parent, base+".mgl")
}

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

	// Place MGL in core's parent dir so OSD browser has correct context
	cfg, _ := GetSystemConfig(game.System)
	launchPath := mglPath(cfg.Core)
	if err := os.MkdirAll(filepath.Dir(launchPath), 0755); err != nil {
		return fmt.Errorf("creating MGL dir: %w", err)
	}
	if err := os.WriteFile(launchPath, []byte(mglContent), 0644); err != nil {
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

	if err := writeCmd("load_core " + launchPath); err != nil {
		return err
	}

	// For systems with post-launch actions (e.g. floppy-disk cores), perform
	// an OSD Reset after launch so the core boots from the mounted disk
	// instead of falling into built-in BASIC/ROM.
	cfg, _ = GetSystemConfig(game.System)
	if cfg.PostLaunch != nil && cfg.PostLaunch.OSDReset {
		coreName := filepath.Base(cfg.Core)
		delay := time.Duration(cfg.PostLaunch.DelayMs) * time.Millisecond
		if delay == 0 {
			delay = 4 * time.Second
		}
		go func() {
			time.Sleep(delay)
			if err := OSDResetByCore(coreName); err != nil { log.Printf("[misterclaw] OSD reset failed: %v", err) } else { log.Printf("[misterclaw] OSD reset completed for %s", coreName) }
		}()
	}

	return nil
}
