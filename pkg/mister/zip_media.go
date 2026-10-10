package mister

import (
	"archive/zip"
	"bytes"
	"encoding/binary"
	"fmt"
	"io"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strings"
	"unicode"
	"unicode/utf8"
)

// These limits apply only to virtual ZIP-member launches, never to ordinary
// files or direct NeoGeo ROM-set archives. Verification streams to io.Discard.
const (
	maxZIPArchiveBytes  = 256 << 20
	maxZIPMetadataBytes = 4 << 20
	maxZIPEntries       = 4096
	maxZIPMemberBytes   = 64 << 20
	maxZIPTotalBytes    = 512 << 20
	maxZIPPathBytes     = 1023
	maxZIPNameBytes     = 255
)

// isPhysicalZIPMemberPath classifies MGL paths without opening ZIP contents or
// verifying members. Launch validation remains mandatory. Checking the physical
// prefix prevents ordinary files inside .zip-named directories from changing
// behavior; checking the full path also preserves existing files and ROM sets.
func isPhysicalZIPMemberPath(mediaPath string) bool {
	if !filepath.IsAbs(mediaPath) {
		return false
	}
	if _, err := os.Stat(mediaPath); err == nil {
		return false
	}
	i := strings.Index(asciiLower(mediaPath), ".zip")
	if i < 0 || i+5 >= len(mediaPath) || mediaPath[i+4] != '/' {
		return false
	}
	info, err := os.Lstat(mediaPath[:i+4])
	return err == nil && info.Mode().IsRegular()
}

// ValidateGameMedia checks the launch media without extracting any ZIP member.
// Physical files retain the existing launch semantics, including NeoGeo ZIPs.
func ValidateGameMedia(game GameInfo) error {
	if _, err := os.Stat(game.Path); err == nil {
		return nil
	}
	if strings.Contains(asciiLower(game.Path), ".zip") {
		return ValidateZIPGame(game)
	}
	return fmt.Errorf("ROM not found: %s", game.Path)
}

func supportsDirectZIP(system string) bool {
	cfg, ok := getDefaultConfig(system)
	if !ok {
		return false
	}
	for _, ext := range cfg.Extensions {
		if strings.EqualFold(ext, ".zip") {
			return true
		}
	}
	return false
}

// ValidateZIPGame validates virtual archive.zip/member paths before a launch
// callback can run. Existing filesystem paths retain their previous behavior.
// Non-ZIP paths are left to LaunchGame's existing filesystem/MRA validation.
func ValidateZIPGame(game GameInfo) error {
	if _, err := os.Stat(game.Path); err == nil {
		return nil
	}
	lower := asciiLower(game.Path)
	i := strings.Index(lower, ".zip")
	if i < 0 {
		return nil
	}
	// Main's FileIsZipped splits at the FIRST .zip substring, not the last
	// archive component. Do not normalize an ambiguous Linux path into one
	// that Main would interpret differently.
	if i+4 >= len(game.Path) || game.Path[i+4] != '/' {
		return fmt.Errorf("ZIP launch requires an explicit archive.zip/member path")
	}
	archivePath, member := game.Path[:i+4], game.Path[i+5:]
	if !filepath.IsAbs(archivePath) || filepath.Clean(archivePath) != archivePath ||
		len(archivePath) > maxZIPNameBytes || len(game.Path) > maxZIPPathBytes ||
		!safeZIPName(strings.TrimPrefix(archivePath, "/"), false) || !safeZIPName(member, false) ||
		strings.Contains(asciiLower(member), ".zip") {
		return fmt.Errorf("unsafe or unsupported ZIP member path")
	}
	// Main's MGL parser does not decode XML entities. Apostrophes are already
	// preserved by GenerateMGL, but other attribute escapes would corrupt this
	// new path type. Ordinary file behavior is deliberately unchanged.
	if strings.ContainsAny(game.Path, "&<>\"") {
		return fmt.Errorf("ZIP member path contains characters unsupported by MiSTer's MGL parser")
	}
	cfg, ok := GetSystemConfig(game.System)
	if !ok || cfg.Core == "" {
		return fmt.Errorf("unknown system: %s", game.System)
	}
	if err := validateZIPProfile(cfg, game.System, member); err != nil {
		return err
	}
	info, err := os.Lstat(archivePath)
	if err != nil || !info.Mode().IsRegular() {
		return fmt.Errorf("ZIP archive is missing or not a regular file: %s", archivePath)
	}
	resolved, err := filepath.EvalSymlinks(archivePath)
	if err != nil || resolved != archivePath {
		return fmt.Errorf("ZIP archive path must not contain symlinks")
	}
	f, err := os.Open(archivePath)
	if err != nil {
		return fmt.Errorf("opening ZIP archive: %w", err)
	}
	defer f.Close()
	opened, err := f.Stat()
	if err != nil || !opened.Mode().IsRegular() || !os.SameFile(info, opened) {
		return fmt.Errorf("ZIP archive changed while opening")
	}
	if err := validateZIPMember(f, opened.Size(), member); err != nil {
		return fmt.Errorf("invalid ZIP member: %w", err)
	}
	current, err := f.Stat()
	linked, linkErr := os.Lstat(archivePath)
	if err != nil || linkErr != nil || !os.SameFile(opened, linked) ||
		!linked.Mode().IsRegular() || current.Size() != opened.Size() ||
		!current.ModTime().Equal(opened.ModTime()) {
		return fmt.Errorf("ZIP archive changed during validation")
	}
	return nil
}

