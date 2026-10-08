package server

import (
	"bufio"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"log"
	"net"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/catallo/misterclaw/pkg/mister"
	"github.com/catallo/misterclaw/pkg/session"
	"github.com/google/uuid"
)

// Request represents an incoming JSON command.
type Request struct {
	// Command execution
	ID      string `json:"id,omitempty"`
	Cmd     string `json:"cmd,omitempty"`
	Session string `json:"session,omitempty"`
	Pty     *bool  `json:"pty,omitempty"`
	Agent   string `json:"agent,omitempty"`

	// Input forwarding
	Input string `json:"input,omitempty"`

	// Session management
	List  *bool `json:"list,omitempty"`
	Kill  *bool `json:"kill,omitempty"`
	Close *bool `json:"close,omitempty"`
	Drain *bool `json:"drain,omitempty"` // cancel queued cmds + kill running (session-kill)

	// File transfer (push = host->MiSTer, pull = MiSTer->host). Chunked
	// base64 over this line protocol; SHA256 verified, atomic-renamed.
	Push     *bool  `json:"push,omitempty"`      // begin push (with Path, Size, Sha256)
	PushData string `json:"push_data,omitempty"` // one base64 chunk
	PushDone *bool  `json:"push_done,omitempty"` // finish push: verify + rename
	Pull     *bool  `json:"pull,omitempty"`      // pull Path to host (server streams chunks)
	Size     int64  `json:"size,omitempty"`      // expected byte count (push)
	Sha256   string `json:"sha256,omitempty"`    // expected hex digest (push)

	// PTY resize
	Resize *ResizeRequest `json:"resize,omitempty"`

	// MiSTer commands
	MiSTer   string   `json:"mister,omitempty"`
	Core     string   `json:"core,omitempty"`
	Path     string   `json:"path,omitempty"`
	Query    string   `json:"query,omitempty"`
	System   string   `json:"system,omitempty"`
	Action   string   `json:"action,omitempty"`
	URL      string   `json:"url,omitempty"`
	Hostname string   `json:"hostname,omitempty"`
	Key      string   `json:"key,omitempty"`
	Raw      *int     `json:"raw,omitempty"`
	Combo    []string `json:"combo,omitempty"`
	Device   string   `json:"device,omitempty"`
	Button   string   `json:"button,omitempty"`
	DPad     string   `json:"dpad,omitempty"`
	Text     string   `json:"text,omitempty"`

	// OSD navigation
	Target string `json:"target,omitempty"`

	// CFG commands
	Option   string `json:"option,omitempty"`
	Value    string `json:"value,omitempty"`
	Location string `json:"location,omitempty"`
}

// ResizeRequest holds PTY dimensions.
type ResizeRequest struct {
	Cols int `json:"cols"`
	Rows int `json:"rows"`
}

// Server is a TCP server handling the MisterClaw JSON protocol.
type Server struct {
	listener            net.Listener
	manager             *session.Manager
	clients             map[net.Conn]struct{}
	mu                  sync.Mutex
	launchGame          func(mister.GameInfo) error // per-server injection for hardware-free tests
	startLocationRescan func(string) bool
	startFullRescan     func() bool
}

// New creates a new Server.
func New(manager *session.Manager) *Server {
	return &Server{
		manager:             manager,
		clients:             make(map[net.Conn]struct{}),
		launchGame:          mister.LaunchGame,
		startLocationRescan: mister.StartRescanLocation,
		startFullRescan:     mister.StartFullRescan,
	}
}

// ListenAndServe starts the TCP server.
func (s *Server) ListenAndServe(addr string) error {
	ln, err := net.Listen("tcp", addr)
	if err != nil {
		return err
	}
	s.listener = ln
	log.Printf("listening on %s", addr)

	for {
		conn, err := ln.Accept()
		if err != nil {
			// Listener was closed during shutdown
			select {
			default:
				return err
			}
		}
		s.mu.Lock()
		s.clients[conn] = struct{}{}
		s.mu.Unlock()

		log.Printf("client connected: %s", conn.RemoteAddr())
		go s.handleConn(conn)
	}
}

