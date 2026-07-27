package mister

import (
	"encoding/base64"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"time"
)

const screenshotDir = "/media/fat/screenshots"

var (
	screenshotRootDir           = screenshotDir
	triggerScreenshot           = TakeScreenshot
	prdpActive                  = PRDPCoreRunning
	screenshotPollInterval      = 300 * time.Millisecond
	screenshotStabilizeInterval = 200 * time.Millisecond
	screenshotStabilizeTimeout  = 3 * time.Second
)

// ScreenshotResult holds screenshot data.
type ScreenshotResult struct {
	Data      string `json:"data"`     // base64-encoded PNG
	Path      string `json:"path"`     // file path on MiSTer
	CoreName  string `json:"core"`     // core subfolder name
	FileName  string `json:"filename"` // screenshot filename
	SizeBytes int    `json:"size"`     // file size
}

// latestScreenshot finds the most recent screenshot file across all core subdirs.
func latestScreenshot() (string, error) {
	entries, err := os.ReadDir(screenshotRootDir)
	if err != nil {
		return "", fmt.Errorf("reading screenshot dir: %w", err)
	}

	var newest string
	var newestTime time.Time

	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		subdir := filepath.Join(screenshotRootDir, entry.Name())
		files, err := os.ReadDir(subdir)
		if err != nil {
			continue
		}
		for _, f := range files {
			if f.IsDir() {
				continue
			}
			info, err := f.Info()
			if err != nil {
				continue
			}
			if info.ModTime().After(newestTime) {
				newestTime = info.ModTime()
				newest = filepath.Join(subdir, f.Name())
			}
		}
	}

	if newest == "" {
		return "", fmt.Errorf("no screenshots found")
	}
	return newest, nil
}

// TakeScreenshotAndCapture triggers a screenshot and returns the result as base64.
func TakeScreenshotAndCapture(timeout time.Duration) (*ScreenshotResult, error) {
	// The pRDP core needs its own capture path: MiSTer main's screenshot
	// grabs black frames there, the real image lives in DDR3 (prdp.go).
	if prdpActive() {
		return capturePRDPScreenshot()
	}

	// Remember the current newest screenshot
	oldNewest, _ := latestScreenshot()

	// Trigger screenshot via MiSTer_cmd
	if err := triggerScreenshot(); err != nil {
		return nil, fmt.Errorf("triggering screenshot: %w", err)
	}

	// Poll for new file
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		time.Sleep(screenshotPollInterval)
		newest, err := latestScreenshot()
		if err != nil {
			continue
		}
		if newest != oldNewest && newest != "" {
			if err := waitForStableFileSize(newest, screenshotStabilizeTimeout); err != nil {
				return nil, err
			}
			return readScreenshot(newest)
		}
	}

	return nil, fmt.Errorf("screenshot not captured within timeout")
}

// ListScreenshots returns all screenshots sorted by newest first.
func ListScreenshots() ([]ScreenshotResult, error) {
	var results []ScreenshotResult

	entries, err := os.ReadDir(screenshotRootDir)
	if err != nil {
		if os.IsNotExist(err) {
			return results, nil
		}
		return nil, err
	}

	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		coreName := entry.Name()
		subdir := filepath.Join(screenshotRootDir, coreName)
		files, err := os.ReadDir(subdir)
		if err != nil {
			continue
		}
		for _, f := range files {
			if f.IsDir() {
				continue
			}
			info, err := f.Info()
			if err != nil {
				continue
			}
			results = append(results, ScreenshotResult{
				Path:      filepath.Join(subdir, f.Name()),
				CoreName:  coreName,
				FileName:  f.Name(),
				SizeBytes: int(info.Size()),
			})
		}
	}

	// Sort newest first
	sort.Slice(results, func(i, j int) bool {
		return results[i].FileName > results[j].FileName
	})

	return results, nil
}

func waitForStableFileSize(path string, timeout time.Duration) error {
	deadline := time.Now().Add(timeout)
	lastSize := int64(-1)
	stableChecks := 0

	for {
		info, err := os.Stat(path)
		if err != nil {
			return fmt.Errorf("stat screenshot: %w", err)
		}

		size := info.Size()
		if size == lastSize {
			stableChecks++
			if stableChecks >= 2 {
				return nil
			}
		} else {
			lastSize = size
			stableChecks = 0
		}

		if time.Now().After(deadline) {
			return fmt.Errorf("screenshot file did not stabilize within %s", timeout)
		}

		time.Sleep(screenshotStabilizeInterval)
	}
}

func readScreenshot(path string) (*ScreenshotResult, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("reading screenshot: %w", err)
	}

	dir := filepath.Dir(path)
	coreName := filepath.Base(dir)

	return &ScreenshotResult{
		Data:      base64.StdEncoding.EncodeToString(data),
		Path:      path,
		CoreName:  coreName,
		FileName:  filepath.Base(path),
		SizeBytes: len(data),
	}, nil
}