func validateZIPProfile(cfg SystemConfig, system, member string) error {
	ext := strings.ToLower(path.Ext(member))
	exts := cfg.Extensions
	// Discovery reports the physical folder's extensions (possibly only .zip),
	// not the member formats supported by a known core's launch profile.
	if defaults, ok := getDefaultConfig(system); ok {
		exts = defaults.Extensions
	}
	allowed := false
	for _, candidate := range exts {
		allowed = allowed || strings.EqualFold(candidate, ext)
	}
	loader, index := cfg.Type, cfg.Index
	if override := getFormatOverride(cfg, member); override != nil {
		loader, index = override.Type, override.Index
		for _, candidate := range override.Extensions {
			allowed = allowed || strings.EqualFold(candidate, ext)
		}
	}
	if !allowed || ext == ".zip" {
		return fmt.Errorf("ZIP member extension %q is not supported by system %s", ext, system)
	}
	if loader != "f" || index < 0 || index > 255 || isDiskMediaExtension(ext) {
		return fmt.Errorf("ZIP members require a compatible file-injection loader, not disk/CD/VHD streams")
	}
	return nil
}

func isDiskMediaExtension(ext string) bool {
	switch ext {
	case ".adf", ".hdf", ".d64", ".g64", ".d81", ".dsk", ".img", ".vhd", ".vhdx", ".qcow2",
		".chd", ".cue", ".iso", ".ccd", ".mdf", ".nrg", ".st", ".msa", ".stx", ".d88", ".dim", ".nib", ".ssd", ".dsd":
		return true
	}
	return false
}

// asciiLower matches Main/miniz's ASCII case-insensitive archive lookup without
// changing byte offsets or applying unrelated Unicode case-folding rules.
func asciiLower(s string) string {
	b := []byte(s)
	for i, c := range b {
		if c >= 'A' && c <= 'Z' {
			b[i] = c + ('a' - 'A')
		}
	}
	return string(b)
}

func safeZIPName(name string, directory bool) bool {
	if directory {
		name = strings.TrimSuffix(name, "/")
	}
	if name == "" || len(name) > maxZIPPathBytes || !utf8.ValidString(name) ||
		strings.ContainsAny(name, "\\:") || path.IsAbs(name) || path.Clean(name) != name {
		return false
	}
	for _, part := range strings.Split(name, "/") {
		if part == ".." || part == "." || part == "" || len(part) > maxZIPNameBytes {
			return false
		}
	}
	for _, r := range name {
		if unicode.IsControl(r) {
			return false
		}
	}
	return true
}

