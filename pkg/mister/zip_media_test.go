package mister

import (
	"archive/zip"
	"bytes"
	"encoding/binary"
	"encoding/xml"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

type zipTestEntry struct {
	name string
	data []byte
	mode os.FileMode
}

func zipTestArchive(t *testing.T, entries ...zipTestEntry) []byte {
	t.Helper()
	var b bytes.Buffer
	w := zip.NewWriter(&b)
	for _, entry := range entries {
		h := &zip.FileHeader{Name: entry.name, Method: zip.Store}
		if entry.mode != 0 {
			h.SetMode(entry.mode)
		}
		f, err := w.CreateHeader(h)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := f.Write(entry.data); err != nil {
			t.Fatal(err)
		}
	}
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}
	return b.Bytes()
}

func zipTestPath(t *testing.T, archiveName string, archive []byte, member string) string {
	t.Helper()
	p := filepath.Join(t.TempDir(), archiveName)
	if err := os.WriteFile(p, archive, 0600); err != nil {
		t.Fatal(err)
	}
	return p + "/" + member
}

func TestZIPMemberLaunchMediaAndMGL(t *testing.T) {
	for _, tc := range []struct{ system, member string }{
		{"GBA", "US/Golden Sun (USA).gba"},
		{"S32X", "nested/Knuckles' Chaotix.32x"},
	} {
		t.Run(tc.system, func(t *testing.T) {
			archive := zipTestArchive(t, zipTestEntry{tc.member, []byte("ROM media"), 0}, zipTestEntry{"readme.txt", []byte("not a ROM"), 0})
			p := zipTestPath(t, "Collection.ZIP", archive, tc.member)
			game := GameInfo{System: tc.system, Path: p}
			if err := ValidateGameMedia(game); err != nil {
				t.Fatal(err)
			}
			var mgl mglFile
			if err := xml.Unmarshal([]byte(GenerateMGL(game)), &mgl); err != nil {
				t.Fatal(err)
			}
			cfg, _ := GetSystemConfig(tc.system)
			if mgl.File.Path != p || mgl.Rbf != cfg.Core || mgl.File.Type != cfg.Type ||
				mgl.File.Index != cfg.Index || mgl.File.Delay != cfg.Delay {
				t.Fatalf("MGL changed native path or profile: %+v", mgl)
			}
			physical := strings.TrimSuffix(p, "/"+tc.member)
			got, err := os.ReadFile(physical)
			if err != nil || !bytes.Equal(got, archive) {
				t.Fatal("archive changed during validation")
			}
			files, _ := os.ReadDir(filepath.Dir(physical))
			if len(files) != 1 {
				t.Fatal("validation extracted files")
			}
		})
	}
}

func TestZIPOrdinaryMediaUnchanged(t *testing.T) {
	root := t.TempDir()
	for _, tc := range []struct{ system, name string }{{"GBA", "rom.gba"}, {"NeoGeo", "set.zip"}, {"GBA", "real.zip/rom.gba"}} {
		p := filepath.Join(root, tc.name)
		if err := os.MkdirAll(filepath.Dir(p), 0700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte("ordinary bytes, not necessarily a ZIP"), 0600); err != nil {
			t.Fatal(err)
		}
		if err := ValidateGameMedia(GameInfo{System: tc.system, Path: p}); err != nil {
			t.Fatalf("ordinary media changed: %v", err)
		}
	}
	if err := ValidateGameMedia(GameInfo{System: "GBA", Path: filepath.Join(root, "missing.gba")}); err == nil {
		t.Fatal("missing ordinary file accepted")
	}
	if err := os.Mkdir(filepath.Join(root, "empty.zip"), 0700); err != nil {
		t.Fatal(err)
	}
	if err := ValidateZIPGame(GameInfo{System: "GBA", Path: filepath.Join(root, "empty.zip/game.gba")}); err == nil {
		t.Fatal("real directory misclassified as archive")
	}
}

