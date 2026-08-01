package mister

// PRDP core support: on the pRDP GPU core the stock MiSTer screenshot
// path (shmem_map of the scaler buffer) captures black frames. The
// core's real framebuffer lives at fixed DDR3 addresses, so we read
// the PRESENTED slot directly over /dev/mem and encode the PNG
// ourselves. Address map (see pRDP tools/prdp_test/fbshot.sh, which
// this replaces):
//
//	0x31000008  frame-ACK counter; bit 0 selects the front slot
//	0x30000000  framebuffer slot 0 (ack&1 == 1 -> front)
//	0x30800000  framebuffer slot 1 (ack&1 == 0 -> front)
//
// Format: 640x480, 16bpp A1R5G5B5. Roughly 1 in 3 reads races the
// swap and catches the freshly cleared back buffer (all-black), so
// capture retries until the frame has content.

import (
	"bytes"
	"encoding/base64"
	"encoding/binary"
	"fmt"
	"image"
	"image/color"
	"image/png"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"time"
)

const (
	prdpCtlBase = 0x31000000 // page holding the ACK word (+8)
	prdpAckOff  = 0x8
	prdpFB0     = 0x30000000
	prdpFB1     = 0x30800000
	prdpWidth   = 640
	prdpHeight  = 480
	prdpFBBytes = prdpWidth * prdpHeight * 2

	prdpCaptureTries = 6
	prdpRetryDelay   = 150 * time.Millisecond
	// A real frame virtually always has content (the fleet draws a
	// stats line even on dark scenes); the cleared back buffer has
	// none. Threshold in non-black pixels (~0.15% of 307k).
	prdpMinLitPixels = 500
)

// prdpCoreNamePath is the file MiSTer main writes the running core's
// name to (var so tests can redirect it).
var prdpCoreNamePath = "/tmp/CORENAME"

// PRDPCoreRunning reports whether the pRDP core is the active core.
// The core's CONF_STR name changed to the product name "XScreenSaver"
// (2026-08-01); pre-rename cores report "PRDP" — accept both.
func PRDPCoreRunning() bool {
	b, err := os.ReadFile(prdpCoreNamePath)
	if err != nil {
		return false
	}
	name := strings.TrimSpace(string(b))
	return name == "XScreenSaver" || name == "PRDP"
}

// prdpDecodeFrame converts a raw A1R5G5B5 framebuffer dump into an
// RGBA image and reports how many pixels carry any color.
func prdpDecodeFrame(raw []byte) (*image.RGBA, int) {
	img := image.NewRGBA(image.Rect(0, 0, prdpWidth, prdpHeight))
	lit := 0
	for y := 0; y < prdpHeight; y++ {
		for x := 0; x < prdpWidth; x++ {
			p := binary.LittleEndian.Uint16(raw[2*(y*prdpWidth+x):])
			if p&0x7FFF != 0 {
				lit++
			}
			r := uint8((p >> 10) & 31)
			g := uint8((p >> 5) & 31)
			b := uint8(p & 31)
			img.SetRGBA(x, y, color.RGBA{
				R: r<<3 | r>>2,
				G: g<<3 | g>>2,
				B: b<<3 | b>>2,
				A: 255,
			})
		}
	}
	return img, lit
}

// prdpReadFrame mmaps /dev/mem and copies the currently presented
// framebuffer, retrying past swap-race black frames.
func prdpReadFrame() ([]byte, error) {
	mem, err := os.OpenFile("/dev/mem", os.O_RDONLY|syscall.O_SYNC, 0)
	if err != nil {
		return nil, fmt.Errorf("open /dev/mem: %w", err)
	}
	defer mem.Close()

	ctl, err := syscall.Mmap(int(mem.Fd()), prdpCtlBase, syscall.Getpagesize(),
		syscall.PROT_READ, syscall.MAP_SHARED)
	if err != nil {
		return nil, fmt.Errorf("mmap PRDP ctl page: %w", err)
	}
	defer syscall.Munmap(ctl)

	fb := make(map[int64][]byte)
	for _, base := range []int64{prdpFB0, prdpFB1} {
		m, err := syscall.Mmap(int(mem.Fd()), base, prdpFBBytes,
			syscall.PROT_READ, syscall.MAP_SHARED)
		if err != nil {
			return nil, fmt.Errorf("mmap PRDP fb 0x%x: %w", base, err)
		}
		defer syscall.Munmap(m)
		fb[base] = m
	}

	var last []byte
	for try := 0; try < prdpCaptureTries; try++ {
		ack := binary.LittleEndian.Uint32(ctl[prdpAckOff:])
		front := int64(prdpFB1)
		if ack&1 == 1 {
			front = prdpFB0
		}
		raw := make([]byte, prdpFBBytes)
		copy(raw, fb[front])
		last = raw

		lit := 0
		for i := 0; i < prdpFBBytes && lit < prdpMinLitPixels; i += 2 {
			if binary.LittleEndian.Uint16(raw[i:])&0x7FFF != 0 {
				lit++
			}
		}
		if lit >= prdpMinLitPixels {
			return raw, nil
		}
		time.Sleep(prdpRetryDelay)
	}
	// All tries dark: the screen may legitimately be black right now
	// (blanker scenes fade). Return the last capture instead of failing.
	return last, nil
}

// capturePRDPScreenshot grabs the presented PRDP frame, stores it as a
// PNG under the regular screenshot tree and returns it like the stock
// screenshot path does.
func capturePRDPScreenshot() (*ScreenshotResult, error) {
	raw, err := prdpReadFrame()
	if err != nil {
		return nil, err
	}
	img, _ := prdpDecodeFrame(raw)

	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		return nil, fmt.Errorf("encoding PRDP png: %w", err)
	}

	dir := filepath.Join(screenshotRootDir, "XScreenSaver")
	fileName := time.Now().Format("20060102_150405") + "-xscreensaver.png"
	path := filepath.Join(dir, fileName)
	// Best effort: the capture is still returned if the SD write fails.
	if err := os.MkdirAll(dir, 0o755); err == nil {
		_ = os.WriteFile(path, buf.Bytes(), 0o644)
	}

	return &ScreenshotResult{
		Data:      base64.StdEncoding.EncodeToString(buf.Bytes()),
		Path:      path,
		CoreName:  "XScreenSaver",
		FileName:  fileName,
		SizeBytes: buf.Len(),
	}, nil
}