// preflightZIP bounds metadata BEFORE archive/zip allocates headers. Go's ZIP
// reader reads past the declared central-directory size and compares entry
// counts modulo 65536, so checking only len(reader.File) afterwards is unsafe.
// Require a conventional, single-disk, non-ZIP64 layout with exact bounds.
func preflightZIP(r io.ReaderAt, size int64) (int64, []int64, error) {
	if size < 22 || size > maxZIPArchiveBytes {
		return 0, nil, fmt.Errorf("archive size exceeds limits or is truncated")
	}
	tailSize := size
	if tailSize > 22+65535 {
		tailSize = 22 + 65535
	}
	tail := make([]byte, int(tailSize))
	if _, err := r.ReadAt(tail, size-tailSize); err != nil {
		return 0, nil, err
	}
	i := bytes.LastIndex(tail, []byte{'P', 'K', 5, 6})
	if i < 0 || i+22 > len(tail) {
		return 0, nil, fmt.Errorf("missing ZIP end record")
	}
	end := tail[i:]
	u16 := binary.LittleEndian.Uint16
	u32 := binary.LittleEndian.Uint32
	count := int(u16(end[10:12]))
	directorySize := int64(u32(end[12:16]))
	directoryOffset := int64(u32(end[16:20]))
	endOffset := size - tailSize + int64(i)
	if int(u16(end[20:22]))+22 != len(end) || u16(end[4:6]) != 0 || u16(end[6:8]) != 0 ||
		int(u16(end[8:10])) != count || count == 0 || count > maxZIPEntries ||
		directorySize > maxZIPMetadataBytes || directorySize < int64(count)*46 ||
		directoryOffset < 30 || directoryOffset+directorySize != endOffset {
		return 0, nil, fmt.Errorf("unsupported ZIP layout or metadata limits exceeded")
	}
	central := make([]byte, int(directorySize))
	if _, err := r.ReadAt(central, directoryOffset); err != nil {
		return 0, nil, err
	}
	offsets := make([]int64, 0, count)
	pos := 0
	for n := 0; n < count; n++ {
		if pos+46 > len(central) || !bytes.Equal(central[pos:pos+4], []byte{'P', 'K', 1, 2}) {
			return 0, nil, fmt.Errorf("malformed ZIP central directory")
		}
		h := central[pos : pos+46]
		nameLen, extraLen, commentLen := int(u16(h[28:30])), int(u16(h[30:32])), int(u16(h[32:34]))
		length := 46 + nameLen + extraLen + commentLen
		if nameLen == 0 || nameLen > maxZIPPathBytes || pos+length > len(central) ||
			u16(h[34:36]) != 0 || int64(u32(h[42:46])) >= directoryOffset ||
			u32(h[20:24]) == 0xffffffff || u32(h[24:28]) == 0xffffffff {
			return 0, nil, fmt.Errorf("invalid ZIP entry bounds or ZIP64 entry")
		}
		if err := checkZIPExtra(central[pos+46+nameLen : pos+46+nameLen+extraLen]); err != nil {
			return 0, nil, err
		}
		offsets = append(offsets, int64(u32(h[42:46])))
		pos += length
	}
	if pos != len(central) {
		return 0, nil, fmt.Errorf("ZIP directory size/count mismatch")
	}
	var signature [4]byte
	if _, err := r.ReadAt(signature[:], 0); err != nil || signature != [4]byte{'P', 'K', 3, 4} {
		return 0, nil, fmt.Errorf("prefixed/self-extracting ZIP archives are unsupported")
	}
	return directoryOffset, offsets, nil
}

func checkZIPExtra(extra []byte) error {
	for len(extra) != 0 {
		if len(extra) < 4 {
			return fmt.Errorf("malformed ZIP extra metadata")
		}
		id := binary.LittleEndian.Uint16(extra[:2])
		n := int(binary.LittleEndian.Uint16(extra[2:4]))
		if id == 1 || n > len(extra)-4 {
			return fmt.Errorf("ZIP64 or malformed ZIP extra metadata is unsupported")
		}
		extra = extra[4+n:]
	}
	return nil
}

