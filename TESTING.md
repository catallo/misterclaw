# TESTING.md — Core Test Plan

Track test coverage for all supported cores. Goal: verify launch, search, OSD navigation, and settings for each core.

## Test Checklist

For each core, test:
1. **Search** — `search "<game>" --system <System>` finds ROMs
2. **Launch** — Game starts and runs correctly
3. **OSD Navigate** — Navigate to a top-level option (e.g. "Reset")
4. **OSD Sub-page** — Navigate to an option in a sub-menu (if core has sub-pages)
5. **Screenshot** — Verify game is running

Mark: ✅ pass, ❌ fail (with note), ⬜ not tested, ➖ not applicable

## Console Cores

| Core | Search | Launch | OSD Nav | OSD Sub | Screenshot | Notes |
|------|--------|--------|---------|---------|------------|-------|
| NES | ⬜ | ⬜ | ⬜ | ⬜ | ⬜ | Known: off-by-one with hidden palette item |
| SNES | ⬜ | ⬜ | ✅ | ✅ | ⬜ | Aspect Ratio tested |
| MegaDrive | ⬜ | ⬜ | ✅ | ⬜ | ⬜ | |
| Genesis | ⬜ | ⬜ | ⬜ | ⬜ | ⬜ | |
| MasterSystem | ⬜ | ⬜ | ⬜ | ⬜ | ⬜ | |
| SMS | ⬜ | ⬜ | ⬜ | ⬜ | ⬜ | |
| GameGear | ⬜ | ⬜ | ⬜ | ⬜ | ⬜ | |
| Gameboy | ⬜ | ⬜ | ✅ | ⬜ | ⬜ | file_load heuristic reverted |
| GBC | ⬜ | ⬜ | ⬜ | ⬜ | ⬜ | |
| GBA | ⬜ | ⬜ | ⬜ | ⬜ | ⬜ | |
| PSX | ⬜ | ⬜ | ✅ | ⬜ | ⬜ | |
| Saturn | ⬜ | ⬜ | ⬜ | ⬜ | ⬜ | |
| N64 | ⬜ | ⬜ | ⬜ | ⬜ | ⬜ | |
| PCEngine | ⬜ | ⬜ | ⬜ | ⬜ | ⬜ | |
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
| PC8801 | ⬜ | ⬜ | ⬜ | ⬜ | ⬜ | PostLaunch OSD reset |
| X68000 | ⬜ | ⬜ | ⬜ | ⬜ | ⬜ | HDF via subdirs |
| MSX | ⬜ | ⬜ | ⬜ | ⬜ | ⬜ | VHD-based |
| ao486 | ⬜ | ⬜ | ⬜ | ⬜ | ⬜ | |
| C64 | ⬜ | ⬜ | ⬜ | ⬜ | ⬜ | |
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
- **Floppy cores**: PostLaunch OSD reset needed (only PC-8801 configured so far)
- **VHD cores**: MSX, some X68000 — no individual game launch, boots into OS

## Test Log

Record test sessions here with date and findings.

<!-- Example:
### 2026-03-24 — NES
- Search "mario": ✅ found 12 results
- Launch "Super Mario Bros": ✅ boots correctly  
- OSD Navigate "Reset": ❌ lands on wrong item (off by 1)
-->
