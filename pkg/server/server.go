package server

import (
	"bufio"
	"encoding/json"
	"fmt"
	"log"
	"net"
	"path/filepath"
	"strings"
	"sync"

	"time"

	"github.com/catallo/clawexec-mister-fpga/pkg/mister"
	"github.com/catallo/clawexec-mister-fpga/pkg/session"
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

	// PTY resize
	Resize *ResizeRequest `json:"resize,omitempty"`

	// MiSTer commands
	MiSTer   string `json:"mister,omitempty"`
	Core     string `json:"core,omitempty"`
	Path     string `json:"path,omitempty"`
	Query    string `json:"query,omitempty"`
	System   string `json:"system,omitempty"`
	Action   string `json:"action,omitempty"`
	URL      string `json:"url,omitempty"`
	Hostname string `json:"hostname,omitempty"`
}

// ResizeRequest holds PTY dimensions.
type ResizeRequest struct {
	Cols int `json:"cols"`
	Rows int `json:"rows"`
}

// Server is a TCP server handling the ClawExec JSON protocol.
type Server struct {
	listener net.Listener
	manager  *session.Manager
	clients  map[net.Conn]struct{}
	mu       sync.Mutex
}

// New creates a new Server.
func New(manager *session.Manager) *Server {
	return &Server{
		manager: manager,
		clients: make(map[net.Conn]struct{}),
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
	defer func() {
		conn.Close()
		s.mu.Lock()
		delete(s.clients, conn)
		s.mu.Unlock()
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

		s.dispatch(req, send)
	}
}

func (s *Server) dispatch(req Request, send func(interface{})) {
	switch {
	case req.List != nil && *req.List:
		s.handleList(send)

	case req.Kill != nil && *req.Kill:
		s.handleKill(req, send)

	case req.Close != nil && *req.Close:
		s.handleClose(req, send)

	case req.Resize != nil:
		s.handleResize(req, send)

	case req.Input != "":
		s.handleInput(req, send)

	case req.MiSTer != "":
		s.handleMiSTer(req, send)

	case req.Cmd != "":
		s.handleCmd(req, send)

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
		"success": success,
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

func (s *Server) handleCmd(req Request, send func(interface{})) {
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
			"data":   string(data),
		})
	}

	doneCb := func(exitCode int) {
		send(map[string]interface{}{
			"id":        id,
			"done":      true,
			"exit_code": exitCode,
			"sessions":  s.manager.List(),
		})
	}

	s.manager.Execute(sessionName, req.Cmd, usePty, req.Agent, outputCb, doneCb)
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
		send(map[string]interface{}{
			"mister":  "systems",
			"systems": stats,
		})

	case "search":
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
			send(map[string]interface{}{
				"mister":  "launch",
				"success": false,
				"error":   "no game found or missing parameters",
			})
			return
		}

		cfg, _ := mister.GetSystemConfig(game.System)
		err := mister.LaunchGame(*game)
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
			"core_name": cfg.Core,
		})

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

	default:
		send(map[string]interface{}{
			"error": fmt.Sprintf("unknown mister command: %s", req.MiSTer),
		})
	}
}