// Close shuts down the server and all client connections.
func (s *Server) Close() {
	if s.listener != nil {
		s.listener.Close()
	}
	s.mu.Lock()
	for conn := range s.clients {
		conn.Close()
	}
	s.mu.Unlock()
}

func (s *Server) handleConn(conn net.Conn) {
	// The lifetime belongs to the accepted connection, not a client-chosen
	// session/agent/request ID. Reconnects can safely reuse the same name.
	owner := session.NewOwner()

	// In-progress host->device push for this connection (task #36). Cleaned
	// up on disconnect so an interrupted transfer leaves no stray temp file.
	var push *pushState

	defer func() {
		conn.Close()
		push.cleanup()
		s.mu.Lock()
		delete(s.clients, conn)
		s.mu.Unlock()

		owner.Close() // cancel only this connection's active and queued jobs

		log.Printf("client disconnected: %s", conn.RemoteAddr())
	}()

	// connMu serializes writes to this connection
	var connMu sync.Mutex
	send := func(v interface{}) {
		data, err := json.Marshal(v)
		if err != nil {
			return
		}
		connMu.Lock()
		conn.Write(append(data, '\n'))
		connMu.Unlock()
	}

	scanner := bufio.NewScanner(conn)
	// Allow large lines (1MB) for base64 data etc.
	scanner.Buffer(make([]byte, 64*1024), 1024*1024)

	for scanner.Scan() {
		line := scanner.Bytes()
		if len(line) == 0 {
			continue
		}

		var req Request
		if err := json.Unmarshal(line, &req); err != nil {
			send(map[string]interface{}{
				"error": fmt.Sprintf("invalid JSON: %v", err),
			})
			continue
		}

		// File-transfer messages carry per-connection push state, so they
		// are handled here rather than in the stateless dispatch (task #36).
		switch {
		case req.Push != nil && *req.Push:
			push.cleanup() // abandon any prior incomplete push on this conn
			push = beginPush(req, send)
			continue
		case req.PushData != "":
			push.writeChunk(req.PushData)
			continue
		case req.PushDone != nil && *req.PushDone:
			push.finish(send)
			push = nil
			continue
		case req.Pull != nil && *req.Pull:
			s.handlePull(req, send)
			continue
		}

		s.dispatch(req, send, owner)
	}
}

func (s *Server) dispatch(req Request, send func(interface{}), owner *session.Owner) {
	switch {
	case req.List != nil && *req.List:
		s.handleList(send)

	case req.Kill != nil && *req.Kill:
		s.handleKill(req, send)

	case req.Close != nil && *req.Close:
		s.handleClose(req, send)

	case req.Drain != nil && *req.Drain:
		s.handleDrain(req, send)

	case req.Resize != nil:
		s.handleResize(req, send)

	case req.Input != "":
		s.handleInput(req, send)

	case req.MiSTer != "":
		s.handleMiSTer(req, send)

	case req.Cmd != "":
		s.handleCmd(req, send, owner)

	default:
		send(map[string]interface{}{
			"error": "unrecognized command",
		})
	}
}

func (s *Server) handleList(send func(interface{})) {
	sessions := s.manager.List()
	send(map[string]interface{}{
		"list":     true,
		"sessions": sessions,
		"total":    len(sessions),
	})
}

func (s *Server) handleKill(req Request, send func(interface{})) {
	success := s.manager.Kill(req.Session)
	send(map[string]interface{}{
		"kill":    true,
		"session": req.Session,
		"success": success,
	})
}

func (s *Server) handleClose(req Request, send func(interface{})) {
	success := s.manager.Close(req.Session)
	send(map[string]interface{}{
		"close":   true,
		"session": req.Session,
		"success": success, // shutdown request accepted, not a synchronous join
		"closing": success,
	})
}