func validateZIPMember(r io.ReaderAt, size int64, member string) error {
	directoryOffset, offsets, err := preflightZIP(r, size)
	if err != nil {
		return err
	}
	// Freeze the bounded directory/end bytes so a concurrently modified
	// archive cannot bypass preflight and trigger unbounded ZIP allocations.
	metadata := make([]byte, int(size-directoryOffset))
	if _, err := r.ReadAt(metadata, directoryOffset); err != nil {
		return err
	}
	frozenOffset := directoryOffset
	r = zipMetadataReader{source: r, offset: frozenOffset, metadata: bytes.NewReader(metadata)}
	directoryOffset, offsets, err = preflightZIP(r, size)
	if err != nil {
		return err
	}
	if directoryOffset != frozenOffset {
		return fmt.Errorf("ZIP directory changed during metadata validation")
	}
	zr, err := zip.NewReader(r, size)
	if err != nil {
		return err
	}
	count := len(offsets)
	if len(zr.File) != count {
		return fmt.Errorf("ZIP entry count mismatch")
	}
	seen := make(map[string]bool, count)
	type span struct{ start, end int64 }
	spans := make([]span, 0, count)
	var selected *zip.File
	var total uint64
	localMetadata := int64(0)
	for i, entry := range zr.File {
		directory := entry.FileInfo().IsDir()
		if !safeZIPName(entry.Name, directory) || entry.Mode()&os.ModeSymlink != 0 ||
			(!directory && !entry.Mode().IsRegular()) {
			return fmt.Errorf("unsafe or nonregular ZIP entry %q", entry.Name)
		}
		key := asciiLower(strings.TrimSuffix(entry.Name, "/"))
		if seen[key] {
			return fmt.Errorf("duplicate or case-ambiguous ZIP entry %q", entry.Name)
		}
		seen[key] = true
		if entry.Flags & ^uint16(0x080e) != 0 || (entry.Method != zip.Store && entry.Method != zip.Deflate) {
			return fmt.Errorf("encrypted or unsupported ZIP entry %q", entry.Name)
		}
		if entry.UncompressedSize64 > maxZIPMemberBytes || entry.CompressedSize64 > maxZIPArchiveBytes ||
			(entry.UncompressedSize64 > 0 && entry.CompressedSize64 == 0) {
			return fmt.Errorf("ZIP member expansion limits exceeded")
		}
		total += entry.UncompressedSize64
		if total > maxZIPTotalBytes {
			return fmt.Errorf("ZIP total expansion limit exceeded")
		}
		dataOffset, err := entry.DataOffset()
		if err != nil {
			return err
		}
		// archive/zip trusts central metadata. Check the actual local header's
		// name, flags, method and bounds as well, before reading any payload.
		local, dataEnd, err := validateZIPLocal(r, entry, offsets[i], dataOffset, directoryOffset)
		if err != nil {
			return err
		}
		localMetadata += int64(local)
		if localMetadata > maxZIPMetadataBytes {
			return fmt.Errorf("ZIP local metadata limit exceeded")
		}
		spans = append(spans, span{offsets[i], dataEnd})
		if entry.Name == member && !directory {
			selected = entry
		}
	}
	sort.Slice(spans, func(i, j int) bool { return spans[i].start < spans[j].start })
	for i := 1; i < len(spans); i++ {
		if spans[i].start < spans[i-1].end {
			return fmt.Errorf("overlapping ZIP entries")
		}
	}
	if selected == nil {
		return fmt.Errorf("exact regular ZIP member %q not found", member)
	}
	if selected.UncompressedSize64 == 0 {
		return fmt.Errorf("ZIP member is empty")
	}
	reader, err := selected.Open()
	if err != nil {
		return err
	}
	defer reader.Close()
	// One extra byte forces EOF/CRC verification while bounding even a forged
	// uncompressed-size header. Nothing is extracted to the filesystem.
	n, err := io.Copy(io.Discard, io.LimitReader(reader, int64(selected.UncompressedSize64)+1))
	if err != nil || uint64(n) != selected.UncompressedSize64 {
		return fmt.Errorf("ZIP member CRC/size verification failed: bytes=%d, error=%v", n, err)
	}
	return nil
}

