# TESTING-CORES.md — Core-by-Core Test Plan

Track test coverage for all supported cores. Goal: verify launch, search, OSD navigation, and settings work reliably before adding new features.

## Test Checklist

For each core, test:
1. **Search** — `search "<game>" --system <System>` finds ROMs
2. **Launch** — Game starts and runs correctly
3. **OSD Navigate** — Navigate to a top-level option (e.g. "Reset")
4. **OSD Sub-page** — Navigate to an option in a sub-menu (if core has sub-pages)
5. **Screenshot** — Verify game is running

Mark: ✅ pass, ❌ fail (with note), ⬜ not tested, ➖ n/a

## Core Overrides

Some cores need special handling beyond standard MGL launch. Configured in `pkg/mister/games.go` (`systemDefaults`).

| Core | Override | Details |
|------|----------|---------|
| PC8801 | OSD reset after launch | Floppy-boot: load .d88 via MGL, then reset to boot from disk (4s delay) |
| MSX | VHD-based, no game launch | Boots into OS from VHD, no individual ROM loading |
| X68000 | ROMs in subdirectory | Games under `GamesHDF/`, not top-level |

This table grows as testing reveals more cores that need special handling.

## Console Cores

| Core | Search | Launch | OSD Nav | OSD Sub | Screenshot | Notes |
|------|--------|--------|---------|---------|------------|-------|
| NES | ✅ | ✅ | ❌ | ❌ | ⬜ | Known off-by-one (hidden palette); Mega Man 2 tested |
| SNES | ⬜ | ⬜ | ✅ | ✅ | ⬜ | Aspect Ratio tested |
| MegaDrive | ⬜ | ⬜ | ✅ | ⬜ | ⬜ | |
| Genesis | ⬜ | ⬜ | ⬜ | ⬜ | ⬜ | |
| MasterSystem | ⬜ | ⬜ | ⬜ | ⬜ | ⬜ | |
| SMS | ⬜ | ⬜ | ⬜ | ⬜ | ⬜ | |
| GameGear | ⬜ | ⬜ | ⬜ | ⬜ | ⬜ | |
| Gameboy | ⬜ | ⬜ | ✅ | ⬜ | ⬜ | file_load heuristic reverted |
| GBC | ⬜ | ⬜ | ⬜ | ⬜ | ⬜ | |
| GBA | ⬜ | ⬜ | ⬜ | ⬜ | ⬜ | |
| PSX | ✅ | ✅ | ✅ | ❌ | ⬜ | Sub-page nav off-by-N (Rotate → Horizontal Crop) |
| Saturn | ⬜ | ⬜ | ⬜ | ⬜ | ⬜ | |
| N64 | ⬜ | ⬜ | ⬜ | ⬜ | ⬜ | |
| PCEngine (TGFX16) | ✅ | ✅ | ✅ | ✅ | ⚠️ | Aspect ratio Sub-page ✅; TGFX16-Alias + Apostroph-Bug gefixt |
| TurboGrafx16 | ⬜ | ⬜ | ⬜ | ⬜ | ⬜ | |
| TGFX16CD | ⬜ | ⬜ | ⬜ | ⬜ | ⬜ | |
| SuperGrafx | ⬜ | ⬜ | ⬜ | ⬜ | ⬜ | |
| MegaCD | ⬜ | ⬜ | ⬜ | ⬜ | ⬜ | |
| S32X | ⬜ | ⬜ | ⬜ | ⬜ | ⬜ | |
| NeoGeo | ⬜ | ⬜ | ⬜ | ⬜ | ⬜ | |
| Atari2600 | ⬜ | ⬜ | ⬜ | ⬜ | ⬜ | |
| Atari5200 | ⬜ | ⬜ | ⬜ | ⬜ | ⬜ | |
| Atari7800 | ⬜ | ⬜ | ⬜ | ⬜ | ⬜ | |
| AtariLynx | ⬜ | ⬜ | ⬜ | ⬜ | ⬜ | |
| Jaguar | ⬜ | ⬜ | ⬜ | ⬜ | ⬜ | |
| ColecoVision | ⬜ | ⬜ | ⬜ | ⬜ | ⬜ | |
| Intellivision | ⬜ | ⬜ | ⬜ | ⬜ | ⬜ | |
| Vectrex | ⬜ | ⬜ | ⬜ | ⬜ | ⬜ | |
| ChannelF | ⬜ | ⬜ | ⬜ | ⬜ | ⬜ | |
| Odyssey2 | ⬜ | ⬜ | ⬜ | ⬜ | ⬜ | |
| SG1000 | ⬜ | ⬜ | ⬜ | ⬜ | ⬜ | |
| SGB | ⬜ | ⬜ | ⬜ | ⬜ | ⬜ | |
| WonderSwan | ⬜ | ⬜ | ⬜ | ⬜ | ⬜ | |
| WonderSwanColor | ⬜ | ⬜ | ⬜ | ⬜ | ⬜ | |
| NGP | ⬜ | ⬜ | ⬜ | ⬜ | ⬜ | |
| PokemonMini | ⬜ | ⬜ | ⬜ | ⬜ | ⬜ | |