func TestZIPProfileAndOverrides(t *testing.T) {
	cfg := SystemConfig{Type: "f", Index: 1, Extensions: []string{".gba", ".d64"}}
	if err := validateZIPProfile(cfg, "Custom", "game.gba"); err != nil {
		t.Fatal(err)
	}
	for _, cfg := range []SystemConfig{
		{Type: "s", Index: 0, Extensions: []string{".gba"}},
		{Type: "f", Index: -1, Extensions: []string{".gba"}},
		{Type: "f", Index: 256, Extensions: []string{".gba"}},
		{Type: "f", Index: 1, Extensions: []string{".32x"}},
	} {
		if err := validateZIPProfile(cfg, "Custom", "game.gba"); err == nil {
			t.Fatalf("incompatible profile accepted: %+v", cfg)
		}
	}
	if err := validateZIPProfile(cfg, "Custom", "disk.d64"); err == nil {
		t.Fatal("disk media accepted with forged file injection")
	}
	cfg.FormatOverrides = []FormatOverride{{Type: "s", Index: 0, Extensions: []string{".gba"}}}
	if err := validateZIPProfile(cfg, "Custom", "game.gba"); err == nil {
		t.Fatal("stream override ignored")
	}
	cfg.Type = "s"
	cfg.FormatOverrides[0].Type = "f"
	cfg.FormatOverrides[0].Index = 7
	if err := validateZIPProfile(cfg, "Custom", "game.gba"); err != nil {
		t.Fatal("file-injection override not preserved", err)
	}
	cfg.Extensions = []string{".zip"}
	cfg.FormatOverrides = nil
	cfg.Type = "f"
	if err := validateZIPProfile(cfg, "GBA", "game.gba"); err != nil {
		t.Fatal("physical discovery extensions hid known member format", err)
	}
}

func TestZIPRejectUnsafePathsAndProfiles(t *testing.T) {
	archive := zipTestArchive(t, zipTestEntry{"game.gba", []byte("ROM"), 0})
	base := strings.TrimSuffix(zipTestPath(t, "game.zip", archive, "game.gba"), "/game.gba")
	for _, tc := range []struct{ system, path string }{
		{"GBA", base + "/missing.gba"}, {"GBA", base + "/GAME.GBA"},
		{"GBA", base + "/../game.gba"}, {"GBA", base + "//game.gba"},
		{"GBA", base + "/folder/../../game.gba"}, {"GBA", base + "/folder\\game.gba"},
		{"GBA", base + "/C:/game.gba"}, {"GBA", base + "/inner.zip/game.gba"},
		{"GBA", base + "/game&.gba"}, {"GBA", base + "/game<.gba"}, {"GBA", base + "/game\".gba"},
		{"GBA", base + "/game\n.gba"}, {"GBA", "relative.zip/game.gba"},
		{"Unknown", base + "/game.gba"}, {"S32X", base + "/game.gba"},
		{"PSX", base + "/game.bin"}, {"C64", base + "/disk.d64"},
		{"GBA", filepath.Join(filepath.Dir(base), "missing.zip/game.gba")},
		{"GBA", filepath.Join(filepath.Dir(base), "before.zip.txt/archive.zip/game.gba")},
		{"GBA", base + "/" + strings.Repeat("x", maxZIPNameBytes+1) + ".gba"},
	} {
		if err := ValidateZIPGame(GameInfo{System: tc.system, Path: tc.path}); err == nil {
			t.Errorf("accepted unsafe/incompatible launch: %+v", tc)
		}
	}
	link := filepath.Join(t.TempDir(), "link.zip")
	if err := os.Symlink(base, link); err != nil {
		t.Fatal(err)
	}
	if err := ValidateZIPGame(GameInfo{System: "GBA", Path: link + "/game.gba"}); err == nil {
		t.Fatal("archive symlink accepted")
	}
}

