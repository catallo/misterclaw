package mister

import (
	"path/filepath"
	"reflect"
	"testing"
)

func TestLocationRescanMergesNewFormatsAndCDLaunchConfig(t *testing.T) {
	root := arcadeDiscoveryEnvironment(t)
	sd := filepath.Join(root, "games", "UnknownMachine")
	usb := filepath.Join(root, "usb0", "UnknownMachine")
	arcadeDiscoveryWrite(t, filepath.Join(sd, "SD.bin"), "a")
	arcadeDiscoveryWrite(t, filepath.Join(usb, "USB.bin"), "b")
	arcadeDiscoverySeed(t)
	arcadeDiscoveryWrite(t, filepath.Join(sd, "New.cue"), "FILE game.bin BINARY")
	RescanLocation("sd")
	cfg, ok := GetSystemConfig("UnknownMachine")
	if !ok || !reflect.DeepEqual(cfg.Extensions, []string{".bin", ".cue"}) || cfg.Type != "s" || cfg.Index != 0 {
		t.Fatalf("refreshed merged config=%+v", cfg)
	}
	if got := len(ScanSystem("UnknownMachine")); got != 3 {
		t.Fatalf("direct listing has %d games, want 3", got)
	}
}

func TestLocationRescanAcceptsNewlyInstalledCore(t *testing.T) {
	root := arcadeDiscoveryEnvironment(t)
	arcadeDiscoveryWrite(t, filepath.Join(root, "games", "NewMachine", "SD.bin"), "a")
	arcadeDiscoveryWrite(t, filepath.Join(root, "usb0", "NewMachine", "USB.bin"), "b")
	arcadeDiscoverySeed(t)
	arcadeDiscoveryWrite(t, filepath.Join(consoleCoresPath, "NewMachine_20261008.rbf"), "core fixture")
	RescanLocation("sd")
	cfg, ok := GetSystemConfig("NewMachine")
	if !ok || cfg.Core == "" {
		t.Fatalf("new core not accepted: %+v", cfg)
	}
	if mgl := GenerateMGL(GameInfo{System: "NewMachine", Path: filepath.Join(root, "games", "NewMachine", "SD.bin")}); mgl == "" {
		t.Fatal("MGL generation still fails despite newly detected core")
	}
}
