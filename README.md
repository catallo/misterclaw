# 👾🦀 MiSTerClaw — MCP Remote Control for MiSTer-FPGA

MiSTerClaw is the first MCP server for MiSTer-FPGA. Control your MiSTer from any AI agent.

![Version](https://img.shields.io/badge/Version-v0.7.0-blue)
![Go](https://img.shields.io/badge/Go-1.21+-00ADD8?logo=go&logoColor=white)
![License](https://img.shields.io/badge/License-MIT-green)
![Platform](https://img.shields.io/badge/Platform-ARMv7_(DE10--Nano)-blue)

## What is this?

MiSTerClaw lets AI agents — Claude, ChatGPT, OpenClaw, Hermes, Cursor, and others — control a MiSTer-FPGA over the network. Launch games, search your ROM library, take screenshots, read and modify core settings and DIP switches, navigate the OSD menu using conf_str-based position calculation (experimental, not yet reliable for all cores), query detailed system information, manage the system, and even set up Tailscale VPN for secure remote access from anywhere. Floppy-disk cores (PC8801, MSX, etc.) support PostLaunch auto-reset for seamless game loading. It uses the [Model Context Protocol (MCP)](https://modelcontextprotocol.io) standard, so any MCP-compatible client works out of the box. For agents without MCP support, a CLI client is also included.

## MCP Setup

Add MiSTerClaw to your MCP client config:

**Claude Desktop** (`claude_desktop_config.json`):
```json
{
  "mcpServers": {
    "misterclaw": {
      "command": "/path/to/misterclaw-mcp",
      "args": ["--host", "mister-fpga"]
    }
  }
}
```

**Cursor** (`.cursor/mcp.json`):
```json
{
  "mcpServers": {
    "misterclaw": {
      "command": "/path/to/misterclaw-mcp",
      "args": ["--host", "mister-fpga"]
    }
  }
}
```

### MCP Tools

| Tool | Description |
|------|-------------|
| `mister_status` | What's currently playing |
| `mister_launch` | Launch a game by search or path |
| `mister_search` | Search the ROM library |
| `mister_systems` | List all available systems with ROM counts |
| `mister_screenshot` | Take a screenshot (returns PNG image) |
| `mister_info` | System info (hostname, IP, temp, RAM, disks, uptime) |
| `mister_input` | Send keyboard input (OSD navigation, named keys) |
| `mister_osd_info` | Query OSD menu structure from CONF_STR database |
| `mister_osd_visible` | Show only visible OSD menu items based on current config |
| `mister_cfg_read` | Read core settings (options + DIP switches) |
| `mister_cfg_write` | Modify core settings with automatic backup |
| `mister_reload` | Reload current core to apply config changes |
| `mister_osd_navigate` | Navigate to a specific OSD menu item by name |
| `mister_system_info` | Get system config, notes, and OSD menu for a system |
| `mister_rescan` | Rescan ROM library (optionally for specific drive) |
| `mister_tailscale` | Manage Tailscale VPN |
| `mister_shell` | Execute shell commands |

## Example Conversations

Once configured, just talk to your agent:

```
You: "What games do I have for SNES?"
Agent: You have 13,435 SNES ROMs! Here are some highlights...

You: "Launch Street Fighter 2"
Agent: 🎮 Launched Street Fighter II Turbo on SNES!

You: "Take a screenshot and show me"
Agent: [screenshot of the game running]

You: "What systems do I have?"
Agent: Your MiSTer has 70+ systems! Top ones:
  - SNES: 13,435 ROMs
  - C64: 46,736 ROMs
  - Amiga: 55,793 ROMs
  ...

You: "Set up Tailscale so I can reach the MiSTer from anywhere"
Agent: Installing Tailscale... Done!
       Please authenticate: https://login.tailscale.com/a/xxx
       After that your MiSTer is reachable from anywhere.

You: "What settings does R-Type have?"
Agent: R-Type (M72 core) has these settings:
       Core Options:
         - No Death Mode: Off
         - Service Mode: Off
       DIP Switches:
         - Difficulty: Medium
         - Coin A: 1 Coin / 1 Credit
         - Lives: 3
         - Demo Sounds: On
         - Continue: Yes

You: "Enable No Death Mode"
Agent: ⚙️ Set "No Death Mode" to On in R-Type.
       The core reloads automatically — change is live!

You: "Navigate to the Reset option in the OSD"
Agent: Navigated to Reset in the SNES core OSD.

You: "What do I need to know about the PC8801 system?"
Agent: PC8801 uses the PC8801 core with .d88 floppy disk images.
       Notes: After loading a disk, press F12 → Reset to boot.
       OSD Menu: FDD0, FDD1, Reset, CPU Speed, ...
```

## CLI Client

For agents that have shell access (like OpenClaw, Codex, or Claude Code), the CLI client can be more efficient than MCP — no tool schemas in context, just call it when needed. It's also useful for shell scripts and automation.

```bash
# Check what's playing
misterclaw-send -H mister-fpga status

# Search for games
misterclaw-send -H mister-fpga search "zelda" --limit 5
misterclaw-send -H mister-fpga search "sonic" --system MegaDrive

# Launch a game
misterclaw-send -H mister-fpga launch "super mario world" --system SNES

# List all systems with ROM counts
misterclaw-send -H mister-fpga systems

# Take a screenshot
misterclaw-send -H mister-fpga screenshot -o game.png

# System info (CPU, memory, storage, uptime)
misterclaw-send -H mister-fpga info

# Run shell commands — configure MiSTer.ini, run updates, manage files
misterclaw-send -H mister-fpga shell "cat /media/fat/MiSTer.ini"
misterclaw-send -H mister-fpga shell "/media/fat/Scripts/update_all.sh"

# Send keyboard input (OSD navigation)
misterclaw-send -H mister-fpga input key osd
misterclaw-send -H mister-fpga input combo leftalt f12

# Query OSD menu structure
misterclaw-send -H mister-fpga osd-info
misterclaw-send -H mister-fpga osd-info --core SNES

# Navigate to a specific OSD menu item
misterclaw-send -H mister-fpga osd-navigate Reset
misterclaw-send -H mister-fpga osd-navigate "Aspect ratio"

# Get detailed system info (config, notes, OSD menu)
misterclaw-send -H mister-fpga system-info PC8801
misterclaw-send -H mister-fpga system-info SNES

# Read core settings (options + DIP switches)
misterclaw-send -H mister-fpga cfg-read

# Modify settings
misterclaw-send -H mister-fpga cfg-write --option "Free Play" --value On

# Reload core to apply changes
misterclaw-send -H mister-fpga reload

# Rescan ROM library (after adding new games)
misterclaw-send -H mister-fpga rescan
misterclaw-send -H mister-fpga rescan --location usb0

# JSON output for scripting
misterclaw-send -H mister-fpga status --json
misterclaw-send -H mister-fpga systems --json
```

All commands support `--json` for machine-readable output.

## Installation

Three binaries:

| Binary | Runs on | Purpose |
|--------|---------|---------|
| `misterclaw` | MiSTer (ARM7) | Server — runs on the MiSTer itself |
| `misterclaw-send` | Anywhere | CLI client for direct commands |
| `misterclaw-mcp` | Your machine | MCP server — bridges AI agents to MiSTer |

### Quick install on MiSTer

```bash
scp misterclaw root@<mister-ip>:/media/fat/Scripts/
ssh root@<mister-ip> "/media/fat/Scripts/misterclaw --install"
```

Or self-install: copy the binary to MiSTer, then run `misterclaw --install`. This copies to `/media/fat/Scripts/`, configures autostart on boot, and is idempotent.

To uninstall: `/media/fat/Scripts/misterclaw --uninstall`

## Security

> ⚠️ **Do not expose port 9900 to the internet.** The ClawExec protocol has no authentication or encryption.

- **Tailscale VPN** (recommended) — encrypted, authenticated, works from anywhere
- **Local network only** — keep port 9900 accessible within your LAN
- **Never** forward port 9900 through your router or firewall

## Architecture

```
Agent ←MCP→ misterclaw-mcp ←TCP→ MiSTer:9900 (misterclaw server)
Agent ←CLI→ misterclaw-send ←TCP→ MiSTer:9900 (misterclaw server)
```

The MCP server and CLI client communicate with the MiSTer over the ClawExec protocol (newline-delimited JSON over TCP, port 9900).

## Dynamic System Detection

MiSTerClaw auto-detects all systems by scanning ROM folders, installed cores, and MGL files. No configuration needed — a well-stocked MiSTer typically has 70+ systems. Discovery results (including full ROM file listings) are cached to disk. First startup scans and builds the cache; subsequent starts load instantly. Use the `rescan` command after adding new games or drives.

## Building from Source

```bash
# MiSTer server (ARM7)
GOOS=linux GOARCH=arm GOARM=7 go build -ldflags="-s -w" -o misterclaw ./cmd/misterclaw/

# CLI client
go build -ldflags="-s -w" -o misterclaw-send ./cmd/misterclaw-send/

# MCP server
go build -ldflags="-s -w" -o misterclaw-mcp ./cmd/misterclaw-mcp/
```

## License

MIT