// handleDrain cancels a session's queued commands and kills the running one
// (session-kill). Unlike Kill (running process only), this flushes the whole
// backlog — the manual recovery for a runaway queue (task #18).
func (s *Server) handleDrain(req Request, send func(interface{})) {
	if req.Session == "" {
		send(map[string]interface{}{"error": "drain requires session"})
		return
	}
	cancelled := s.manager.Drain(req.Session)
	if cancelled < 0 {
		send(map[string]interface{}{
			"drain":   true,
			"session": req.Session,
			"success": false,
			"error":   fmt.Sprintf("session %q not found", req.Session),
		})
		return
	}
	send(map[string]interface{}{
		"drain":     true,
		"session":   req.Session,
		"success":   true,
		"cancelled": cancelled,
	})
}

func (s *Server) handleResize(req Request, send func(interface{})) {
	if req.Session == "" {
		send(map[string]interface{}{"error": "resize requires session"})
		return
	}
	err := s.manager.Resize(req.Session, uint16(req.Resize.Cols), uint16(req.Resize.Rows))
	if err != nil {
		send(map[string]interface{}{"error": err.Error()})
		return
	}
	send(map[string]interface{}{"resized": true, "session": req.Session})
}

func (s *Server) handleInput(req Request, send func(interface{})) {
	sessionName := req.Session
	if sessionName == "" && req.ID != "" {
		// Input can target by ID — but we route by session name
		send(map[string]interface{}{"error": "input requires session"})
		return
	}
	err := s.manager.WriteInput(sessionName, []byte(req.Input))
	if err != nil {
		send(map[string]interface{}{"error": err.Error()})
	}
}

func (s *Server) handleCmd(req Request, send func(interface{}), owner *session.Owner) {
	sessionName := req.Session
	if sessionName == "" {
		sessionName = "default"
	}

	id := req.ID
	if id == "" {
		id = uuid.New().String()
	}

	usePty := true
	if req.Pty != nil {
		usePty = *req.Pty
	}

	outputCb := func(data []byte) {
		send(map[string]interface{}{
			"id":     id,
			"stream": "stdout",
			"data":   sanitizeOutput(string(data)),
		})
	}

	doneCb := func(result session.Result) {
		response := map[string]interface{}{
			"id": id, "done": true, "exit_code": result.ExitCode,
			"sessions": s.manager.List(),
		}
		if result.Err != nil {
			response["error"] = result.Err.Error()
		}
		send(response)
	}

	_ = s.manager.Submit(owner, sessionName, req.Cmd, usePty, req.Agent, outputCb, doneCb)
}

