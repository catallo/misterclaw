# 👾🦀 MiSTerClaw — MCP Remote Control for MiSTer-FPGA

MiSTerClaw might be the first MCP server for MiSTer-FPGA. Control your MiSTer from any AI agent.

![Version](https://img.shields.io/badge/Version-v0.1.0-blue)
![Go](https://img.shields.io/badge/Go-1.21+-00ADD8?logo=go&logoColor=white)
![License](https://img.shields.io/badge/License-MIT-green)
![Platform](https://img.shields.io/badge/Platform-ARMv7_(DE10--Nano)-blue)

## What is this?

MiSTerClaw lets AI agents — Claude, ChatGPT, OpenClaw, Hermes, Cursor, and others — control a MiSTer-FPGA over the network. Launch games, search your ROM library, take screenshots, manage the system, and even set up Tailscale VPN for secure remote access from anywhere. It uses the [Model Context Protocol (MCP)](https://modelcontextprotocol.io) standard, so any MCP-compatible client works out of the box.

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

You: "Set up Tailscale so I can play from anywhere"
Agent: Installing Tailscale... Done!
       Please authenticate: https://login.tailscale.com/a/xxx
       After that your MiSTer is reachable from anywhere.
```

## CLI Client

For systems that don't support MCP, MiSTerClaw also includes a CLI client.

```bash
misterclaw-send -H mister-fpga search "sonic"
misterclaw-send -H mister-fpga launch "sonic 2" --system MegaDrive
misterclaw-send -H mister-fpga systems
misterclaw-send -H mister-fpga screenshot -o game.png
```

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

MiSTerClaw auto-detects all systems by scanning ROM folders, installed cores, and MGL files. No configuration needed — a well-stocked MiSTer typically has 70+ systems. Discovery runs in the background at server startup and results are cached in memory.

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
