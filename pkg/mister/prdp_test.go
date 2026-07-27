package mister

import (
	"encoding/binary"
	"os"
	"path/filepath"
	"testing"
)

func TestPRDPCoreRunning(t *testing.T) {
	tmp := filepath.Join(t.TempDir(), "CORENAME")
	old := prdpCoreNamePath
	prdpCoreNamePath = tmp
	t.Cleanup(func() { prdpCoreNamePath = old })

	if PRDPCoreRunning() {
		t.Error("no CORENAME file: expected false")
	}
	os.WriteFile(tmp, []byte("SNES\n"), 0644)
	if PRDPCoreRunning() {
		t.Error("CORENAME=SNES: expected false")
	}
	os.WriteFile(tmp, []byte("PRDP\n"), 0644)
	if !PRDPCoreRunning() {
		t.Error("CORENAME=PRDP: expected true")
	}
}

func TestPRDPDecodeFrame(t *testing.T) {
	raw := make([]byte, prdpFBBytes)
	// Pixel (0,0): pure red (R=31), opaque bit set
	binary.LittleEndian.PutUint16(raw[0:], 0x8000|31<<10)
	// Pixel (1,0): mid green (G=16)
	binary.LittleEndian.PutUint16(raw[2:], 16<<5)
	// Pixel (0,1): pure blue (B=31)
	binary.LittleEndian.PutUint16(raw[2*prdpWidth:], 31)

	img, lit := prdpDecodeFrame(raw)
	if lit != 3 {
		t.Errorf("lit = %d, want 3 (alpha bit must not count as color)", lit)
	}
	if c := img.RGBAAt(0, 0); c.R != 255 || c.G != 0 || c.B != 0 {
		t.Errorf("(0,0) = %v, want pure red", c)
	}
	if c := img.RGBAAt(1, 0); c.G != 16<<3|16>>2 {
		t.Errorf("(1,0).G = %d, want %d (5->8 bit expansion)", c.G, 16<<3|16>>2)
	}
	if c := img.RGBAAt(0, 1); c.B != 255 {
		t.Errorf("(0,1).B = %d, want 255", c.B)
	}
	if c := img.RGBAAt(5, 5); c.R != 0 || c.G != 0 || c.B != 0 || c.A != 255 {
		t.Errorf("(5,5) = %v, want opaque black", c)
	}
}