func (s *Server) handleMiSTer(req Request, send func(interface{})) {
	switch req.MiSTer {
	case "load_core":
		path := req.Path
		if path == "" && req.Core != "" {
			path = req.Core
		}
		if path == "" {
			send(map[string]interface{}{"error": "load_core requires path or core"})
			return
		}
		status, err := mister.LoadCoreVerified(path, 5*time.Second)
		if err != nil {
			send(map[string]interface{}{
				"mister":  "load_core",
				"success": false,
				"error":   err.Error(),
			})
			return
		}
		send(map[string]interface{}{
			"mister":    "load_core",
			"success":   true,
			"core_name": status.CoreName,
			"core_path": status.CorePath,
			"game_path": status.GamePath,
		})

	case "status":
		status, err := mister.GetRunningCore()
		if err != nil {
			send(map[string]interface{}{"error": err.Error()})
			return
		}
		send(map[string]interface{}{
			"mister":    "status",
			"core_name": status.CoreName,
			"core_path": status.CorePath,
			"game_path": status.GamePath,
		})

	case "screenshot":
		result, err := mister.TakeScreenshotAndCapture(5 * time.Second)
		if err != nil {
			send(map[string]interface{}{"error": err.Error()})
			return
		}
		send(map[string]interface{}{
			"mister":   "screenshot",
			"success":  true,
			"data":     result.Data,
			"core":     result.CoreName,
			"filename": result.FileName,
			"size":     result.SizeBytes,
		})

	case "info":
		info := mister.GetSystemInfo()
		send(info)

	case "systems":
		stats := mister.GetSystemStats()
		if stats == nil && !mister.IsDiscoveryReady() {
			send(map[string]interface{}{
				"mister":  "systems",
				"status":  "pending",
				"message": "System discovery is in progress. This may take several minutes on large ROM collections. Try again shortly.",
			})
			return
		}
		complete := mister.IsDiscoveryComplete()
		send(map[string]interface{}{
			"mister":   "systems",
			"systems":  stats,
			"complete": complete,
			"scanning": !complete,
		})

	case "search":
		if !mister.IsGamesReady() {
			send(map[string]interface{}{
				"mister":  "search",
				"status":  "pending",
				"message": "Game library scan is in progress. This may take several minutes on large ROM collections. Results will be available once the scan completes.",
			})
			return
		}
		results := mister.SearchGames(req.Query, req.System)
		send(map[string]interface{}{
			"mister":  "search",
			"results": results,
			"total":   len(results),
		})

	case "launch":
		var game *mister.GameInfo
		if req.Path != "" && req.System != "" {
			base := filepath.Base(req.Path)
			name := strings.TrimSuffix(base, filepath.Ext(base))
			game = &mister.GameInfo{
				Name:   name,
				Path:   req.Path,
				System: req.System,
			}
		} else if req.Query != "" {
			// Launch by search (first match)
			results := mister.SearchGames(req.Query, req.System)
			if len(results) > 0 {
				game = &results[0]
			}
		}

		if game == nil {
			if !mister.IsGamesReady() {
				send(map[string]interface{}{
					"mister":  "launch",
					"success": false,
					"status":  "pending",
					"message": "Game library scan is in progress. The game may exist but hasn't been indexed yet. This may take several minutes on large ROM collections.",
				})
				return
			}
			send(map[string]interface{}{
				"mister":  "launch",
				"success": false,
				"error":   "no game found or missing parameters",
			})
			return
		}

		err := s.launchGame(*game)
		if err != nil {
			send(map[string]interface{}{
				"mister":  "launch",
				"success": false,
				"error":   err.Error(),
			})
			return
		}
		send(map[string]interface{}{
			"mister":    "launch",
			"success":   true,
			"game":      game.Name,
			"core_name": mister.LaunchCoreName(*game),
		})

	case "input":
		switch {
		case req.Button != "":
			// "button" field is shorthand for gamepad
			if err := mister.PressGamepadButton(req.Button); err != nil {
				send(map[string]interface{}{
					"mister":  "input",
					"success": false,
					"error":   err.Error(),
				})
				return
			}
			send(map[string]interface{}{
				"mister":  "input",
				"success": true,
				"button":  req.Button,
				"device":  "gamepad",
			})
		case req.DPad != "":
			// "dpad" field uses gamepad (explicit or default)
			if err := mister.GamepadDPad(req.DPad); err != nil {
				send(map[string]interface{}{
					"mister":  "input",
					"success": false,
					"error":   err.Error(),
				})
				return
			}
			send(map[string]interface{}{
				"mister":  "input",
				"success": true,
				"dpad":    req.DPad,
				"device":  "gamepad",
			})
		case req.Key != "":
			if req.Device == "gamepad" {
				if err := mister.PressGamepadButton(req.Key); err != nil {
					send(map[string]interface{}{
						"mister":  "input",
						"success": false,
						"error":   err.Error(),
					})
					return
				}
				send(map[string]interface{}{
					"mister":  "input",
					"success": true,
					"key":     req.Key,
					"device":  "gamepad",
				})
			} else {
				if err := mister.PressKey(req.Key); err != nil {
					send(map[string]interface{}{
						"mister":  "input",
						"success": false,
						"error":   err.Error(),
					})
					return
				}
				send(map[string]interface{}{
					"mister":  "input",
					"success": true,
					"key":     req.Key,
				})
			}
		case req.Raw != nil:
			if err := mister.PressRawKey(*req.Raw); err != nil {
				send(map[string]interface{}{
					"mister":  "input",
					"success": false,
					"error":   err.Error(),
				})
				return
			}
			send(map[string]interface{}{
				"mister":  "input",
				"success": true,
				"raw":     *req.Raw,
			})
		case len(req.Combo) > 0:
			if err := mister.PressCombo(req.Combo); err != nil {
				send(map[string]interface{}{
					"mister":  "input",
					"success": false,
					"error":   err.Error(),
				})
				return
			}
			send(map[string]interface{}{
				"mister":  "input",
				"success": true,
				"combo":   req.Combo,
			})
		case req.Text != "":
			if err := mister.TypeText(req.Text); err != nil {
				send(map[string]interface{}{
					"mister":  "input",
					"success": false,
					"error":   err.Error(),
				})
				return
			}
			send(map[string]interface{}{
				"mister":  "input",
				"success": true,
				"text":    req.Text,
			})
		default:
			send(map[string]interface{}{
				"error": "input requires key, raw, combo, button, dpad, or text parameter",
			})
		}

	case "tailscale":
		switch req.Action {
		case "setup":
			authURL, err := mister.TailscaleSetup(req.URL, req.Hostname)
			if err != nil {
				send(map[string]interface{}{
					"mister":  "tailscale",
					"action":  "setup",
					"success": false,
					"error":   err.Error(),
				})
				return
			}
			resp := map[string]interface{}{
				"mister":  "tailscale",
				"action":  "setup",
				"success": true,
			}
			if authURL != "" {
				resp["auth_url"] = authURL
			} else {
				// Already authenticated — include IP
				status, err := mister.TailscaleGetStatus()
				if err == nil && status.IP != "" {
					resp["ip"] = status.IP
				}
			}
			send(resp)

		case "status":
			status, err := mister.TailscaleGetStatus()
			if err != nil {
				send(map[string]interface{}{"error": err.Error()})
				return
			}
			send(status)

		case "start":
			err := mister.TailscaleStart()
			if err != nil {
				send(map[string]interface{}{
					"mister":  "tailscale",
					"action":  "start",
					"success": false,
					"error":   err.Error(),
				})
				return
			}
			send(map[string]interface{}{
				"mister":  "tailscale",
				"action":  "start",
				"success": true,
			})

		case "stop":
			err := mister.TailscaleStop()
			if err != nil {
				send(map[string]interface{}{
					"mister":  "tailscale",
					"action":  "stop",
					"success": false,
					"error":   err.Error(),
				})
				return
			}
			send(map[string]interface{}{
				"mister":  "tailscale",
				"action":  "stop",
				"success": true,
			})

		default:
			send(map[string]interface{}{
				"error": fmt.Sprintf("unknown tailscale action: %s", req.Action),
			})
		}

	case "osd_info":
		coreName := req.Core
		if coreName == "" {
			// Use currently running core
			status, err := mister.GetRunningCore()
			if err != nil {
				send(map[string]interface{}{"error": "no core specified and " + err.Error()})
				return
			}
			coreName = mister.StripCoreDateSuffix(status.CoreName)
		}

		db, err := mister.GetConfStrDB()
		if err != nil {
			send(map[string]interface{}{
				"mister":  "osd_info",
				"success": false,
				"error":   fmt.Sprintf("confstr database not available: %v", err),
			})
			return
		}

		osd := mister.LookupCoreOSD(db, coreName)
		if osd == nil {
			send(map[string]interface{}{
				"mister":  "osd_info",
				"success": false,
				"error":   fmt.Sprintf("no OSD info found for core: %s", coreName),
			})
			return
		}

		send(map[string]interface{}{
			"mister":       "osd_info",
			"success":      true,
			"core_name":    osd.CoreName,
			"repo":         osd.Repo,
			"conf_str_raw": osd.ConfStrRaw,
			"menu":         osd.Menu,
		})

	case "osd_visible":
		s.handleOSDVisible(req, send)

	case "cfg_read":
		s.handleCFGRead(req, send)

	case "cfg_write":
		s.handleCFGWrite(req, send)

	case "reload":
		s.handleReload(req, send)

	case "rescan":
		s.handleRescan(req, send)

	case "osd_navigate":
		target := req.Target
		if target == "" {
			send(map[string]interface{}{"error": "osd_navigate requires target"})
			return
		}
		coreName := req.Core
		if coreName == "" {
			status, err := mister.GetRunningCore()
			if err != nil {
				send(map[string]interface{}{"error": "no core specified and " + err.Error()})
				return
			}
			coreName = mister.StripCoreDateSuffix(status.CoreName)
		}
		if err := mister.OSDNavigateTo(coreName, target); err != nil {
			send(map[string]interface{}{
				"mister":  "osd_navigate",
				"success": false,
				"error":   err.Error(),
			})
			return
		}
		send(map[string]interface{}{
			"mister":  "osd_navigate",
			"success": true,
			"target":  target,
			"core":    coreName,
		})

	case "system_info":
		system := req.System
		if system == "" {
			send(map[string]interface{}{"error": "system_info requires system"})
			return
		}
		cfg, ok := mister.GetSystemConfig(system)
		if !ok {
			send(map[string]interface{}{
				"mister":  "system_info",
				"success": false,
				"error":   fmt.Sprintf("unknown system: %s", system),
			})
			return
		}
		resp := map[string]interface{}{
			"mister":  "system_info",
			"success": true,
			"system":  system,
			"config":  cfg,
		}
		if cfg.PostLaunch != nil && cfg.PostLaunch.Notes != "" {
			resp["notes"] = cfg.PostLaunch.Notes
		}
		db, err := mister.GetConfStrDB()
		if err == nil {
			coreName := filepath.Base(cfg.Core)
			osd := mister.LookupCoreOSD(db, coreName)
			if osd != nil {
				resp["core_name"] = osd.CoreName
				resp["menu"] = osd.Menu
			}
		}
		send(resp)

	default:
		send(map[string]interface{}{
			"error": fmt.Sprintf("unknown mister command: %s", req.MiSTer),
		})
	}
}