func validateZIPLocal(r io.ReaderAt, entry *zip.File, offset, dataOffset, directoryOffset int64) (int, int64, error) {
	var h [30]byte
	if offset < 0 || offset+30 > directoryOffset {
		return 0, 0, fmt.Errorf("invalid local ZIP header offset")
	}
	if _, err := r.ReadAt(h[:], offset); err != nil {
		return 0, 0, err
	}
	u16 := binary.LittleEndian.Uint16
	u32 := binary.LittleEndian.Uint32
	nameLen, extraLen := int(u16(h[26:28])), int(u16(h[28:30]))
	length := 30 + nameLen + extraLen
	if !bytes.Equal(h[:4], []byte{'P', 'K', 3, 4}) ||
		u16(h[6:8]) != entry.Flags || u16(h[8:10]) != entry.Method ||
		nameLen != len(entry.Name) || extraLen > maxZIPMetadataBytes ||
		offset+int64(length) != dataOffset || dataOffset > directoryOffset ||
		entry.CompressedSize64 > uint64(directoryOffset-dataOffset) {
		return 0, 0, fmt.Errorf("local/central ZIP header mismatch or invalid bounds")
	}
	metadata := make([]byte, nameLen+extraLen)
	if _, err := r.ReadAt(metadata, offset+30); err != nil {
		return 0, 0, err
	}
	if string(metadata[:nameLen]) != entry.Name {
		return 0, 0, fmt.Errorf("local/central ZIP member name mismatch")
	}
	if err := checkZIPExtra(metadata[nameLen:]); err != nil {
		return 0, 0, err
	}
	crc, compressed, uncompressed := u32(h[14:18]), u32(h[18:22]), u32(h[22:26])
	if entry.Flags&8 == 0 {
		if crc != entry.CRC32 || uint64(compressed) != entry.CompressedSize64 || uint64(uncompressed) != entry.UncompressedSize64 {
			return 0, 0, fmt.Errorf("local/central ZIP CRC or size mismatch")
		}
	} else if (crc != 0 && crc != entry.CRC32) || (compressed != 0 && uint64(compressed) != entry.CompressedSize64) ||
		(uncompressed != 0 && uint64(uncompressed) != entry.UncompressedSize64) {
		return 0, 0, fmt.Errorf("invalid local ZIP data-descriptor metadata")
	}
	dataEnd := dataOffset + int64(entry.CompressedSize64)
	if entry.Flags&8 != 0 {
		var descriptor [16]byte
		if dataEnd+12 > directoryOffset {
			return 0, 0, fmt.Errorf("truncated ZIP data descriptor")
		}
		if _, err := r.ReadAt(descriptor[:12], dataEnd); err != nil {
			return 0, 0, err
		}
		fields := descriptor[:12]
		descriptorSize := int64(12)
		if u32(descriptor[:4]) == 0x08074b50 {
			if dataEnd+16 > directoryOffset {
				return 0, 0, fmt.Errorf("truncated ZIP data descriptor")
			}
			if _, err := r.ReadAt(descriptor[:], dataEnd); err != nil {
				return 0, 0, err
			}
			fields = descriptor[4:]
			descriptorSize = 16
		}
		if u32(fields[:4]) != entry.CRC32 || uint64(u32(fields[4:8])) != entry.CompressedSize64 ||
			uint64(u32(fields[8:12])) != entry.UncompressedSize64 {
			return 0, 0, fmt.Errorf("ZIP data descriptor CRC/size mismatch")
		}
		dataEnd += descriptorSize
	}
	return length, dataEnd, nil
}

// zipMetadataReader keeps payload reads on the physical archive while serving
// all central-directory and end-record reads from an immutable bounded snapshot.
type zipMetadataReader struct {
	source   io.ReaderAt
	offset   int64
	metadata *bytes.Reader
}

func (r zipMetadataReader) ReadAt(p []byte, offset int64) (int, error) {
	if offset >= r.offset {
		return r.metadata.ReadAt(p, offset-r.offset)
	}
	if int64(len(p)) <= r.offset-offset {
		return r.source.ReadAt(p, offset)
	}
	prefix := int(r.offset - offset)
	n, err := r.source.ReadAt(p[:prefix], offset)
	if err != nil {
		return n, err
	}
	m, err := r.metadata.ReadAt(p[prefix:], 0)
	return n + m, err
}
