# 👾🦀 MisterClaw — AI-powered remote control for MiSTer-FPGA

Remote control server and CLI client for MiSTer-FPGA. Built on the ClawExec protocol.

![Version](https://img.shields.io/badge/Version-v0.1.0-blue)
![Go](https://img.shields.io/badge/Go-1.21+-00ADD8?logo=go&logoColor=white)
![License](https://img.shields.io/badge/License-MIT-green)
![Platform](https://img.shields.io/badge/Platform-ARMv7_(DE10--Nano)-blue)

---

## For AI Agents

This section is the primary reference for LLMs and AI agents. It contains everything you need to control a MiSTer-FPGA remotely.

### Quick Reference

| Property | Value |
|----------|-------|
| Server binary | `misterclaw` |
| Client binary | `misterclaw-send` |
| Protocol | Newline-delimited JSON over TCP |
| Default port | `9900` |
| Default host | `mister-fpga` (Tailscale hostname, auto-discovered on LAN) |
| JSON flag | `--json` (or `-j`) for machine-parseable output on any command |
| Timeout flag | `--timeout 30` (or `-t 30`) recommended for search/launch |
| Host flag | `--host <ip>` (or `-H <ip>`) — usually not needed |

### Installation

**Server** (on MiSTer-FPGA):
```bash
wget -q https://github.com/catallo/misterclaw/releases/latest/download/misterclaw-arm7 -O /tmp/misterclaw && chmod +x /tmp/misterclaw && /tmp/misterclaw --install
```

**Client** (on your machine):
```bash
# Linux amd64:
wget https://github.com/catallo/misterclaw/releases/latest/download/misterclaw-send-linux-amd64 -O misterclaw-send && chmod +x misterclaw-send

# macOS Apple Silicon:
wget https://github.com/catallo/misterclaw/releases/latest/download/misterclaw-send-darwin-arm64 -O misterclaw-send && chmod +x misterclaw-send
```

### All Commands

#### `status` — Current core and game

```bash
misterclaw-send status
```
```
Core: SNES
Game: /media/fat/games/SNES/Super Mario World (USA).sfc
```

```bash
misterclaw-send status --json
```
```json
{
  "mister": "status",
  "core_name": "SNES",
  "core_path": "/media/fat/_Console/SNES_20250705.rbf",
  "game_path": "/media/fat/games/SNES/Super Mario World (USA).sfc"
}
```

#### `search` — Search ROM library

```bash
misterclaw-send search "zelda" --system SNES --limit 5
```
```
1. Legend of Zelda, The - A Link to the Past (USA) [SNES, sd]
2. Zelda no Densetsu - Kamigami no Triforce (Japan) [SNES, sd]
Found: 2 results
```

```bash
misterclaw-send search "zelda" --json --timeout 30
```
```json
{
  "mister": "search",
  "results": [
    {"name": "Legend of Zelda, The - A Link to the Past (USA)", "system": "SNES", "path": "/media/fat/games/SNES/Legend of Zelda, The - A Link to the Past (USA).sfc", "location": "sd"},
    {"name": "Legend of Zelda, The (USA)", "system": "NES", "path": "/media/fat/games/NES/Legend of Zelda, The (USA).nes", "location": "sd"}
  ],
  "total": 2
}
```

#### `launch` — Launch a game

By fuzzy search (picks best match):
```bash
misterclaw-send launch "super mario world" --system SNES
```
```
Launched: Super Mario World (USA)
```

By direct path:
```bash
misterclaw-send launch --path "/media/usb0/SNES/Super Mario World (USA).sfc" --system SNES
```
```
Launched: Super Mario World (USA)
```

Cross-system fuzzy search (no `--system`):
```bash
misterclaw-send launch "sonic 2"
```
```
Launched: Sonic The Hedgehog 2 (World)
```

```bash
misterclaw-send launch "castlevania" --system NES --json
```
```json
{
  "mister": "launch",
  "success": true,
  "game": "Castlevania (USA)",
  "core_name": "NES"
}
```

#### `systems` — List available systems and ROM counts

```bash
misterclaw-send systems
```
```
SNES             13435 ROMs (sd)
C64              46736 ROMs (sd)
Amiga            55793 ROMs (usb0)
MegaDrive         8341 ROMs (sd)
NES               7204 ROMs (sd)
Gameboy           4512 ROMs (usb0)
GBA               3891 ROMs (usb0)
PSX                342 ROMs (usb1)
```

```bash
misterclaw-send systems --json
```
```json
{
  "mister": "systems",
  "systems": [
    {"system": "SNES", "rom_count": 13435, "location": "sd"},
    {"system": "C64", "rom_count": 46736, "location": "sd"},
    {"system": "Amiga", "rom_count": 55793, "location": "usb0"},
    {"system": "MegaDrive", "rom_count": 8341, "location": "sd"},
    {"system": "NES", "rom_count": 7204, "location": "sd"}
  ]
}
```

#### `screenshot` — Capture current screen as PNG

Base64 to stdout (for piping/agents):
```bash
misterclaw-send screenshot
```
```
iVBORw0KGgoAAAANSUhEU...
```

Save to file:
```bash
misterclaw-send screenshot --output shot.png
```
```
Screenshot saved: shot.png (148KB, SNES core)
```

JSON with metadata:
```bash
misterclaw-send screenshot --json
```
```json
{
  "mister": "screenshot",
  "success": true,
  "data": "iVBORw0KGgoAAAANSUhEU...",
  "core": "SNES",
  "filename": "SNES-20260322-143012.png",
  "size": 151632
}
```

#### `info` — System information

```bash
misterclaw-send info
```
```
Hostname: MiSTer
IP:       10.0.0.8
Temp:     52.3°C
RAM:      416/492 MB free
Disk:     /media/fat — 8755/244005 MB free (97% used) [/dev/root]
Disk:     /media/usb0 — 11452/57449 MB free (79% used) [/dev/sda1]
Disk:     /media/usb1 — 489/117355 MB free (100% used) [/dev/sdb1]
Uptime:   3d 14h
```

```bash
misterclaw-send info --json
```
```json
{
  "hostname": "MiSTer",
  "ip": "10.0.0.8",
  "temp": 52.3,
  "ram_mb": 492,
  "ram_free_mb": 416,
  "disks": [
    {"mount": "/media/fat", "device": "/dev/root", "total_mb": 244005, "free_mb": 8755, "use_pct": "97%"},
    {"mount": "/media/usb0", "device": "/dev/sda1", "total_mb": 57449, "free_mb": 11452, "use_pct": "79%"},
    {"mount": "/media/usb1", "device": "/dev/sdb1", "total_mb": 117355, "free_mb": 489, "use_pct": "100%"}
  ],
  "uptime": "28m"
}
```

#### `tailscale` — VPN management

```bash
misterclaw-send tailscale setup
```
```
Please authenticate: https://login.tailscale.com/a/abc123def456
Waiting for authentication...
Connected! IP: 100.92.156.99
```

```bash
misterclaw-send tailscale status
```
```
Tailscale: running
IP: 100.92.156.99
Hostname: mister-fpga
Online: yes
```

```bash
misterclaw-send tailscale status --json
```
```json
{
  "installed": true,
  "running": true,
  "ip": "100.92.156.99",
  "hostname": "mister-fpga",
  "online": true,
  "backend_state": "Running"
}
```

```bash
misterclaw-send tailscale start
misterclaw-send tailscale stop
```
```
Tailscale start: OK
Tailscale stop: OK
```

#### `shell` — Execute any command on MiSTer-FPGA

```bash
misterclaw-send shell "ls /media/fat/games/"
```
```
SNES
MegaDrive
NES
Gameboy
GBA
```

```bash
misterclaw-send shell "cat /proc/uptime" --json
```
```json
{
  "output": "307432.12 290118.45\n",
  "exit_code": 0
}
```

#### `discover` — Scan LAN for MiSTer-FPGA servers

```bash
misterclaw-send discover
```
```
Found 1 MiSTer-FPGA server(s):
  10.0.0.8:9900 — Core: SNES_20250605
```

```bash
misterclaw-send discover --json
```
```json
[{"host": "10.0.0.8", "port": 9900, "core": "SNES_20250605"}]
```

### Common Workflows

#### First-Time Setup

```bash
# 1. Install server on MiSTer-FPGA (SSH in first)
wget -q https://github.com/catallo/misterclaw/releases/latest/download/misterclaw-arm7 -O /tmp/misterclaw
chmod +x /tmp/misterclaw
/tmp/misterclaw --install
# Binary copied to /media/fat/Scripts/, autostart configured

# 2. Set up Tailscale VPN (on your machine with client installed)
misterclaw-send tailscale setup
# → Returns auth URL — open it in browser to authenticate
# → Polls until authenticated, returns Tailscale IP

# 3. Get Tailscale IP
misterclaw-send tailscale status --json
# → {"running": true, "ip": "100.92.156.99", "hostname": "mister-fpga", "online": true, ...}

# 4. From now on, default hostname "mister-fpga" or auto-discovery — no --host needed
misterclaw-send status
```

#### Launch a Game

```bash
# 1. Search for the game (use --timeout 30 for large libraries)
misterclaw-send search "super mario" --json --timeout 30
# → Returns list of matches with system, path, location

# 2. Launch by fuzzy search (picks best match)
misterclaw-send launch "super mario world" --system SNES

# OR: Launch by exact path
misterclaw-send launch --path "/media/fat/games/SNES/Super Mario World (USA).sfc" --system SNES
```

#### Check Status

```bash
misterclaw-send status --json
# → {"mister": "status", "core_name": "SNES", "core_path": "...", "game_path": "..."}
```

#### Take a Screenshot

```bash
misterclaw-send screenshot --json
# → {"mister": "screenshot", "success": true, "data": "<base64 PNG>", "core": "SNES", "filename": "...", "size": 151632}
```

#### System Diagnostics

```bash
misterclaw-send info --json
# → hostname, IP, temp, RAM, all mounted disks, uptime
```

### System Names

Systems are auto-detected from your ROM library — there is no hardcoded list. Any folder containing ROMs under `/media/fat/games/` or USB drives (`/media/usb0/` through `/media/usb7/`) is automatically discovered as a system. A well-stocked MiSTer typically has 70+ systems. System names match the folder names on disk (case-insensitive).

Core matching is automatic: ROM folder names are matched to installed `.rbf` cores and `.mgl` mappings. Well-known systems (SNES, NES, Genesis, PSX, etc.) have curated MGL launch parameters built in as defaults. Unknown systems get sensible defaults — CD-based systems (with `.chd`/`.cue`/`.iso` files) are detected automatically.

Use `misterclaw-send systems` to see all detected systems and ROM counts.

### Background Discovery

System discovery runs in the background at server startup, recursively scanning all ROM folders across SD and USB drives. On a typical setup this takes ~90 seconds. During the scan, `systems` and `search` commands return `"status": "pending"` until discovery completes. Once finished, results are cached in memory for instant access.

### Error Handling

Errors are returned as JSON with an `"error"` key:

```json
{"error": "connecting to mister-fpga:9900: dial tcp: lookup mister-fpga: no such host"}
{"error": "no game found or missing parameters"}
{"error": "ROM not found: /media/fat/games/SNES/nonexistent.sfc"}
{"error": "/dev/MiSTer_cmd not found (not running on MiSTer?)"}
{"error": "unknown system: INVALID"}
```

The CLI exits with code 1 on error and prints to stderr:
```
Error: connecting to mister-fpga:9900: dial tcp: lookup mister-fpga: no such host
```

### Tips & Gotchas

- **Timeout**: Use `--timeout 30` for `search` and `launch` — ROM scanning can take time on large libraries (thousands of ROMs across multiple USB drives)
- **Fuzzy matching**: Search is case-insensitive and uses substring matching. All search terms must match (AND logic). Partial names work: `"mario"` finds all Mario games
- **Multiple disks**: ROMs can be on SD card (`/media/fat/games/`) or USB drives (`/media/usb0/` through `/media/usb7/`). Search scans all locations automatically
- **Auto-discovery**: On LAN, `--host` is usually not needed. The client probes the default hostname `mister-fpga` first, then scans the local /24 subnet
- **Screenshots**: Return base64-encoded PNG in JSON mode. Use `--output file.png` to save to disk
- **Shell command**: `shell "any linux command"` gives full shell access to the MiSTer. Output is streamed
- **All commands support `--json`** for structured, machine-parseable output
- **`--path` requires `--system`**: When launching by direct path, you must also specify the system
- **Cross-system search**: Omit `--system` to search all systems at once
- **Self-install**: The server binary can install itself with `--install` — copies to `/media/fat/Scripts/`, configures autostart on boot, and is idempotent (safe to run multiple times)

---

## For Humans

### What Is This?

MisterClaw lets your AI agent control your MiSTer-FPGA. Search your game library, launch ROMs, take screenshots, check disk space, set up VPN access — all through natural conversation. You never need to learn CLI commands.

### Setup

**One-time install on MiSTer** (SSH in):
```bash
wget -q https://github.com/catallo/misterclaw/releases/latest/download/misterclaw-arm7 -O /tmp/misterclaw
chmod +x /tmp/misterclaw
/tmp/misterclaw --install
```

**Install the client** on your machine (your agent uses this behind the scenes):
```bash
# Linux
wget https://github.com/catallo/misterclaw/releases/latest/download/misterclaw-send-linux-amd64 -O misterclaw-send
chmod +x misterclaw-send

# macOS
wget https://github.com/catallo/misterclaw/releases/latest/download/misterclaw-send-darwin-arm64 -O misterclaw-send
chmod +x misterclaw-send
```

To uninstall: `/media/fat/Scripts/misterclaw --uninstall`

### What It Looks Like

Once installed, you just talk to your AI agent:

```
You: "Start Sonic 2 on the MiSTer"
Agent: Launched Sonic The Hedgehog 2 on MegaDrive! 🦔

You: "What systems do I have?"
Agent: Your MiSTer has 70+ systems! Top ones:
  - SNES: 13,435 ROMs
  - C64: 46,736 ROMs
  - Amiga: 55,793 ROMs
  ...

You: "Take a screenshot"
Agent: [shows screenshot from MiSTer]

You: "Set up Tailscale so I can play from anywhere"
Agent: Installing Tailscale... Done!
       Please authenticate: https://login.tailscale.com/a/xxx
       After that your MiSTer is reachable from anywhere.
```

Your agent reads the documentation above, picks the right commands, and handles everything. You just say what you want.

---

## Protocol Reference

Newline-delimited JSON over TCP (port 9900). Each request is a single JSON object followed by a newline. Responses are one or more JSON objects, each on its own line.

### MiSTer Commands

```json
{"mister": "status"}
{"mister": "info"}
{"mister": "systems"}
{"mister": "search", "query": "sonic", "system": "Genesis"}
{"mister": "launch", "query": "sonic 2", "system": "MegaDrive"}
{"mister": "launch", "path": "/media/fat/games/SNES/Game.sfc", "system": "SNES"}
{"mister": "screenshot"}
{"mister": "load_core", "core": "SNES"}
{"mister": "load_core", "path": "/media/fat/_Console/SNES_20250705.rbf"}
{"mister": "tailscale", "action": "setup"}
{"mister": "tailscale", "action": "status"}
{"mister": "tailscale", "action": "start"}
{"mister": "tailscale", "action": "stop"}
```

### Shell Commands

```json
{"cmd": "ls -la", "session": "browse", "pty": true}
{"input": "some text", "session": "browse"}
{"list": true}
{"kill": true, "session": "browse"}
{"close": true, "session": "browse"}
{"session": "browse", "resize": {"cols": 80, "rows": 24}}
```

### Response Format

```json
{"mister": "status", "core_name": "SNES", "core_path": "...", "game_path": "..."}
{"id": "uuid", "stream": "stdout", "data": "output line..."}
{"id": "uuid", "done": true, "exit_code": 0, "sessions": [...]}
{"error": "description of what went wrong"}
```

---

## Building from Source

```bash
git clone https://github.com/catallo/misterclaw.git
cd misterclaw

# Build server (ARM7 for MiSTer)
GOOS=linux GOARCH=arm GOARM=7 go build -ldflags="-s -w" -o misterclaw ./cmd/misterclaw/

# Build client (for your machine)
go build -ldflags="-s -w" -o misterclaw-send ./cmd/misterclaw-send/

# Run tests
go test ./...
```

---

## Architecture

```
cmd/misterclaw/main.go       Server entry point, flags, signal handling
cmd/misterclaw-send/main.go  Client CLI, subcommands, flag parsing
pkg/
  server/server.go                      TCP server, JSON protocol dispatch
  session/manager.go                    Session management, per-session locks
  pty/executor.go                       PTY execution (creack/pty) + pipe fallback
  mister/
    cmd.go                              /dev/MiSTer_cmd interface (load_core, screenshots)
    discover.go                         Dynamic system discovery (cores, MGLs, ROM folders)
    osd.go                              Framebuffer OSD rendering (8x16 bitmap font)
    games.go                            ROM filesystem scanner, search index, MGL generator
    screenshots.go                      Screenshot capture from /media/fat/screenshots/
    system.go                           System info (temp, RAM, disk, network, uptime)
    tailscale.go                        Tailscale VPN: download, setup, start/stop, status
```

---

## License

MIT

## Security

> ⚠️ **Do not expose port 9900 to the internet.** The MisterClaw protocol has no authentication or encryption.

**Recommended setups:**
- **Tailscale VPN** (best) — encrypted, authenticated, works from anywhere. Use Tailscale: already authenticated
IP: 100.101.202.25 to configure.
- **Local network only** — keep port 9900 accessible only within your LAN.
- **Never** forward port 9900 through your router or firewall.
