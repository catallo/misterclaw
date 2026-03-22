package mister

import (
	"os"
	"path/filepath"
	"testing"
)

const (
	testWidth  = 1280
	testHeight = 1024
)

func testOSD() *OSD {
	return newOSD(testWidth, testHeight)
}

func TestColorBGRA(t *testing.T) {
	c := Color{B: 0xFF, G: 0x00, R: 0x80, A: 0xCC}
	if c.B != 0xFF || c.G != 0x00 || c.R != 0x80 || c.A != 0xCC {
		t.Errorf("unexpected color values: %+v", c)
	}
}

func TestFontLookup(t *testing.T) {
	// Space (index 0) should be all zeros
	for _, b := range font8x16[0] {
		if b != 0 {
			t.Fatal("space character should be all zeros")
		}
	}

	// 'A' (index 65-32=33) should have non-zero data
	hasNonZero := false
	for _, b := range font8x16['A'-32] {
		if b != 0 {
			hasNonZero = true
			break
		}
	}
	if !hasNonZero {
		t.Fatal("'A' glyph should have non-zero data")
	}
}

func TestFontBounds(t *testing.T) {
	// Should have exactly 95 entries (ASCII 32-126)
	if len(font8x16) != 95 {
		t.Fatalf("expected 95 font entries, got %d", len(font8x16))
	}

	// Each glyph should be 16 bytes
	for i, glyph := range font8x16 {
		if len(glyph) != 16 {
			t.Errorf("glyph %d has %d bytes, expected 16", i, len(glyph))
		}
	}
}

func TestTextWidth(t *testing.T) {
	if w := textWidth("Hello", 1); w != 5*8 {
		t.Errorf("expected width 40, got %d", w)
	}
	if w := textWidth("Hi", 2); w != 2*8*2 {
		t.Errorf("expected width 32, got %d", w)
	}
	if w := textWidth("", 1); w != 0 {
		t.Errorf("expected width 0, got %d", w)
	}
}

func TestFillRect(t *testing.T) {
	osd := testOSD()
	c := Color{0x10, 0x20, 0x30, 0xFF}
	osd.FillRect(0, 0, 2, 2, c)

	// Check pixel at (0,0)
	if osd.buf[0] != 0x10 || osd.buf[1] != 0x20 || osd.buf[2] != 0x30 || osd.buf[3] != 0xFF {
		t.Errorf("pixel (0,0) = %v, want [0x10,0x20,0x30,0xFF]", osd.buf[0:4])
	}

	// Check pixel at (1,0)
	off := 4
	if osd.buf[off] != 0x10 || osd.buf[off+1] != 0x20 || osd.buf[off+2] != 0x30 || osd.buf[off+3] != 0xFF {
		t.Errorf("pixel (1,0) = %v, want [0x10,0x20,0x30,0xFF]", osd.buf[off:off+4])
	}

	// Check pixel at (0,1) — next row
	off = osd.stride
	if osd.buf[off] != 0x10 || osd.buf[off+1] != 0x20 || osd.buf[off+2] != 0x30 || osd.buf[off+3] != 0xFF {
		t.Errorf("pixel (0,1) = %v, want [0x10,0x20,0x30,0xFF]", osd.buf[off:off+4])
	}
}

func TestFillRectBounds(t *testing.T) {
	osd := testOSD()
	// Should not panic with out-of-bounds coordinates
	osd.FillRect(-10, -10, 20, 20, colorWhite)
	osd.FillRect(testWidth-5, testHeight-5, 20, 20, colorWhite)
}

func TestClear(t *testing.T) {
	osd := testOSD()
	osd.FillRect(0, 0, 100, 100, colorWhite)
	osd.Clear()
	for i := 0; i < 100; i++ {
		if osd.buf[i] != 0 {
			t.Fatalf("buffer not cleared at index %d: %d", i, osd.buf[i])
		}
	}
}