func TestZIPRejectUnsafeAndAmbiguousEntries(t *testing.T) {
	valid := zipTestEntry{"game.gba", []byte("ROM"), 0}
	for _, bad := range []zipTestEntry{
		{"game.gba", []byte("duplicate"), 0}, {"GAME.GBA", []byte("case duplicate"), 0},
		{"../escape.gba", nil, 0}, {"/absolute.gba", nil, 0}, {"C:/absolute.gba", nil, 0},
		{"folder\\escape.gba", nil, 0}, {"folder/../escape.gba", nil, 0},
		{"link.gba", []byte("target.gba"), os.ModeSymlink | 0777},
	} {
		p := zipTestPath(t, "game.zip", zipTestArchive(t, valid, bad), "game.gba")
		if err := ValidateZIPGame(GameInfo{System: "GBA", Path: p}); err == nil {
			t.Errorf("unsafe entry accepted: %+v", bad)
		}
	}
	for _, entry := range []zipTestEntry{{"game.gba/", nil, os.ModeDir | 0755}, {"game.gba", nil, 0}} {
		p := zipTestPath(t, "game.zip", zipTestArchive(t, entry), "game.gba")
		if err := ValidateZIPGame(GameInfo{System: "GBA", Path: p}); err == nil {
			t.Error("empty/directory member accepted")
		}
	}
}

func TestZIPRejectMalformedHeadersAndCRC(t *testing.T) {
	good := zipTestArchive(t, zipTestEntry{"game.gba", []byte("ROM payload"), 0})
	central := bytes.Index(good, []byte{'P', 'K', 1, 2})
	end := bytes.LastIndex(good, []byte{'P', 'K', 5, 6})
	put16 := binary.LittleEndian.PutUint16
	put32 := binary.LittleEndian.PutUint32
	cases := map[string]func([]byte){
		"CRC":                 func(b []byte) { b[30+len("game.gba")] ^= 0xff },
		"encrypted":           func(b []byte) { b[6] |= 1; b[central+8] |= 1 },
		"compression":         func(b []byte) { put16(b[8:10], 12); put16(b[central+10:central+12], 12) },
		"local name":          func(b []byte) { b[30] = 'X' },
		"local flags":         func(b []byte) { b[6] ^= 8 },
		"descriptor":          func(b []byte) { b[central-12] ^= 0xff },
		"member expansion":    func(b []byte) { put32(b[central+24:central+28], maxZIPMemberBytes+1) },
		"metadata":            func(b []byte) { put32(b[end+12:end+16], maxZIPMetadataBytes+1) },
		"entries":             func(b []byte) { put16(b[end+8:end+10], maxZIPEntries+1); put16(b[end+10:end+12], maxZIPEntries+1) },
		"multi disk":          func(b []byte) { put16(b[end+4:end+6], 1) },
		"ZIP64":               func(b []byte) { put32(b[end+16:end+20], 0xffffffff) },
		"central count":       func(b []byte) { put16(b[end+8:end+10], 2); put16(b[end+10:end+12], 2) },
		"overlap":             func(b []byte) { put32(b[central+20:central+24], uint32(len(b))) },
		"forged small output": func(b []byte) { put32(b[central+24:central+28], 1); put32(b[central-4:central], 1) },
	}
	for name, mutate := range cases {
		t.Run(name, func(t *testing.T) {
			b := append([]byte(nil), good...)
			mutate(b)
			if err := validateZIPMember(bytes.NewReader(b), int64(len(b)), "game.gba"); err == nil {
				t.Fatal("malformed ZIP accepted")
			}
		})
	}
	for _, b := range [][]byte{nil, []byte("forged archive"), good[:len(good)-5], append([]byte("prefix"), good...), append(append([]byte(nil), good...), 'x')} {
		if err := validateZIPMember(bytes.NewReader(b), int64(len(b)), "game.gba"); err == nil {
			t.Fatal("truncated/prefixed/malformed ZIP accepted")
		}
	}
	if _, _, err := preflightZIP(bytes.NewReader(good), maxZIPArchiveBytes+1); err == nil {
		t.Fatal("archive size cap not enforced before reading")
	}
}

func TestZIPHighCompressionPaddedROM(t *testing.T) {
	var b bytes.Buffer
	w := zip.NewWriter(&b)
	f, err := w.Create("padded.gba")
	if err != nil {
		t.Fatal(err)
	}
	data := make([]byte, 8<<20)
	if _, err := f.Write(data); err != nil {
		t.Fatal(err)
	}
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}
	if err := validateZIPMember(bytes.NewReader(b.Bytes()), int64(b.Len()), "padded.gba"); err != nil {
		t.Fatal("valid padded ROM rejected by compression ratio", err)
	}
}