// coreContext holds all resolved state for the current core: OSD info, CFG data, DIP data, paths.

// reloadCurrentCore reloads the currently running core/game so config changes take effect.
func (s *Server) handleReload(req Request, send func(interface{})) {
	status, err := mister.GetRunningCore()
	if err != nil || status == nil {
		send(map[string]interface{}{"mister": "reload", "success": false, "error": "no core running"})
		return
	}
	path := status.GamePath
	if path == "" {
		path = status.CorePath
	}
	if path == "" {
		send(map[string]interface{}{"mister": "reload", "success": false, "error": "no core path found"})
		return
	}
	mister.LoadCore(path)
	send(map[string]interface{}{"mister": "reload", "success": true, "path": path})
}

func (s *Server) handleRescan(req Request, send func(interface{})) {
	location := req.Location
	if location == "" {
		if !s.startFullRescan() {
			send(map[string]interface{}{"mister": "rescan", "success": false, "error": "discovery is already running; wait for completion"})
			return
		}
		// Respond immediately — scan runs in background
		send(map[string]interface{}{
			"mister":   "rescan",
			"success":  true,
			"message":  "Rescan started in background. Discovery may take several minutes on large ROM collections. Use 'systems' to check progress.",
			"location": "all",
		})
		return
	}

	if !s.startLocationRescan(location) {
		send(map[string]interface{}{"mister": "rescan", "success": false, "error": "invalid location, discovery already running, or initial cache not ready"})
		return
	}
	send(map[string]interface{}{
		"mister":   "rescan",
		"success":  true,
		"status":   "pending",
		"message":  "Location rescan started in background. Use 'systems' to check the scanning flag; existing results remain available until completion.",
		"location": location,
	})
}

