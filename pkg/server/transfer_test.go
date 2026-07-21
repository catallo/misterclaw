package server

import (
	"bufio"
	"bytes"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func readMsg(t *testing.T, sc *bufio.Scanner) map[string]interface{} {
	t.Helper()
	if !sc.Scan() {
		t.Fatalf("no response: %v", sc.Err())
	}
	var m map[string]interface{}
	if err := json.Unmarshal(sc.Bytes(), &m); err != nil {
		t.Fatalf("unmarshal: %v (raw %s)", err, sc.Text())
	}
	return m
}

func hashOf(b []byte) string {
	s := sha256.Sum256(b)
	return hex.EncodeToString(s[:])
}

// TestPushRoundTrip pushes a multi-chunk payload and checks it lands intact.
func TestPushRoundTrip(t *testing.T) {
	addr, _ := startTestServer(t)
	conn, sc := dial(t, addr)

	payload := make([]byte, 600*1024) // spans 3 chunks (256K+256K+88K)
	for i := range payload {
		payload[i] = byte(i * 31)
	}
	target := filepath.Join(t.TempDir(), "sub", "pushed.bin")
	if err := os.MkdirAll(filepath.Dir(target), 0755); err != nil {
		t.Fatal(err)
	}

	fmt.Fprintf(conn, `{"push":true,"path":%q,"size":%d,"sha256":%q}`+"\n", target, len(payload), hashOf(payload))
	if ready := readMsg(t, sc); ready["ready"] != true {
		t.Fatalf("expected ready, got %v", ready)
	}
	for off := 0; off < len(payload); off += transferChunk {
		end := off + transferChunk
		if end > len(payload) {
			end = len(payload)
		}
		fmt.Fprintf(conn, `{"push_data":%q}`+"\n", base64.StdEncoding.EncodeToString(payload[off:end]))
	}
	fmt.Fprintf(conn, `{"push_done":true}`+"\n")
	if res := readMsg(t, sc); res["success"] != true {
		t.Fatalf("push failed: %v", res)
	}

	got, err := os.ReadFile(target)
	if err != nil {
		t.Fatalf("read target: %v", err)
	}
	if !bytes.Equal(got, payload) {
		t.Errorf("target content mismatch: got %d bytes, want %d", len(got), len(payload))
	}
}

// TestPushHashMismatch verifies a corrupt transfer is rejected and leaves
// neither the target nor a temp file behind.
func TestPushHashMismatch(t *testing.T) {
	addr, _ := startTestServer(t)
	conn, sc := dial(t, addr)

	payload := []byte("hello world")
	dir := t.TempDir()
	target := filepath.Join(dir, "corrupt.bin")

	fmt.Fprintf(conn, `{"push":true,"path":%q,"size":%d,"sha256":%q}`+"\n", target, len(payload), "deadbeef")
	readMsg(t, sc) // ready
	fmt.Fprintf(conn, `{"push_data":%q}`+"\n", base64.StdEncoding.EncodeToString(payload))
	fmt.Fprintf(conn, `{"push_done":true}`+"\n")

	if res := readMsg(t, sc); res["success"] == true {
		t.Fatal("expected failure on hash mismatch")
	}
	if _, err := os.Stat(target); !os.IsNotExist(err) {
		t.Errorf("target must not exist after a failed push")
	}
	entries, _ := os.ReadDir(dir)
	for _, e := range entries {
		if strings.HasPrefix(e.Name(), ".mcpush-") {
			t.Errorf("temp file left behind: %s", e.Name())
		}
	}
}

// TestPullRoundTrip streams a device file to the "host" side and verifies the
// reconstructed bytes + trailer hash.
func TestPullRoundTrip(t *testing.T) {
	addr, _ := startTestServer(t)
	conn, sc := dial(t, addr)

	payload := make([]byte, 300*1024)
	for i := range payload {
		payload[i] = byte(i * 7)
	}
	src := filepath.Join(t.TempDir(), "src.bin")
	if err := os.WriteFile(src, payload, 0644); err != nil {
		t.Fatal(err)
	}

	fmt.Fprintf(conn, `{"pull":true,"path":%q}`+"\n", src)
	var got []byte
	var trailerHash string
	var trailerSize int64
	for {
		m := readMsg(t, sc)
		if e, ok := m["error"].(string); ok && e != "" {
			t.Fatalf("pull error: %s", e)
		}
		if d, ok := m["pull_data"].(string); ok {
			raw, err := base64.StdEncoding.DecodeString(d)
			if err != nil {
				t.Fatalf("bad chunk: %v", err)
			}
			got = append(got, raw...)
			continue
		}
		if m["pull_done"] == true {
			trailerHash, _ = m["sha256"].(string)
			if s, ok := m["size"].(float64); ok {
				trailerSize = int64(s)
			}
			break
		}
	}
	if !bytes.Equal(got, payload) {
		t.Errorf("pull content mismatch: got %d bytes", len(got))
	}
	if trailerHash != hashOf(payload) {
		t.Errorf("pull trailer hash mismatch")
	}
	if trailerSize != int64(len(payload)) {
		t.Errorf("pull trailer size = %d, want %d", trailerSize, len(payload))
	}
}
