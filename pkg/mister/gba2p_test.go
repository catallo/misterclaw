package mister

import (
	"encoding/xml"
	"reflect"
	"testing"
)

func TestGBA2PDefaultUsesIndependentCore(t *testing.T) {
	for _, name := range []string{"GBA2P", "gba2p"} {
		cfg, ok := getDefaultConfig(name)
		if !ok || cfg.Core != "_Console/GBA2P" || cfg.Type != "f" || cfg.Index != 1 || cfg.Delay != 2 || !reflect.DeepEqual(cfg.Extensions, []string{".gba"}) || cfg.SetName != "" {
			t.Fatalf("incorrect independent GBA2P profile for %q: %+v", name, cfg)
		}
	}
	base, ok := getDefaultConfig("GBA")
	if !ok || base.Core != "_Console/GBA" {
		t.Fatalf("base GBA profile changed: %+v", base)
	}
}

func TestGBA2PMGLKeepsArchiveMemberAndCoreNamespace(t *testing.T) {
	path := zipTestPath(t, "Advance Wars.zip", zipTestArchive(t, zipTestEntry{"Advance Wars.gba", []byte("ROM"), 0}), "Advance Wars.gba")
	var descriptor mglFile
	if err := xml.Unmarshal([]byte(GenerateMGL(GameInfo{System: "GBA2P", Path: path, Name: "Advance Wars"})), &descriptor); err != nil {
		t.Fatal(err)
	}
	if descriptor.Rbf != "_Console/GBA2P" || descriptor.File.Type != "f" || descriptor.File.Index != 1 || descriptor.File.Path != path || descriptor.SetName != nil {
		t.Fatalf("incorrect GBA2P ZIP descriptor: %+v", descriptor)
	}
	if got := mglPath("_Console/GBA2P"); got != "/media/fat/_Console/GBA2P.mgl" {
		t.Fatalf("GBA2P descriptor must not reuse base GBA namespace: %q", got)
	}
}