type coreContext struct {
	OSD     *mister.CoreOSD
	CFGData []byte
	CFGPath string
	MRAPath string // empty if not arcade
	MRA     *mister.MRA
	DIPData []byte // nil if no MRA/DIP switches
	DIPPath string // empty if no MRA
}

// resolveCore resolves the current core's OSD info, CFG file, and DIP file.
func (s *Server) resolveCore(req Request, send func(interface{})) (*coreContext, bool) {
	coreName := req.Core
	cfgName := ""
	mraPath := ""
	var mra *mister.MRA
	if coreName == "" {
		status, err := mister.GetRunningCore()
		if err != nil {
			send(map[string]interface{}{"error": "no core specified and " + err.Error()})
			return nil, false
		}
		coreName = mister.StripCoreDateSuffix(status.CoreName)
		// CFG name comes from the game (MRA), not the core
		if status.GamePath != "" {
			if strings.HasSuffix(strings.ToLower(status.GamePath), ".mra") {
				mraPath = status.GamePath
				if parsed, err := mister.ParseMRA(status.GamePath); err == nil {
					mra = parsed
					if mra.SetName != "" {
						cfgName = mra.SetName
					}
				}
			}
			// Fallback: use MRA filename without extension
			if cfgName == "" {
				base := filepath.Base(status.GamePath)
				cfgName = strings.TrimSuffix(base, filepath.Ext(base))
			}
		} else {
			cfgName = coreName
		}
	} else {
		cfgName = coreName
	}

	db, err := mister.GetConfStrDB()
	if err != nil {
		send(map[string]interface{}{"error": fmt.Sprintf("confstr database not available: %v", err)})
		return nil, false
	}

	osd := mister.LookupCoreOSD(db, coreName)
	if osd == nil {
		send(map[string]interface{}{"error": fmt.Sprintf("no OSD info found for core: %s", coreName)})
		return nil, false
	}

	cfgPath := mister.CFGPath(cfgName)
	cfgData, err := mister.ReadCFG(cfgPath)
	if err != nil {
		// If no CFG exists, use all zeros (default state)
		cfgData = make([]byte, 16)
	}

	ctx := &coreContext{
		OSD:     osd,
		CFGData: cfgData,
		CFGPath: cfgPath,
		MRAPath: mraPath,
		MRA:     mra,
	}

	// Load DIP data if this is an arcade game with an MRA
	if mra != nil && mraPath != "" {
		dipPath := mister.DIPPath(mraPath)
		ctx.DIPPath = dipPath
		ctx.DIPData = mister.LoadDIPData(dipPath, mra)
	}

	return ctx, true
}