func TestZIPScannerDoesNotReturnBareGBAArchives(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "game.zip"), []byte("ordinary ROM-set"), 0600); err != nil {
		t.Fatal(err)
	}
	if games := scanDir(root, "GBA", "sd", map[string]bool{".zip": true}); len(games) != 0 {
		t.Fatal("bare GBA ZIP returned for auto-launch", games)
	}
	if games := scanDir(root, "NeoGeo", "sd", map[string]bool{".zip": true}); len(games) != 1 {
		t.Fatal("direct NeoGeo ZIP disappeared", games)
	}
}

func TestZIPLaunchRejectsBeforeCommandWrite(t *testing.T) {
	sink := reviewCommandSink(t)
	p := zipTestPath(t, "bad.zip", []byte("not ZIP"), "game.gba")
	if err := LaunchGame(GameInfo{System: "GBA", Path: p}); err == nil {
		t.Fatal("invalid ZIP reached native launch")
	}
	if got, _ := os.ReadFile(sink); len(got) != 0 {
		t.Fatal("invalid ZIP wrote native command")
	}
}

func TestZIPSearchFiltersOldBareArchiveCache(t *testing.T) {
	cacheMu.Lock()
	previous := cachedGames
	cachedGames = map[string][]GameInfo{
		"gba":    {{Name: "Golden Sun", System: "GBA", Path: "/media/fat/game.zip"}},
		"neogeo": {{Name: "Metal Slug", System: "NeoGeo", Path: "/media/fat/slug.zip"}},
	}
	cacheMu.Unlock()
	t.Cleanup(func() {
		cacheMu.Lock()
		cachedGames = previous
		cacheMu.Unlock()
	})
	if games := SearchGames("Golden Sun", "GBA"); len(games) != 0 {
		t.Fatal("old bare GBA archive still searchable", games)
	}
	if games := SearchGames("Metal Slug", "NeoGeo"); len(games) != 1 {
		t.Fatal("old direct NeoGeo archive no longer searchable", games)
	}
}

func FuzzZIPMemberValidation(f *testing.F) {
	f.Add([]byte("not an archive"))
	var b bytes.Buffer
	w := zip.NewWriter(&b)
	rom, err := w.Create("game.gba")
	if err != nil {
		f.Fatal(err)
	}
	if _, err := rom.Write([]byte("ROM")); err != nil {
		f.Fatal(err)
	}
	if err := w.Close(); err != nil {
		f.Fatal(err)
	}
	f.Add(b.Bytes())
	f.Fuzz(func(t *testing.T, data []byte) {
		_ = validateZIPMember(bytes.NewReader(data), int64(len(data)), "game.gba")
	})
}

func TestZIPMetadataSnapshotIsImmutable(t *testing.T) {
	original := zipTestArchive(t, zipTestEntry{"game.gba", []byte("ROM"), 0})
	mutable := append([]byte(nil), original...)
	offset, _, err := preflightZIP(bytes.NewReader(mutable), int64(len(mutable)))
	if err != nil {
		t.Fatal(err)
	}
	metadata := append([]byte(nil), mutable[offset:]...)
	r := zipMetadataReader{source: bytes.NewReader(mutable), offset: offset, metadata: bytes.NewReader(metadata)}
	for i := int(offset); i < len(mutable); i++ {
		mutable[i] = 0xff
	}
	zr, err := zip.NewReader(r, int64(len(mutable)))
	if err != nil || len(zr.File) != 1 || zr.File[0].Name != "game.gba" {
		t.Fatalf("reader used mutable central metadata: %v", err)
	}
	buf := make([]byte, 40)
	if _, err := r.ReadAt(buf, offset-20); err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(buf, original[offset-20:offset+20]) {
		t.Fatal("read crossing frozen metadata boundary changed")
	}
}
