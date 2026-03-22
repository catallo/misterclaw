# ClawExec for MiSTer-FPGA

Headless ClawExec server and CLI client for MiSTer FPGA (DE10-Nano, ARMv7).
Same TCP/JSON protocol as the Flutter GUI — existing `clawexec-send` client works unchanged.

## Repository
- **GitHub:** github.com/catallo/clawexec-mister (public, open-source)
- **License:** MIT

## Binaries

| Binary | Purpose | Target |
|--------|---------|--------|
| `clawexec-mister-fpga` | Server — runs on MiSTer-FPGA | `GOOS=linux GOARCH=arm GOARM=7` |
| `clawexec-mister-fpga-send` | Client CLI — runs anywhere | `GOOS=linux GOARCH=amd64`, `darwin/arm64`, etc. |

- **Server** runs on MiSTer-FPGA (ARM7, Buildroot Linux, 492 MB RAM), listens on TCP port 9900
- **Client** runs on any platform (Linux, macOS), connects to server via TCP

## Architecture

### Package Structure

```
cmd/clawexec-mister-fpga/main.go       — Server entry point, flags, signal handling
cmd/clawexec-mister-fpga-send/main.go  — Client CLI, subcommands, flag parsing
pkg/
  server/server.go                      — TCP server, JSON protocol (port 9900)
  session/manager.go                    — Session management, per-session locks, sequential execution
  pty/executor.go                       — PTY execution (creack/pty) + pipe fallback
  mister/
    cmd.go                              — /dev/MiSTer_cmd interface (load_core, screenshot trigger)
    osd.go                              — Framebuffer OSD rendering (8x16 bitmap font)
    games.go                            — ROM filesystem scanner, search index, MGL generator
    screenshots.go                      — Screenshot capture + read from /media/fat/screenshots/
    system.go                           — System info (temp, RAM, disk, network, uptime)
    tailscale.go                        — Tailscale VPN: download, setup, start/stop, status
```

## Implemented Features

### Client Commands (`clawexec-mister-fpga-send`)
- `launch` — Launch a game by fuzzy search or direct ROM path
- `search` — Search ROM library across all systems
- `systems` — List available systems and ROM counts
- `screenshot` — Take a screenshot (returns PNG, base64 or file)
- `info` — System information (temp, RAM, disk, uptime, IP)
- `status` — Show current core and game
- `tailscale setup|status|start|stop` — Tailscale VPN management
- `shell` — Execute arbitrary shell command on MiSTer-FPGA

### Server Features (`clawexec-mister-fpga`)
- TCP server with newline-delimited JSON protocol on port 9900
- Session management (parallel sessions, sequential per-session execution)
- PTY execution with pipe fallback
- Graceful shutdown on SIGINT/SIGTERM

## Protocol

Newline-delimited JSON over TCP (port 9900). **100% compatible with ClawExec GUI protocol.**

### Standard Commands (identical to ClawExec GUI)

```json
// Execute shell command
{"id": "uuid", "cmd": "ls -la", "session": "browse", "pty": true}
// Stream response
{"id": "uuid", "stream": "stdout", "data": "output..."}
// Completion
{"id": "uuid", "done": true, "exit_code": 0, "sessions": [...]}
// Input to running process
{"id": "uuid", "input": "some text"}
// Session management
{"list": true}
{"kill": true, "session": "name"}
{"close": true, "session": "name"}
// Resize PTY
{"session": "name", "resize": {"cols": 80, "rows": 24}}
```

### MiSTer-FPGA Commands

```json
// Load a core
{"mister": "load_core", "core": "SNES"}
{"mister": "load_core", "path": "/media/fat/_Console/SNES_20250705.rbf"}
// Search and launch games
{"mister": "search", "query": "sonic", "system": "Genesis"}
{"mister": "launch", "system": "Genesis", "path": "/media/fat/games/Genesis/Sonic.md"}
// Screenshot (trigger + return base64 PNG)
{"mister": "screenshot"}
// System info
{"mister": "info"}
// List available systems and ROM counts
{"mister": "systems"}
// Current core status
{"mister": "status"}
// Tailscale management
{"mister": "tailscale", "action": "setup"}
{"mister": "tailscale", "action": "status"}
{"mister": "tailscale", "action": "start"}
{"mister": "tailscale", "action": "stop"}
```

## Tailscale Integration

- Automatic download and installation of Tailscale ARM binaries
- Userspace networking (`--tun=userspace-networking`) — no kernel module needed
- Auth URL returned for browser-based login, with polling for completion
- Autostart via `/media/fat/linux/user-startup.sh`
- Start/stop/status management

## MiSTer Interfaces Used

| Interface | Purpose | Method |
|-----------|---------|--------|
| /dev/MiSTer_cmd | Load cores, trigger screenshots | Named pipe (FIFO write) |
| /media/fat/*.rbf | Core files | Filesystem scan |
| /media/fat/_*/ | Core categories | Filesystem scan |
| /media/fat/games/ | ROM files | Filesystem scan + index |
| /media/fat/screenshots/ | Screenshot files | File read |
| /proc/*, sysfs | System info | OS reads |

## Build & Deploy

```bash
# Build server (for MiSTer-FPGA, ARM7)
GOOS=linux GOARCH=arm GOARM=7 go build -ldflags="-s -w" -o clawexec-mister-fpga ./cmd/clawexec-mister-fpga/

# Build client (for local machine, e.g. Linux amd64)
GOOS=linux GOARCH=amd64 go build -ldflags="-s -w" -o clawexec-mister-fpga-send ./cmd/clawexec-mister-fpga-send/

# Deploy server to MiSTer
scp clawexec-mister-fpga root@10.0.0.8:/media/fat/Scripts/

# Run server on MiSTer
/media/fat/Scripts/clawexec-mister-fpga --port 9900

# Autostart: add to /media/fat/linux/user-startup.sh
# /media/fat/Scripts/clawexec-mister-fpga --port 9900 --daemon &
```

## Cross-compilation Targets
- Server: `GOOS=linux GOARCH=arm GOARM=7` (MiSTer-FPGA DE10-Nano)
- Client: `GOOS=linux GOARCH=amd64`, `GOOS=darwin GOARCH=arm64`, etc.
- Expected server binary size: 3-5 MB (stripped), ~2 MB with UPX

## Dependencies
- Go stdlib
- github.com/creack/pty (PTY allocation)
- github.com/google/uuid (request IDs)
- Zero runtime dependencies on MiSTer

## Phase 2/3 TODO (Open)
- /dev/uinput keyboard/controller simulation
- Game history / playtime tracking
- Savestate management
- Auto-discovery (mDNS)
- Event callbacks (core/game change notifications)
- INI management (profiles, video_mode)
- Core discovery + listing (.rbf enumeration)

## Testing
- Unit tests for all packages
- Integration tests with mock /dev/MiSTer_cmd (tmpfile FIFO)
- Cross-compile verification in CI
- **NEVER test against production — use mock interfaces**

## Design Principles
- **Universal compatibility** — must work on ANY MiSTer, not just one specific setup
- **No hardcoded paths** — ROMs can be on SD (/media/fat/games/) or any USB drive (/media/usb0-7/)
- **Case-insensitive matching** — folder names vary across setups (GameBoy vs Gameboy vs GB)
- **Dynamic core detection** — scan for .rbf files, dont hardcode date suffixes
- **Graceful degradation** — clear errors when cores/ROMs missing, never crash
- **Zero config** — works out of the box with no config file needed
