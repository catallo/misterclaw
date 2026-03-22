# ClawExec for MiSTer-FPGA

Remote control server and CLI client for MiSTer-FPGA retro gaming platforms.

![Go](https://img.shields.io/badge/Go-1.21+-00ADD8?logo=go&logoColor=white)
![License](https://img.shields.io/badge/License-MIT-green)
![Platform](https://img.shields.io/badge/Platform-ARMv7_(DE10--Nano)-blue)

## What is this?

- A **headless TCP server** that runs on MiSTer-FPGA (DE10-Nano)
- A **dedicated CLI client** to control it from anywhere
- Speaks a **JSON protocol over TCP** (port 9900)
- **Zero configuration** needed — works out of the box
- Built for **AI agents** but also great for humans and scripts

## Why?

- **Remote control** your MiSTer-FPGA from any machine on your network
- **AI agents** can search your ROM library, launch games, take screenshots
- **Automated Tailscale VPN setup** — one command to make your MiSTer reachable from anywhere
- **No dependencies** on the MiSTer side (single static binary, ~3 MB)

## Features

### Launch Games

Fuzzy search across your entire ROM library, filter by system, or use a direct path.

```bash
# Fuzzy search — finds best match and launches it
$ clawexec-mister-fpga-send launch "super mario world" --system SNES
Launched: Super Mario World (USA)

# Direct path for exact control
$ clawexec-mister-fpga-send launch --path "/media/usb0/SNES/Super Mario World (USA).sfc" --system SNES
Launched: Super Mario World (USA)

# Cross-system fuzzy search (no --system)
$ clawexec-mister-fpga-send launch "sonic 2"
Launched: Sonic The Hedgehog 2 (World)
```

### Search ROM Library

Search across all systems and all storage devices (SD card, USB drives).

```bash
$ clawexec-mister-fpga-send search "zelda" --system SNES --limit 5
1. Legend of Zelda, The - A Link to the Past (USA) [SNES, sd]
2. Zelda no Densetsu - Kamigami no Triforce (Japan) [SNES, sd]
Found: 2 results

# JSON output for scripts/agents
$ clawexec-mister-fpga-send search "zelda" --json
{
  "mister": "search",
  "results": [
    {"name": "Legend of Zelda, The - A Link to the Past (USA)", "system": "SNES", "path": "/media/fat/games/SNES/...", "location": "sd"},
    ...
  ],
  "total": 12
}
```

### List Systems

See all available systems with ROM counts.

```bash
$ clawexec-mister-fpga-send systems
SNES              892 ROMs (sd)
MegaDrive         634 ROMs (sd)
NES               512 ROMs (sd)
Gameboy           340 ROMs (usb0)
GBA               256 ROMs (usb0)
PSX                42 ROMs (usb1)
```

### Screenshots

Capture the current screen as PNG.

```bash
# Save to file
$ clawexec-mister-fpga-send screenshot --output shot.png
Screenshot saved: shot.png (148KB, SNES core)

# Base64 to stdout (for piping/agents)
$ clawexec-mister-fpga-send screenshot
iVBORw0KGgoAAAANSUhEU...

# JSON with metadata
$ clawexec-mister-fpga-send screenshot --json
{
  "mister": "screenshot",
  "success": true,
  "data": "iVBORw0KGgoAAAANSUhEU...",
  "core": "SNES",
  "filename": "SNES-20260322-143012.png",
  "size": 151632
}
```

### System Info

Temperature, RAM, disk usage, uptime, and network.

```bash
$ clawexec-mister-fpga-send info
Hostname: MiSTer
IP:       192.168.1.100
Temp:     52.3°C
RAM:      312/492 MB free
Disk:     10240/14800 MB free
Uptime:   3d 14h 22m
```

### Status

Check what core and game are currently running.

```bash
$ clawexec-mister-fpga-send status
Core: SNES
Game: /media/fat/games/SNES/Super Mario World (USA).sfc

$ clawexec-mister-fpga-send status --json
{
  "mister": "status",
  "core_name": "SNES",
  "core_path": "/media/fat/_Console/SNES_20250705.rbf",
  "game_path": "/media/fat/games/SNES/Super Mario World (USA).sfc"
}
```

### Tailscale VPN

Automated Tailscale setup, status, and management.

```bash
$ clawexec-mister-fpga-send tailscale setup
Please authenticate: https://login.tailscale.com/a/abc123def456
Waiting for authentication...
Connected! IP: 100.92.156.99

$ clawexec-mister-fpga-send tailscale status
Tailscale: running
IP: 100.92.156.99
Hostname: mister-fpga
Online: yes

$ clawexec-mister-fpga-send tailscale stop
Tailscale stop: OK
```

### Shell Access

Execute any command directly on MiSTer-FPGA.

```bash
$ clawexec-mister-fpga-send shell "ls /media/fat/games/"
SNES
MegaDrive
NES
Gameboy
GBA

$ clawexec-mister-fpga-send shell "cat /proc/uptime" --json
{
  "output": "307432.12 290118.45\n",
  "exit_code": 0
}
```

## Installation

### Server (on MiSTer-FPGA)

```bash
# Download the latest release
wget -q https://github.com/catallo/clawexec-mister-fpga/releases/latest/download/clawexec-mister-fpga-arm7 -O /media/fat/Scripts/clawexec-mister-fpga
chmod +x /media/fat/Scripts/clawexec-mister-fpga

# Add to autostart
echo '[[ -e /media/fat/Scripts/clawexec-mister-fpga ]] && /media/fat/Scripts/clawexec-mister-fpga --port 9900 &' >> /media/fat/linux/user-startup.sh

# Start now
/media/fat/Scripts/clawexec-mister-fpga --port 9900 &
```

### Client (on your machine)

```bash
# Linux amd64:
wget https://github.com/catallo/clawexec-mister-fpga/releases/latest/download/clawexec-mister-fpga-send-linux-amd64

# macOS Apple Silicon:
wget https://github.com/catallo/clawexec-mister-fpga/releases/latest/download/clawexec-mister-fpga-send-darwin-arm64

chmod +x clawexec-mister-fpga-send-*
```

### Build from Source

```bash
git clone https://github.com/catallo/clawexec-mister-fpga.git
cd clawexec-mister-fpga

# Build server (ARM7 for MiSTer)
GOOS=linux GOARCH=arm GOARM=7 go build -ldflags="-s -w" -o clawexec-mister-fpga ./cmd/clawexec-mister-fpga/

# Build client (for your machine)
go build -ldflags="-s -w" -o clawexec-mister-fpga-send ./cmd/clawexec-mister-fpga-send/
```

## Tailscale VPN Setup

Make your MiSTer-FPGA reachable from anywhere with one command:

```bash
clawexec-mister-fpga-send --host <mister-local-ip> tailscale setup
```

This command:
1. Downloads Tailscale ARM binaries to MiSTer
2. Starts the Tailscale daemon in userspace mode (no kernel module needed)
3. Returns an auth URL — open it in your browser to authenticate
4. Polls until authentication completes
5. Returns the Tailscale IP address

After setup, your MiSTer is reachable from anywhere on your Tailscale network. The default hostname `mister-fpga` works automatically:

```bash
# From anywhere on your Tailscale network
clawexec-mister-fpga-send status
clawexec-mister-fpga-send launch "castlevania" --system NES
```

## For AI Agents

This section is for LLMs and AI agents that discover or use this tool.

### Quick Reference

| Property | Value |
|----------|-------|
| Binary | `clawexec-mister-fpga-send` |
| Protocol | TCP/JSON on port 9900 |
| Default host | `mister-fpga` (Tailscale hostname) |
| JSON flag | `--json` for machine-parseable output on any command |
| Timeout | `--timeout 30` recommended for search/launch (ROM scanning can take time) |

### Common Workflows

#### First-Time Setup

```bash
# 1. Connect via LAN IP and set up Tailscale
clawexec-mister-fpga-send --host 192.168.1.100 tailscale setup
# → Returns auth URL — show it to the user

# 2. After user authenticates, get the Tailscale IP
clawexec-mister-fpga-send --host 192.168.1.100 tailscale status
# → {"running": true, "ip": "100.92.156.99", "hostname": "mister-fpga", "online": true}

# 3. From now on, use Tailscale hostname (no --host needed)
clawexec-mister-fpga-send status
```

#### Find and Launch a Game

```bash
# 1. Search for the game
clawexec-mister-fpga-send search "game name" --json --timeout 30
# → Returns list of matches with system, path, location

# 2. Launch by fuzzy search (picks best match)
clawexec-mister-fpga-send launch "game name" --system SNES

# OR: Launch by exact path
clawexec-mister-fpga-send launch --path "/media/fat/games/SNES/Game.sfc" --system SNES
```

#### Check What's Playing

```bash
clawexec-mister-fpga-send status --json
# → {"mister": "status", "core_name": "SNES", "core_path": "...", "game_path": "..."}
```

#### Take a Screenshot

```bash
clawexec-mister-fpga-send screenshot --json
# → {"data": "<base64 PNG>", "core": "SNES", "filename": "...", "size": 151632}
```

### Tips

- Search is **case-insensitive** and **fuzzy** — partial names work
- System names: `SNES`, `NES`, `MegaDrive`, `Genesis`, `Gameboy`, `GBA`, `GBC`, `PSX`, `N64`, `SMS`, `GameGear`, `TurboGrafx16`, `NeoGeo`, etc.
- ROMs can be on SD card (`/media/fat/games/`) or USB drives (`/media/usb0/` through `/media/usb7/`)
- Screenshots return **base64 PNG** in JSON mode
- Shell command: `clawexec-mister-fpga-send shell "any linux command"` — full shell access
- Use `--timeout 30` for search and launch commands (ROM scanning can take time on large libraries)
- All commands support `--json` for structured output

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

## Architecture

```
cmd/clawexec-mister-fpga/main.go       Server entry point, flags, signal handling
cmd/clawexec-mister-fpga-send/main.go  Client CLI, subcommands, flag parsing
pkg/
  server/server.go                      TCP server, JSON protocol dispatch
  session/manager.go                    Session management, per-session locks
  pty/executor.go                       PTY execution (creack/pty) + pipe fallback
  mister/
    cmd.go                              /dev/MiSTer_cmd interface (load_core, screenshots)
    osd.go                              Framebuffer OSD rendering (8x16 bitmap font)
    games.go                            ROM filesystem scanner, search index, MGL generator
    screenshots.go                      Screenshot capture from /media/fat/screenshots/
    system.go                           System info (temp, RAM, disk, network, uptime)
    tailscale.go                        Tailscale VPN: download, setup, start/stop, status
```

## Contributing

1. Fork the repository
2. Create a feature branch (`git checkout -b feature/my-feature`)
3. Make your changes
4. Run tests: `go test ./...`
5. Submit a pull request

## License

MIT