## Computer Cores

| Core | Search | Launch | OSD Nav | OSD Sub | Screenshot | Notes |
|------|--------|--------|---------|---------|------------|-------|
| PC8801 | ⬜ | ⬜ | ⬜ | ⬜ | ⬜ | PostLaunch OSD reset, floppy-boot |
| X68000 | ⬜ | ⬜ | ⬜ | ⬜ | ⬜ | HDF via subdirs |
| MSX | ⬜ | ⬜ | ⬜ | ⬜ | ⬜ | VHD-based, no individual game launch |
| ao486 | ⬜ | ⬜ | ⬜ | ⬜ | ⬜ | |
| C64 | ✅ | ✅ | ✅ | ✅ | ✅ | DolphinDOS 2.0 required. D64/G64/T64/D81: disk mount + Alt+ESC auto-load. PRG: direct injection. CRT: instant boot. TAP: not auto-launchable (DolphinDOS conflict). 20/20 games tested. |
| C128 | ⬜ | ⬜ | ⬜ | ⬜ | ⬜ | |
| Amiga | ⬜ | ⬜ | ⬜ | ⬜ | ⬜ | |
| AtariST | ⬜ | ⬜ | ⬜ | ⬜ | ⬜ | |
| Amstrad | ⬜ | ⬜ | ⬜ | ⬜ | ⬜ | |
| AppleII | ⬜ | ⬜ | ⬜ | ⬜ | ⬜ | |
| MacPlus | ⬜ | ⬜ | ⬜ | ⬜ | ⬜ | |
| BBCMicro | ⬜ | ⬜ | ⬜ | ⬜ | ⬜ | |
| ZXSpectrum | ⬜ | ⬜ | ⬜ | ⬜ | ⬜ | |
| ZX81 | ⬜ | ⬜ | ⬜ | ⬜ | ⬜ | |
| SAMCoupe | ⬜ | ⬜ | ⬜ | ⬜ | ⬜ | |
| VIC20 | ⬜ | ⬜ | ⬜ | ⬜ | ⬜ | |
| PET | ⬜ | ⬜ | ⬜ | ⬜ | ⬜ | |
| Archimedes | ⬜ | ⬜ | ⬜ | ⬜ | ⬜ | |

## Known Issues

- **NES**: off-by-one OSD navigation (hidden "Custom Palette" file_load counted)
- **Floppy cores**: PostLaunch OSD reset needed — only PC-8801 configured so far
- **VHD cores**: MSX, some X68000 — no individual game launch, boots into OS

## Test Log

### 2026-03-24 — PCEngine / TurboGrafx16 (TGFX16)
- Search "bonk" --system TGFX16: ✅ 26 Ergebnisse
- Launch "Bonk's Adventure": ✅ Läuft (Bonk III gestartet)
- OSD Navigate "Reset": ✅ `Navigated to: Reset (core: TurboGrafx16)`
- OSD Sub-page: ➖ nicht getestet (wenige Sub-Pages)
- Screenshot: ⚠️ schwarzes Bild (Framebuffer-Problem, nicht Core-Problem)
- **Bug:** TGFX16 Ordner nicht als System erkannt → Alias `TGFX16` in systemDefaults hinzugefügt
- **Bug:** Apostrophe in Dateinamen (Bomberman '94) → `&#39;` XML-Escape in MGL-Pfaden bricht MiSTer → Fix: raw single quotes
- OSD Sub-page "Aspect ratio": ✅ Cursor korrekt positioniert

### 2026-03-24 — PSX
- Search "tekken": ✅ 2 Ergebnisse
- Launch "Tekken 3": ✅ Spiel läuft
- OSD Navigate "Aspect ratio" (sub-page): ✅ korrekt
- OSD Navigate "Rotate" (sub-page): ❌ Cursor auf "Horizontal Crop" statt "Rotate"
- cfg-read: ✅ Render 24 Bit = On (DE-prefix fix)
- **Bug:** OSD muss vor Navigate geschlossen werden → Escape vor F12 hinzugefügt
- **Bug:** Sub-page Navigation off-by-N bei tieferen Items auf Video & Audio Seite

### 2026-03-24 — NES
- Search "mega man 2": ✅ 5+ Ergebnisse
- Launch "Mega Man 2": ✅ Spiel läuft
- OSD Navigate: ❌ Known off-by-one (hidden Custom Palette file_load)
- OSD Sub-page: ❌ Known off-by-one


Record test sessions here with date and findings.

<!-- Example:
### 2026-03-24 — NES
- Search "mario": ✅ found 12 results
- Launch "Super Mario Bros": ✅ boots correctly  
- OSD Navigate "Reset": ❌ lands on wrong item (off by 1)
-->