func (s *Server) handleOSDVisible(req Request, send func(interface{})) {
	ctx, ok := s.resolveCore(req, send)
	if !ok {
		return
	}

	visible := mister.VisibleMenu(ctx.OSD, ctx.CFGData)
	send(map[string]interface{}{
		"mister":    "osd_visible",
		"success":   true,
		"core_name": ctx.OSD.CoreName,
		"menu":      visible,
	})
}

func (s *Server) handleCFGRead(req Request, send func(interface{})) {
	ctx, ok := s.resolveCore(req, send)
	if !ok {
		return
	}

	// Decode core option values from CFG bits
	settings := []map[string]interface{}{}
	for _, item := range ctx.OSD.Menu {
		if item.Type != "option" && item.Type != "option_hidden" {
			continue
		}
		val := mister.GetBitRange(ctx.CFGData, item.Bit, item.BitHigh)
		opt := map[string]interface{}{
			"name":   item.Name,
			"bit":    item.Bit,
			"value":  val,
			"source": "cfg",
		}
		if val < len(item.Values) {
			opt["value_name"] = item.Values[val]
		}
		if len(item.Values) > 0 {
			opt["values"] = item.Values
		}
		settings = append(settings, opt)
	}

	// Include DIP switches from MRA — read from separate .dip data
	if ctx.MRA != nil {
		dips := mister.ParseDIPSwitches(ctx.MRA)
		for _, dip := range dips {
			val := mister.GetBitRange(ctx.DIPData, dip.Bit, dip.BitHigh)
			opt := map[string]interface{}{
				"name":   dip.Name,
				"bit":    dip.Bit,
				"value":  val,
				"source": "dip",
			}
			if val < len(dip.Values) {
				opt["value_name"] = dip.Values[val]
			}
			if len(dip.Values) > 0 {
				opt["values"] = dip.Values
			}
			settings = append(settings, opt)
		}
	}

	resp := map[string]interface{}{
		"mister":    "cfg_read",
		"success":   true,
		"core_name": ctx.OSD.CoreName,
		"cfg_path":  ctx.CFGPath,
		"cfg_hex":   hex.EncodeToString(ctx.CFGData),
		"cfg_size":  len(ctx.CFGData),
		"settings":  settings,
	}
	if ctx.DIPPath != "" {
		resp["dip_path"] = ctx.DIPPath
		resp["dip_hex"] = hex.EncodeToString(ctx.DIPData)
	}
	send(resp)
}