func TestDrawText(t *testing.T) {
	osd := testOSD()
	// Drawing 'A' should produce non-zero pixels
	osd.DrawText(0, 0, "A", colorWhite)

	hasPixel := false
	for row := 0; row < fontHeight; row++ {
		for col := 0; col < fontWidth; col++ {
			off := row*osd.stride + col*fbBPP
			if osd.buf[off] != 0 || osd.buf[off+1] != 0 || osd.buf[off+2] != 0 {
				hasPixel = true
				break
			}
		}
		if hasPixel {
			break
		}
	}
	if !hasPixel {
		t.Error("drawing 'A' produced no visible pixels")
	}
}

func TestDrawTextNonASCII(t *testing.T) {
	osd := testOSD()
	// Non-ASCII should render as '?' without panicking
	osd.DrawText(0, 0, "\x80\xff", colorWhite)
}

func TestFormatNotificationText(t *testing.T) {
	lines := FormatNotificationText("Loading...", "Sonic the Hedgehog")
	if len(lines) != 3 {
		t.Fatalf("expected 3 lines, got %d", len(lines))
	}
	if lines[0] != "Loading..." {
		t.Errorf("line 0 = %q, want %q", lines[0], "Loading...")
	}
	if lines[1] != "Sonic the Hedgehog" {
		t.Errorf("line 1 = %q, want %q", lines[1], "Sonic the Hedgehog")
	}
	if lines[2] != "MisterClaw" {
		t.Errorf("line 2 = %q, want %q", lines[2], "MisterClaw")
	}
}

func TestFlushToTempFile(t *testing.T) {
	tmp := filepath.Join(t.TempDir(), "fb0")
	// Create the file first
	if err := os.WriteFile(tmp, []byte{}, 0644); err != nil {
		t.Fatal(err)
	}

	oldDev := fbDevice
	fbDevice = tmp
	defer func() { fbDevice = oldDev }()

	osd := testOSD()
	osd.FillRect(0, 0, 10, 10, colorWhite)
	if err := osd.flush(); err != nil {
		t.Fatalf("flush to temp file: %v", err)
	}

	data, err := os.ReadFile(tmp)
	if err != nil {
		t.Fatal(err)
	}
	expectedSize := testWidth * testHeight * fbBPP
	if len(data) != expectedSize {
		t.Errorf("wrote %d bytes, expected %d", len(data), expectedSize)
	}

	// Verify pixel data
	if data[0] != 0xFF || data[1] != 0xFF || data[2] != 0xFF || data[3] != 0xFF {
		t.Errorf("pixel (0,0) = %v, want all 0xFF", data[0:4])
	}
}

func TestShowNotificationGracefulDegradation(t *testing.T) {
	oldDev := fbDevice
	fbDevice = "/dev/nonexistent_fb_test"
	defer func() { fbDevice = oldDev }()

	// Reset singleton for test
	osd := testOSD()
	// Should not panic even when fb doesn't exist
	osd.ShowNotification("Test", "subtitle", 100)
}

func TestRenderNotification(t *testing.T) {
	osd := testOSD()
	osd.renderNotification("Loading...", "Test Game (USA)")

	// The buffer should have non-zero content (the notification box)
	hasContent := false
	for i := 0; i < len(osd.buf); i += 4 {
		if osd.buf[i] != 0 || osd.buf[i+1] != 0 || osd.buf[i+2] != 0 || osd.buf[i+3] != 0 {
			hasContent = true
			break
		}
	}
	if !hasContent {
		t.Error("renderNotification produced empty buffer")
	}
}

func TestReadFBSize(t *testing.T) {
	tmp := filepath.Join(t.TempDir(), "virtual_size")
	if err := os.WriteFile(tmp, []byte("640,480\n"), 0644); err != nil {
		t.Fatal(err)
	}

	oldFile := fbSizeFile
	fbSizeFile = tmp
	defer func() { fbSizeFile = oldFile }()

	w, h := readFBSize()
	if w != 640 || h != 480 {
		t.Errorf("readFBSize() = %d,%d, want 640,480", w, h)
	}
}

func TestReadFBSizeFallback(t *testing.T) {
	oldFile := fbSizeFile
	fbSizeFile = "/nonexistent/path"
	defer func() { fbSizeFile = oldFile }()

	w, h := readFBSize()
	if w != 640 || h != 480 {
		t.Errorf("readFBSize() fallback = %d,%d, want 640,480", w, h)
	}
}
