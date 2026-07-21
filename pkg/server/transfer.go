package server

import (
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"fmt"
	"hash"
	"io"
	"os"
	"path/filepath"
)

// transferChunk is the raw byte count per push/pull message. 256 KB
// base64-encodes to ~344 KB, comfortably under the 1 MB scanner line limit
// that rules out shipping a multi-MB file as one base64 blob (task #36).
const transferChunk = 256 * 1024

// pushState accumulates an in-progress host->device transfer for ONE
// connection. Sequence: begin (Path/Size/Sha256) -> N chunks -> done. Bytes
// land in a temp file in the TARGET directory, get SHA256-verified, then
// os.Rename'd into place — so an interrupted or corrupt transfer never leaves
// a partial/garbage file where a valid one is expected (the "0-byte RBF
// install" class of failure this op exists to kill).
type pushState struct {
	target  string
	tmp     *os.File
	tmpName string
	expSize int64
	written int64
	expHash string
	hasher  hash.Hash
	failed  error
}

// beginPush opens the temp file (in the target dir, for an atomic rename) and
// returns the accumulator, or nil on error (which it reports to the client).
func beginPush(req Request, send func(interface{})) *pushState {
	if req.Path == "" {
		send(map[string]interface{}{"error": "push requires path"})
		return nil
	}
	dir := filepath.Dir(req.Path)
	tmp, err := os.CreateTemp(dir, ".mcpush-*")
	if err != nil {
		send(map[string]interface{}{"error": fmt.Sprintf("push: cannot create temp in %s: %v", dir, err)})
		return nil
	}
	send(map[string]interface{}{"push": true, "ready": true})
	return &pushState{
		target:  req.Path,
		tmp:     tmp,
		tmpName: tmp.Name(),
		expSize: req.Size,
		expHash: req.Sha256,
		hasher:  sha256.New(),
	}
}

// writeChunk decodes and appends one base64 chunk (nil/failed-safe).
func (ps *pushState) writeChunk(b64 string) {
	if ps == nil || ps.failed != nil {
		return
	}
	data, err := base64.StdEncoding.DecodeString(b64)
	if err != nil {
		ps.failed = fmt.Errorf("bad base64 chunk: %w", err)
		return
	}
	n, err := ps.tmp.Write(data)
	if err != nil {
		ps.failed = fmt.Errorf("writing temp file: %w", err)
		return
	}
	ps.hasher.Write(data[:n])
	ps.written += int64(n)
}

// finish closes the temp file, verifies size + SHA256, and on success renames
// it atomically over the target. On any failure the temp file is removed.
func (ps *pushState) finish(send func(interface{})) {
	if ps == nil {
		send(map[string]interface{}{"error": "push_done without an active push"})
		return
	}
	defer ps.cleanup() // no-op after a successful rename clears tmpName
	ps.tmp.Close()

	switch {
	case ps.failed != nil:
		send(map[string]interface{}{"push": true, "success": false, "error": ps.failed.Error()})
	case ps.expSize > 0 && ps.written != ps.expSize:
		send(map[string]interface{}{"push": true, "success": false,
			"error": fmt.Sprintf("size mismatch: received %d bytes, expected %d", ps.written, ps.expSize)})
	default:
		got := hex.EncodeToString(ps.hasher.Sum(nil))
		if ps.expHash != "" && got != ps.expHash {
			send(map[string]interface{}{"push": true, "success": false,
				"error": fmt.Sprintf("sha256 mismatch: got %s, expected %s (transfer corrupted)", got, ps.expHash)})
			return
		}
		if err := os.Rename(ps.tmpName, ps.target); err != nil {
			send(map[string]interface{}{"push": true, "success": false,
				"error": fmt.Sprintf("rename into place: %v", err)})
			return
		}
		ps.tmpName = "" // committed; cleanup becomes a no-op
		send(map[string]interface{}{"push": true, "success": true, "size": ps.written, "sha256": got})
	}
}

// cleanup closes and removes the temp file if the push did not commit. Safe to
// call on nil and more than once (e.g. from handleConn's defer on a dropped
// connection AND from finish).
func (ps *pushState) cleanup() {
	if ps == nil || ps.tmpName == "" {
		return
	}
	ps.tmp.Close()
	os.Remove(ps.tmpName)
	ps.tmpName = ""
}

// handlePull streams a device file to the host as base64 chunks, ending with a
// trailer carrying the total size + SHA256 for the client to verify.
func (s *Server) handlePull(req Request, send func(interface{})) {
	if req.Path == "" {
		send(map[string]interface{}{"error": "pull requires path"})
		return
	}
	f, err := os.Open(req.Path)
	if err != nil {
		send(map[string]interface{}{"error": fmt.Sprintf("pull: %v", err)})
		return
	}
	defer f.Close()

	hasher := sha256.New()
	buf := make([]byte, transferChunk)
	var total int64
	for {
		n, rerr := f.Read(buf)
		if n > 0 {
			hasher.Write(buf[:n])
			total += int64(n)
			send(map[string]interface{}{"pull_data": base64.StdEncoding.EncodeToString(buf[:n])})
		}
		if rerr == io.EOF {
			break
		}
		if rerr != nil {
			send(map[string]interface{}{"error": fmt.Sprintf("pull read: %v", rerr)})
			return
		}
	}
	send(map[string]interface{}{"pull_done": true, "size": total, "sha256": hex.EncodeToString(hasher.Sum(nil))})
}
