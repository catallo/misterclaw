package mister

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"time"
)

var cmdPath = "/dev/MiSTer_cmd"

// setMisterCmdPath overrides the command path (for testing).
func setMisterCmdPath(p string) {
	cmdPath = p
}

// CoreStatus represents the currently running core.
type CoreStatus struct {
	CoreName string `json:"core_name"` // e.g. "DonkeyKong_20240526"
	CorePath string `json:"core_path"` // e.g. "/media/fat/_Arcade/cores/DonkeyKong_20240526.rbf"
	GamePath string `json:"game_path"` // e.g. "/media/fat/_Arcade/Donkey Kong (US, Set 1).mra"
}

// GetRunningCore reads /proc to find what core MiSTer is currently running.
func GetRunningCore() (*CoreStatus, error) {
	entries, err := os.ReadDir("/proc")
	if err != nil {
		return nil, fmt.Errorf("reading /proc: %w", err)
	}

	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		// Only look at numeric (PID) directories
		if len(entry.Name()) == 0 || entry.Name()[0] < '0' || entry.Name()[0] > '9' {
			continue
		}
		cmdline, err := os.ReadFile(filepath.Join("/proc", entry.Name(), "cmdline"))
		if err != nil {
			continue
		}
		// cmdline is null-separated
		parts := strings.Split(string(cmdline), "\x00")
		if len(parts) < 1 {
			continue
		}
		// Look for the MiSTer binary
		if !strings.HasSuffix(parts[0], "/MiSTer") && parts[0] != "MiSTer" {
			continue
		}

		status := &CoreStatus{}
		for _, arg := range parts[1:] {
			if arg == "" {
				continue
			}
			if strings.HasSuffix(arg, ".rbf") {
				status.CorePath = arg
				base := filepath.Base(arg)
				status.CoreName = strings.TrimSuffix(base, filepath.Ext(base))
			} else if strings.HasSuffix(arg, ".mra") || strings.HasSuffix(arg, ".mgl") {
				status.GamePath = arg
			}
		}
		return status, nil
	}

	return nil, fmt.Errorf("MiSTer process not found")
}

// LoadCore writes a load_core command to /dev/MiSTer_cmd.
func LoadCore(path string) error {
	return writeCmd("load_core " + path)
}

// ValidateCorePath checks that a load_core target is plausible before it is
// handed to MiSTer. A missing, empty, or truncated .rbf (e.g. a failed
// transfer) would otherwise be loaded straight into the FPGA fabric —
// task #18's "0-byte RBF install". Guard by existence, type, extension, and
// (for .rbf) a sane minimum size.
func ValidateCorePath(path string) error {
	info, err := os.Stat(path)
	if err != nil {
		if os.IsNotExist(err) {
			return fmt.Errorf("core path does not exist: %s", path)
		}
		return fmt.Errorf("stat core path %s: %w", path, err)
	}
	if info.IsDir() {
		return fmt.Errorf("core path is a directory, not a core file: %s", path)
	}
	switch strings.ToLower(filepath.Ext(path)) {
	case ".rbf":
		// Real DE10-Nano cores are multiple MB; anything this small is a
		// truncated or empty bitstream, not a loadable core.
		const minRBFSize = 64 * 1024
		if info.Size() < minRBFSize {
			return fmt.Errorf("refusing to load %s: %d bytes is too small for an RBF "+
				"(truncated/empty transfer?), expected >= %d", path, info.Size(), minRBFSize)
		}
	case ".mgl", ".mra":
		if info.Size() == 0 {
			return fmt.Errorf("refusing to load %s: core descriptor is empty", path)
		}
	default:
		return fmt.Errorf("refusing to load %s: unsupported extension "+
			"(expected .rbf, .mgl or .mra)", path)
	}
	return nil
}

// LoadCoreVerified loads a core and verifies the switch actually happened.
// Returns the new core status or an error if verification failed.
func LoadCoreVerified(path string, timeout time.Duration) (*CoreStatus, error) {
	// Guard the target before touching /dev/MiSTer_cmd (task #18).
	if err := ValidateCorePath(path); err != nil {
		return nil, err
	}

	// Get current state before loading
	before, _ := GetRunningCore()
	var beforeGame string
	if before != nil {
		beforeGame = before.GamePath
	}
	var beforeCore string
	if before != nil {
		beforeCore = before.CorePath
	}

	// Send the load command
	if err := LoadCore(path); err != nil {
		return nil, fmt.Errorf("writing command: %w", err)
	}

	// Poll for change
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		time.Sleep(500 * time.Millisecond)

		after, err := GetRunningCore()
		if err != nil {
			continue
		}

		// Check if something changed
		if after.CorePath != beforeCore || after.GamePath != beforeGame {
			return after, nil
		}
	}

	// Check one final time
	after, err := GetRunningCore()
	if err != nil {
		return nil, fmt.Errorf("core load not verified: %w", err)
	}

	// If loading the same core again (e.g. reload), consider it success
	if strings.Contains(path, filepath.Base(after.CorePath)) || after.GamePath == path {
		return after, nil
	}

	return nil, fmt.Errorf("core did not change after load_core (still running: %s)", after.CorePath)
}

// TakeScreenshot writes a screenshot command to /dev/MiSTer_cmd.
func TakeScreenshot() error {
	return writeCmd("screenshot")
}

func writeCmd(cmd string) error {
	// Open the FIFO non-blocking. /dev/MiSTer_cmd is a named pipe read by
	// the MiSTer main process; a plain O_WRONLY open(2) BLOCKS FOREVER when
	// no reader is present. If MiSTer main has died, every screenshot /
	// load_core / reload then hangs the daemon's connection until the
	// client's read deadline fires — the opaque "i/o timeout" seen in the
	// field (task #18). O_NONBLOCK makes open(2) return ENXIO immediately
	// so we can report the real cause instead of hanging.
	f, err := os.OpenFile(cmdPath, os.O_WRONLY|syscall.O_NONBLOCK, 0)
	if err != nil {
		if os.IsNotExist(err) {
			return fmt.Errorf("%s not found (not running on MiSTer?)", cmdPath)
		}
		if errors.Is(err, syscall.ENXIO) {
			return fmt.Errorf("%s has no reader — the MiSTer main process is not running "+
				"(restart it with '/media/fat/MiSTer &')", cmdPath)
		}
		return fmt.Errorf("opening %s: %w", cmdPath, err)
	}
	defer f.Close()
	if _, err := f.WriteString(cmd + "\n"); err != nil {
		if errors.Is(err, syscall.EAGAIN) {
			return fmt.Errorf("writing to %s: pipe full — MiSTer main is not consuming "+
				"commands (stuck or overloaded)", cmdPath)
		}
		return err
	}
	return nil
}
