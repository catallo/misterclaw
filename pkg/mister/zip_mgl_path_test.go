package mister

import (
	"bytes"
	"encoding/xml"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestZIPMGLNativePathBufferBoundary(t *testing.T) {
	for _, system := range []string{"GBA", "GBA2P", "S32X"} {
		t.Run(system, func(t *testing.T) {
			archivePath := filepath.Join(t.TempDir(), "Boundary.ZIP")
			ext := ".gba"
			if system == "S32X" {
				ext = ".32x"
			}
			// Build a real archive member whose virtual path exactly fills the
			// native 1024-byte buffer, leaving one byte for its terminator.
			remaining := maxZIPPathBytes - len(archivePath) - 1
			var components []string
			for remaining > maxZIPNameBytes {
				components = append(components, strings.Repeat("d", 200))
				remaining -= 201
			}
			components = append(components, strings.Repeat("r", remaining-len(ext))+ext)
			member := strings.Join(components, "/")
			oversizedMember := strings.TrimSuffix(member, ext) + "r" + ext
			archive := zipTestArchive(t,
				zipTestEntry{member, []byte("boundary ROM"), 0},
				zipTestEntry{oversizedMember, []byte("oversized ROM"), 0})
			if err := os.WriteFile(archivePath, archive, 0600); err != nil {
				t.Fatal(err)
			}
			game := GameInfo{System: system, Path: archivePath + "/" + member}
			if len(game.Path) != 1023 {
				t.Fatalf("fixture has %d bytes, want 1023", len(game.Path))
			}
			if err := ValidateGameMedia(game); err != nil {
				t.Fatalf("1023-byte native path rejected: %v", err)
			}
			mgl := GenerateMGL(game)
			var descriptor mglFile
			if err := xml.Unmarshal([]byte(mgl), &descriptor); err != nil {
				t.Fatal(err)
			}
			if descriptor.File.Path != game.Path || len(descriptor.File.Path) != 1023 || !filepath.IsAbs(descriptor.File.Path) {
				t.Fatalf("native attribute changed or exceeded buffer: %q (%d bytes)", descriptor.File.Path, len(descriptor.File.Path))
			}
			// Check the actual transmitted attribute, not only XML decoding.
			if !strings.Contains(mgl, `path="`+game.Path+`"`) {
				t.Fatal("generated MGL does not carry the exact absolute attribute")
			}
			var nativeBuffer [1024]byte
			copy(nativeBuffer[:1023], descriptor.File.Path)
			if nativeBuffer[1023] != 0 || string(nativeBuffer[:1023]) != game.Path {
				t.Fatal("path would truncate in the native buffer")
			}
			game.Path = archivePath + "/" + oversizedMember
			if len(game.Path) != 1024 {
				t.Fatal("oversized fixture is not 1024 bytes")
			}
			if err := ValidateGameMedia(game); err == nil {
				t.Fatal("1024-byte member path accepted by validation")
			}
			if mgl := GenerateMGL(game); mgl != "" {
				t.Fatal("pure MGL generation accepted a truncating member attribute")
			}
			got, err := os.ReadFile(archivePath)
			if err != nil || !bytes.Equal(got, archive) {
				t.Fatal("validation or MGL generation changed the archive")
			}
			files, err := os.ReadDir(filepath.Dir(archivePath))
			if err != nil || len(files) != 1 {
				t.Fatal("validation or MGL generation extracted files")
			}
		})
	}
}

func TestZIPMGLClassificationPreservesPhysicalFiles(t *testing.T) {
	root := t.TempDir()
	for _, tc := range []struct{ system, name string }{
		{"GBA", "regular.gba"},
		{"NeoGeo", "direct.zip"},
		{"GBA", "legacy-bare.zip"},
		{"GBA", "directory.zip/regular.gba"},
	} {
		p := filepath.Join(root, tc.name)
		if err := os.MkdirAll(filepath.Dir(p), 0700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte("legacy physical file"), 0600); err != nil {
			t.Fatal(err)
		}
		var descriptor mglFile
		if err := xml.Unmarshal([]byte(GenerateMGL(GameInfo{System: tc.system, Path: p})), &descriptor); err != nil {
			t.Fatal(err)
		}
		if descriptor.File.Path != "../../../../.."+p {
			t.Errorf("ordinary path behavior changed for %q", p)
		}
	}
	for _, p := range []string{
		filepath.Join(root, "directory.zip/missing.gba"),
		filepath.Join(root, "missing.zip/member.gba"),
		filepath.Join(root, "earlier.zip.suffix/archive.zip/member.gba"),
	} {
		if isPhysicalZIPMemberPath(p) {
			t.Errorf("nonarchive or ambiguous path misclassified: %q", p)
		}
	}
}

func TestZIPMGLClassificationDoesNotValidateOrOpenMembers(t *testing.T) {
	p := zipTestPath(t, "Regular.ZIP", []byte("not a ZIP archive"), "member.gba")
	// GenerateMGL only classifies the filesystem prefix. Actual launch must
	// reject this archive; descriptor generation must not open or extract it.
	if !isPhysicalZIPMemberPath(p) {
		t.Fatal("regular archive prefix was not classified")
	}
	if err := ValidateGameMedia(GameInfo{System: "GBA", Path: p}); err == nil {
		t.Fatal("descriptor classification replaced mandatory launch validation")
	}
	link := filepath.Join(t.TempDir(), "link.zip")
	if err := os.Symlink(strings.TrimSuffix(p, "/member.gba"), link); err != nil {
		t.Fatal(err)
	}
	if isPhysicalZIPMemberPath(link + "/member.gba") {
		t.Fatal("symlink archive misclassified as supported physical archive")
	}
}