func (s *Server) handleCFGWrite(req Request, send func(interface{})) {
	if req.Option == "" {
		send(map[string]interface{}{"error": "cfg_write requires option parameter"})
		return
	}
	if req.Value == "" {
		send(map[string]interface{}{"error": "cfg_write requires value parameter"})
		return
	}

	ctx, ok := s.resolveCore(req, send)
	if !ok {
		return
	}

	// Try CONF_STR options first → write to .CFG file
	item := mister.FindOption(ctx.OSD, req.Option)
	if item != nil {
		valIdx := mister.FindOptionValue(item, req.Value)
		if valIdx < 0 {
			send(map[string]interface{}{
				"mister":  "cfg_write",
				"success": false,
				"error":   fmt.Sprintf("value %q not found for option %s (available: %v)", req.Value, req.Option, item.Values),
			})
			return
		}

		mister.SetBitRange(ctx.CFGData, item.Bit, item.BitHigh, valIdx)

		if err := mister.WriteCFG(ctx.CFGPath, ctx.CFGData); err != nil {
			send(map[string]interface{}{
				"mister":  "cfg_write",
				"success": false,
				"error":   err.Error(),
			})
			return
		}

		send(map[string]interface{}{
			"mister":          "cfg_write",
			"success":         true,
			"core_name":       ctx.OSD.CoreName,
			"option":          item.Name,
			"value":           req.Value,
			"value_index":     valIdx,
			"cfg_path":        ctx.CFGPath,
			"source":          "cfg",
			"reload_required": true,
		})
		return
	}

	// Try DIP switches from MRA → write to .dip file
	if ctx.MRA != nil {
		dips := mister.ParseDIPSwitches(ctx.MRA)
		dip := mister.FindDIPSwitch(dips, req.Option)
		if dip != nil {
			valIdx := mister.FindDIPValue(dip, req.Value)
			if valIdx < 0 {
				send(map[string]interface{}{
					"mister":  "cfg_write",
					"success": false,
					"error":   fmt.Sprintf("value %q not found for DIP switch %s (available: %v)", req.Value, req.Option, dip.Values),
				})
				return
			}

			mister.SetBitRange(ctx.DIPData, dip.Bit, dip.BitHigh, valIdx)

			if err := mister.WriteDIP(ctx.DIPPath, ctx.DIPData); err != nil {
				send(map[string]interface{}{
					"mister":  "cfg_write",
					"success": false,
					"error":   err.Error(),
				})
				return
			}

			send(map[string]interface{}{
				"mister":          "cfg_write",
				"success":         true,
				"core_name":       ctx.OSD.CoreName,
				"option":          dip.Name,
				"value":           req.Value,
				"value_index":     valIdx,
				"dip_path":        ctx.DIPPath,
				"source":          "dip",
				"reload_required": true,
			})
			return
		}
	}

	send(map[string]interface{}{
		"mister":  "cfg_write",
		"success": false,
		"error":   fmt.Sprintf("option not found: %s (checked CONF_STR options and DIP switches)", req.Option),
	})
}

// sanitizeOutput removes non-printable characters that would corrupt JSON.
// Keeps printable ASCII, tabs, newlines, carriage returns, and valid UTF-8.
func sanitizeOutput(s string) string {
	var buf strings.Builder
	for _, r := range s {
		if (r >= 32 && r <= 126) || r == '\t' || r == '\n' || r == '\r' || r > 127 {
			buf.WriteRune(r)
		}
		// Drop control chars (0-31 except tab/newline/CR) and DEL (127)
	}
	return buf.String()
}
