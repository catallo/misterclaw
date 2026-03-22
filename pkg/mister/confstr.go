package mister

import (
	"encoding/json"
	"fmt"
	"os"
	"strconv"
	"strings"
	"sync"
)

// ConfStrDB holds the parsed CONF_STR database for all known cores.
type ConfStrDB struct {
	Cores   []CoreOSD `json:"cores"`
	Version string    `json:"version,omitempty"`
}

// CoreOSD represents one core's parsed OSD menu structure.
type CoreOSD struct {
	CoreName   string     `json:"core_name"`
	RbfName    string     `json:"rbf_name,omitempty"`
	Repo       string     `json:"repo"`
	ConfStrRaw string     `json:"conf_str_raw"`
	Menu       []MenuItem `json:"menu"`
}

// HideCondition represents a visibility/enable condition from H/h/D/d prefixes.
type HideCondition struct {
	Bit      int    `json:"bit"`
	Type     string `json:"type"`     // "hide" or "disable"
	Inverted bool   `json:"inverted"` // h/d = inverted (hide/disable when bit=0)
}

// MenuItem represents a single parsed CONF_STR menu entry.
type MenuItem struct {
	Type           string          `json:"type"`
	Raw            string          `json:"raw"`
	Name           string          `json:"name,omitempty"`
	Bit            int             `json:"bit,omitempty"`
	BitHigh        int             `json:"bit_high,omitempty"`
	Values         []string        `json:"values,omitempty"`
	Extensions     []string        `json:"extensions,omitempty"`
	Label          string          `json:"label,omitempty"`
	Index          int             `json:"index,omitempty"`
	PageID         int             `json:"page_id,omitempty"`
	Default        int             `json:"default,omitempty"`
	HideConditions []HideCondition `json:"hide_conditions,omitempty"`
}

// Visible returns whether this menu item is visible given the current CFG data.
// Items with no hide conditions are always visible.
// H[bit] = hide when bit=1, h[bit] = hide when bit=0
func (m *MenuItem) Visible(cfgData []byte) bool {
	for _, cond := range m.HideConditions {
		if cond.Type != "hide" {
			continue // disable conditions don't affect visibility
		}
		bitSet := GetBit(cfgData, cond.Bit)
		if cond.Inverted {
			// h = hide when bit is 0
			if !bitSet {
				return false
			}
		} else {
			// H = hide when bit is 1
			if bitSet {
				return false
			}
		}
	}
	return true
}

// Enabled returns whether this menu item is enabled (not grayed out) given CFG data.
func (m *MenuItem) Enabled(cfgData []byte) bool {
	for _, cond := range m.HideConditions {
		if cond.Type != "disable" {
			continue
		}
		bitSet := GetBit(cfgData, cond.Bit)
		if cond.Inverted {
			if !bitSet {
				return false
			}
		} else {
			if bitSet {
				return false
			}
		}
	}
	return true
}

// ParseConfStr parses a raw CONF_STR semicolon-delimited string into menu items.
// Format: "CORE_NAME;OPT1;OPT2;..." — first item is the core display name.
func ParseConfStr(raw string) []MenuItem {
	// Split on semicolons, trim whitespace
	parts := strings.Split(raw, ";")
	if len(parts) == 0 {
		return nil
	}

	var items []MenuItem
	for _, part := range parts {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		item := parseMenuItem(part)
		if item != nil {
			items = append(items, *item)
		}
	}
	return items
}

// parseMenuItem parses a single CONF_STR entry.
func parseMenuItem(entry string) *MenuItem {
	if entry == "" {
		return nil
	}

	// Separator line
	if entry == "-" {
		return &MenuItem{Type: "separator", Raw: entry}
	}

	// DIP switch block
	if entry == "DIP" {
		return &MenuItem{Type: "dip", Raw: entry}
	}

	prefix := entry[0]
	rest := ""
	if len(entry) > 1 {
		rest = entry[1:]
	}

	// Commands are a single letter followed by a digit, comma, or specific char.
	// Multi-letter words (e.g. "SNES", "ACTIVE") are labels, not commands.
	// Check: if entry has 2+ chars and second char is an uppercase letter (A-Z)
	// that isn't a valid bit specifier position, treat as label.
	if len(entry) > 1 && isCommandPrefix(prefix) && isUpperAlpha(entry[1]) && !isValidCommandStart(entry) {
		return &MenuItem{Type: "label", Raw: entry, Name: entry}
	}

	switch prefix {
	case 'O', 'o':
		return parseOption(entry, rest, prefix == 'o')
	case 'T', 't':
		return parseTrigger(entry, rest, prefix == 't')
	case 'F':
		return parseFileLoad(entry, rest)
	case 'S':
		return parseFileLoad(entry, rest) // S is mount (SD image), similar format
	case 'P':
		return parseSubPage(entry, rest)
	case 'R':
		return parseReset(entry, rest)
	case 'C':
		return parseCheat(entry, rest)
	case 'H', 'h':
		return parseHideDisable(entry, rest, "hide", prefix == 'h')
	case 'D', 'd':
		return parseHideDisable(entry, rest, "disable", prefix == 'd')
	case 'I':
		return parseInfo(entry, rest)
	case 'V':
		return parseVersion(entry, rest)
	case 'J':
		return parseJoystick(entry, rest)
	default:
		// Could be a core name or unknown entry — treat as label
		return &MenuItem{Type: "label", Raw: entry, Name: entry}
	}
}

// isCommandPrefix returns true if the byte is a known CONF_STR command letter.
func isCommandPrefix(b byte) bool {
	switch b {
	case 'O', 'o', 'T', 't', 'F', 'S', 'P', 'R', 'C', 'H', 'h', 'D', 'd', 'I', 'V', 'J':
		return true
	}
	return false
}

// isUpperAlpha returns true if b is A-Z.
func isUpperAlpha(b byte) bool {
	return b >= 'A' && b <= 'Z'
}

// isValidCommandStart checks if an entry starting with a command prefix is actually
// a command (not a multi-letter label like "SNES", "ACTIVE", "CORE").
// Commands: O/o followed by digit/A-V, T/t followed by digit, F followed by digit/C/comma,
// S followed by digit/comma, P followed by digit, H/h/D/d followed by digit, etc.
func isValidCommandStart(entry string) bool {
	if len(entry) < 2 {
		return true
	}
	second := entry[1]
	switch entry[0] {
	case 'O', 'o':
		// O followed by digit or A-V (bit specifier)
		return (second >= '0' && second <= '9') || (second >= 'A' && second <= 'V')
	case 'T', 't':
		return second >= '0' && second <= '9'
	case 'F':
		return second >= '0' && second <= '9' || second == 'C' || second == 'S' || second == ','
	case 'S':
		return second >= '0' && second <= '9' || second == ','
	case 'P':
		return second >= '0' && second <= '9'
	case 'R':
		return second >= '0' && second <= '9' || second == ','
	case 'H', 'h':
		return second >= '0' && second <= '9'
	case 'D', 'd':
		return second >= '0' && second <= '9'
	}
	return true
}

// parseOption parses O/o entries: O[bits],[name],[val1],[val2],...
// Bits can be: single digit, two digits (range), or letter-based (A-V = 10-31).
func parseOption(raw, rest string, hidden bool) *MenuItem {
	parts := strings.SplitN(rest, ",", -1)
	if len(parts) < 2 {
		return &MenuItem{Type: "option", Raw: raw, Name: rest}
	}

	bitStr := parts[0]
	name := parts[1]
	values := parts[2:]

	low, high := parseBitRange(bitStr)

	item := &MenuItem{
		Type:    "option",
		Raw:     raw,
		Name:    name,
		Bit:     low,
		BitHigh: high,
		Values:  values,
	}
	if hidden {
		item.Type = "option_hidden"
	}
	return item
}

// parseBitRange parses CONF_STR bit specifiers.
// Single char: "3" -> (3,3), two chars: "89" -> (8,9), range with letters: "AB" -> (10,11).
func parseBitRange(s string) (int, int) {
	if len(s) == 0 {
		return 0, 0
	}

	// Parse each character as a bit number
	// 0-9 = 0-9, A-V = 10-31
	bits := make([]int, 0, len(s))
	for _, c := range s {
		switch {
		case c >= '0' && c <= '9':
			bits = append(bits, int(c-'0'))
		case c >= 'A' && c <= 'V':
			bits = append(bits, int(c-'A'+10))
		case c >= 'a' && c <= 'v':
			bits = append(bits, int(c-'a'+10))
		}
	}

	if len(bits) == 0 {
		return 0, 0
	}
	if len(bits) == 1 {
		return bits[0], bits[0]
	}
	return bits[0], bits[len(bits)-1]
}

// parseTrigger parses T/t entries: T[bit],[name]
func parseTrigger(raw, rest string, hidden bool) *MenuItem {
	parts := strings.SplitN(rest, ",", 2)
	bit := 0
	name := ""
	if len(parts) >= 1 {
		if b, err := strconv.Atoi(parts[0]); err == nil {
			bit = b
		}
	}
	if len(parts) >= 2 {
		name = parts[1]
	}

	item := &MenuItem{
		Type: "trigger",
		Raw:  raw,
		Name: name,
		Bit:  bit,
	}
	if hidden {
		item.Type = "trigger_hidden"
	}
	return item
}

// parseFileLoad parses F/FC/FS/S entries: F[S][index],[ext1ext2ext3],[label]
func parseFileLoad(raw, rest string) *MenuItem {
	typ := "file_load"
	if raw[0] == 'S' {
		typ = "mount"
	}

	// Check for FC (core-selecting file load) or FS (file load with storage slot)
	if len(rest) > 0 && rest[0] == 'C' {
		typ = "file_load_core"
		rest = rest[1:]
	} else if len(rest) > 0 && rest[0] == 'S' {
		// FS = file load with S-type slot (storage)
		rest = rest[1:]
	}

	parts := strings.SplitN(rest, ",", -1)

	item := &MenuItem{
		Type: typ,
		Raw:  raw,
	}

	idx := 0
	partStart := 0

	// First part might be a numeric index
	if len(parts) > 0 {
		if n, err := strconv.Atoi(parts[0]); err == nil {
			idx = n
			partStart = 1
		}
	}
	item.Index = idx

	// Extensions part: 3-char groups concatenated, e.g. "BINSFC" -> [BIN, SFC]
	if partStart < len(parts) {
		extStr := parts[partStart]
		item.Extensions = parseExtensions(extStr)
		partStart++
	}

	// Label
	if partStart < len(parts) {
		item.Label = parts[partStart]
	}

	return item
}

// parseExtensions splits a concatenated extension string into 3-char groups.
// "BINSFC" -> ["BIN", "SFC"], "SFCSMCBS" -> ["SFC", "SMC", "BS "]
// Also handles variable-length extensions separated by dots or already short.
func parseExtensions(s string) []string {
	if s == "" {
		return nil
	}

	// If it contains a dot, split on dots
	if strings.Contains(s, ".") {
		parts := strings.Split(s, ".")
		var exts []string
		for _, p := range parts {
			p = strings.TrimSpace(p)
			if p != "" {
				exts = append(exts, strings.ToUpper(p))
			}
		}
		return exts
	}

	// Standard CONF_STR: 3-character groups
	var exts []string
	for i := 0; i+3 <= len(s); i += 3 {
		ext := s[i : i+3]
		exts = append(exts, strings.TrimSpace(ext))
	}
	// Handle remainder (1-2 chars)
	remainder := len(s) % 3
	if remainder > 0 {
		ext := s[len(s)-remainder:]
		exts = append(exts, strings.TrimSpace(ext))
	}
	return exts
}

// parseSubPage parses P[id],[name] or P[id]O entries.
func parseSubPage(raw, rest string) *MenuItem {
	// P can be followed by a digit and then a comma+name, or just a digit+letter combo
	parts := strings.SplitN(rest, ",", 2)
	id := 0
	name := ""

	if len(parts) >= 1 {
		// The first char(s) before comma are the page ID
		idStr := parts[0]
		if n, err := strconv.Atoi(idStr); err == nil {
			id = n
		} else if len(idStr) > 0 {
			// Could be like "1O" — page 1, open
			if idStr[0] >= '0' && idStr[0] <= '9' {
				id = int(idStr[0] - '0')
			}
		}
	}
	if len(parts) >= 2 {
		name = parts[1]
	}

	return &MenuItem{
		Type:   "sub_page",
		Raw:    raw,
		Name:   name,
		PageID: id,
	}
}

// parseReset parses R[bit],[name]
func parseReset(raw, rest string) *MenuItem {
	parts := strings.SplitN(rest, ",", 2)
	bit := 0
	name := ""
	if len(parts) >= 1 {
		if b, err := strconv.Atoi(parts[0]); err == nil {
			bit = b
		}
	}
	if len(parts) >= 2 {
		name = parts[1]
	}
	return &MenuItem{
		Type: "reset",
		Raw:  raw,
		Name: name,
		Bit:  bit,
	}
}

// parseCheat parses C entries.
func parseCheat(raw, rest string) *MenuItem {
	return &MenuItem{
		Type: "cheat",
		Raw:  raw,
		Name: rest,
	}
}

// parseHideDisable parses H/h/D/d entries.
// Format: H[bit][inner_item] — e.g. "H1O34,Hidden Opt,A,B" means hide-controlled (bit 1) option O34.
// When an inner command is present (O, T, etc.), it parses the inner item and attaches the hide condition.
// Otherwise returns a standalone hide/disable marker.
func parseHideDisable(raw, rest, typ string, inverted bool) *MenuItem {
	actualType := typ
	if inverted {
		actualType = typ + "_inverted"
	}

	if len(rest) == 0 {
		return &MenuItem{Type: actualType, Raw: raw}
	}

	// First character(s) are the bit number
	bit := 0
	i := 0
	for i < len(rest) && rest[i] >= '0' && rest[i] <= '9' {
		i++
	}
	if i > 0 {
		if b, err := strconv.Atoi(rest[:i]); err == nil {
			bit = b
		}
	}

	cond := HideCondition{
		Bit:      bit,
		Type:     typ,
		Inverted: inverted,
	}

	remaining := rest[i:]

	// If remaining starts with a command letter (O, T, etc.), parse the inner item
	if len(remaining) > 0 && isCommandPrefix(remaining[0]) {
		inner := parseMenuItem(remaining)
		if inner != nil {
			inner.Raw = raw // preserve original raw including H/D prefix
			inner.HideConditions = append(inner.HideConditions, cond)
			return inner
		}
	}

	// Standalone hide/disable marker (e.g. "H1,Some Label")
	name := ""
	if len(remaining) > 0 && remaining[0] == ',' {
		name = remaining[1:]
	}

	return &MenuItem{
		Type:           actualType,
		Raw:            raw,
		Name:           name,
		Bit:            bit,
		HideConditions: []HideCondition{cond},
	}
}

// parseInfo parses I entries (informational text).
func parseInfo(raw, rest string) *MenuItem {
	return &MenuItem{
		Type: "info",
		Raw:  raw,
		Name: rest,
	}
}

// parseVersion parses V entries (version string).
func parseVersion(raw, rest string) *MenuItem {
	return &MenuItem{
		Type: "version",
		Raw:  raw,
		Name: rest,
	}
}

// parseJoystick parses J entries (joystick button mapping).
func parseJoystick(raw, rest string) *MenuItem {
	parts := strings.SplitN(rest, ",", -1)
	return &MenuItem{
		Type:   "joystick",
		Raw:    raw,
		Name:   strings.Join(parts, ","),
		Values: parts,
	}
}

// ExtractConfStr extracts the CONF_STR value from SystemVerilog source code.
// Handles both: localparam CONF_STR = { "...","..." } and parameter CONF_STR = "..."
func ExtractConfStr(source string) string {
	// Find CONF_STR assignment
	idx := strings.Index(strings.ToUpper(source), "CONF_STR")
	if idx < 0 {
		return ""
	}

	// Find the start of the string content after CONF_STR
	rest := source[idx:]

	// Skip past "CONF_STR" and any whitespace/= sign
	eqIdx := strings.Index(rest, "=")
	if eqIdx < 0 {
		return ""
	}
	rest = rest[eqIdx+1:]

	// Determine format: curly-brace concatenation or plain string
	trimmed := strings.TrimSpace(rest)

	if strings.HasPrefix(trimmed, "{") {
		return extractBraceConfStr(trimmed)
	}

	// Plain string: parameter CONF_STR = "..."
	if strings.HasPrefix(trimmed, "\"") {
		return extractQuotedString(trimmed)
	}

	return ""
}

// extractBraceConfStr extracts from { "str1", "str2", ... }; format.
func extractBraceConfStr(s string) string {
	// Find matching closing brace
	depth := 0
	end := -1
	for i, c := range s {
		if c == '{' {
			depth++
		} else if c == '}' {
			depth--
			if depth == 0 {
				end = i
				break
			}
		}
	}
	if end < 0 {
		return ""
	}

	inner := s[1:end]

	// Extract all quoted strings and concatenate
	var sb strings.Builder
	inQuote := false
	escaped := false
	for _, c := range inner {
		if escaped {
			sb.WriteRune(c)
			escaped = false
			continue
		}
		if c == '\\' {
			escaped = true
			continue
		}
		if c == '"' {
			inQuote = !inQuote
			continue
		}
		if inQuote {
			sb.WriteRune(c)
		}
	}
	return sb.String()
}

// extractQuotedString extracts a single "..." string value.
func extractQuotedString(s string) string {
	if len(s) < 2 || s[0] != '"' {
		return ""
	}
	var sb strings.Builder
	escaped := false
	for _, c := range s[1:] {
		if escaped {
			sb.WriteRune(c)
			escaped = false
			continue
		}
		if c == '\\' {
			escaped = true
			continue
		}
		if c == '"' {
			return sb.String()
		}
		sb.WriteRune(c)
	}
	return sb.String()
}

// ExtractCoreName extracts the core display name from a CONF_STR raw string.
// The first semicolon-delimited field is the core name.
func ExtractCoreName(raw string) string {
	idx := strings.Index(raw, ";")
	if idx < 0 {
		return raw
	}
	return strings.TrimSpace(raw[:idx])
}

// ConfStr DB loading for the server

var (
	confstrDB     *ConfStrDB
	confstrDBOnce sync.Once
	confstrDBPath = "/media/fat/Scripts/confstr_db.json"
)

// SetConfStrDBPath sets the path to the CONF_STR database file.
func SetConfStrDBPath(path string) {
	confstrDBPath = path
}

// LoadConfStrDB loads the CONF_STR database from disk.
func LoadConfStrDB() (*ConfStrDB, error) {
	data, err := os.ReadFile(confstrDBPath)
	if err != nil {
		return nil, fmt.Errorf("reading confstr db: %w", err)
	}
	var db ConfStrDB
	if err := json.Unmarshal(data, &db); err != nil {
		return nil, fmt.Errorf("parsing confstr db: %w", err)
	}
	return &db, nil
}

// GetConfStrDB returns the cached CONF_STR database, loading it on first access.
func GetConfStrDB() (*ConfStrDB, error) {
	var loadErr error
	confstrDBOnce.Do(func() {
		confstrDB, loadErr = LoadConfStrDB()
	})
	if loadErr != nil {
		return nil, loadErr
	}
	return confstrDB, nil
}

// LookupCoreOSD finds a core's OSD info by name (case-insensitive).
// Tries exact match on core_name, then rbf_name, then repo name, then substring matching.
func LookupCoreOSD(db *ConfStrDB, coreName string) *CoreOSD {
	target := strings.ToLower(coreName)
	// Exact match on core_name
	for i := range db.Cores {
		if strings.ToLower(db.Cores[i].CoreName) == target {
			return &db.Cores[i]
		}
	}
	// Exact match on rbf_name
	for i := range db.Cores {
		if db.Cores[i].RbfName != "" && strings.ToLower(db.Cores[i].RbfName) == target {
			return &db.Cores[i]
		}
	}
	// Match against repo name suffix
	for i := range db.Cores {
		repo := db.Cores[i].Repo
		if idx := strings.LastIndex(repo, "/"); idx >= 0 {
			repoName := strings.ToLower(repo[idx+1:])
			repoName = strings.TrimSuffix(repoName, "_mister")
			repoName = strings.TrimPrefix(repoName, "arcade-")
			if repoName == target {
				return &db.Cores[i]
			}
		}
	}
	// Substring matching
	for i := range db.Cores {
		rbf := strings.ToLower(db.Cores[i].RbfName)
		if rbf != "" && (strings.Contains(rbf, target) || strings.Contains(target, rbf)) {
			return &db.Cores[i]
		}
	}
	// Subsequence matching (TaitoSJ is subsequence of TaitoSystemSJ)
	normTarget := normalizeForMatch(coreName)
	for i := range db.Cores {
		candidates := []string{db.Cores[i].CoreName, db.Cores[i].RbfName}
		repo := db.Cores[i].Repo
		if idx := strings.LastIndex(repo, "/"); idx >= 0 {
			candidates = append(candidates, RepoToCoreName(repo[idx+1:]))
		}
		for _, c := range candidates {
			nc := normalizeForMatch(c)
			if nc == "" || nc == normTarget {
				continue
			}
			// Check if shorter is subsequence of longer, and length ratio > 0.5
			short, long := normTarget, nc
			if len(short) > len(long) {
				short, long = long, short
			}
			if float64(len(short))/float64(len(long)) > 0.4 && isSubsequence(short, long) {
				return &db.Cores[i]
			}
		}
	}
	// Fuzzy matching via longest common subsequence ratio
	if len(normTarget) < 4 {
		return nil
	}
	var bestCore *CoreOSD
	bestRatio := 0.0
	const lcsThreshold = 0.85
	for i := range db.Cores {
		candidates := []string{db.Cores[i].CoreName, db.Cores[i].RbfName}
		repo := db.Cores[i].Repo
		if idx := strings.LastIndex(repo, "/"); idx >= 0 {
			candidates = append(candidates, RepoToCoreName(repo[idx+1:]))
		}
		for _, c := range candidates {
			nc := normalizeForMatch(c)
			if nc == "" {
				continue
			}
			ratio := lcsRatio(normTarget, nc)
			if ratio > lcsThreshold && ratio > bestRatio {
				bestRatio = ratio
				bestCore = &db.Cores[i]
			}
		}
	}
	return bestCore
}

// normalizeForMatch prepares a string for fuzzy comparison:
// lowercases, strips "a." prefix, removes non-alpha characters.
func normalizeForMatch(s string) string {
	s = strings.ToLower(s)
	s = strings.TrimPrefix(s, "a.")
	var sb strings.Builder
	for _, r := range s {
		if r >= 'a' && r <= 'z' {
			sb.WriteRune(r)
		}
	}
	return sb.String()
}

// lcsRatio returns the longest common subsequence length divided by
// the length of the shorter string. This measures what fraction of the
// shorter string appears as a subsequence of the longer one.

// isSubsequence checks if short is a subsequence of long (all chars appear in order).
func isSubsequence(short, long string) bool {
	si := 0
	for li := 0; li < len(long) && si < len(short); li++ {
		if short[si] == long[li] {
			si++
		}
	}
	return si == len(short)
}

func lcsRatio(a, b string) float64 {
	m, n := len(a), len(b)
	if m == 0 || n == 0 {
		return 0
	}
	// Space-optimized LCS: two rows
	prev := make([]int, n+1)
	curr := make([]int, n+1)
	for i := 1; i <= m; i++ {
		for j := 1; j <= n; j++ {
			if a[i-1] == b[j-1] {
				curr[j] = prev[j-1] + 1
			} else if prev[j] > curr[j-1] {
				curr[j] = prev[j]
			} else {
				curr[j] = curr[j-1]
			}
		}
		prev, curr = curr, prev
		for j := range curr {
			curr[j] = 0
		}
	}
	minLen := m
	if n < m {
		minLen = n
	}
	return float64(prev[n]) / float64(minLen)
}

// RepoToCoreName extracts a core name from a GitHub repo name.
// "SNES_MiSTer" -> "SNES", "Arcade-DonkeyKong_MiSTer" -> "DonkeyKong", "jtcps1" -> "jtcps1"
func RepoToCoreName(repoName string) string {
	name := strings.TrimSuffix(repoName, "_MiSTer")
	name = strings.TrimPrefix(name, "Arcade-")
	return name
}

// VisibleMenu returns only the menu items that are visible given the current CFG state.
// This matches what the MiSTer OSD actually displays.
func VisibleMenu(core *CoreOSD, cfgData []byte) []MenuItem {
	var visible []MenuItem
	for _, item := range core.Menu {
		if item.Visible(cfgData) {
			visible = append(visible, item)
		}
	}
	return visible
}

// FindOption finds a menu item by name (case-insensitive) in a core's menu.
func FindOption(core *CoreOSD, name string) *MenuItem {
	target := strings.ToLower(name)
	for i := range core.Menu {
		if strings.ToLower(core.Menu[i].Name) == target {
			return &core.Menu[i]
		}
	}
	return nil
}

// FindOptionValue returns the index of a value name within an option's Values list.
// Returns -1 if not found. Case-insensitive.
func FindOptionValue(item *MenuItem, valueName string) int {
	target := strings.ToLower(valueName)
	for i, v := range item.Values {
		if strings.ToLower(v) == target {
			return i
		}
	}
	return -1
}

// LetterToBit converts a CONF_STR bit letter to a bit number.
// A-Z = 0-25, a-z = 32-57 (a=32).
func LetterToBit(c byte) int {
	switch {
	case c >= 'A' && c <= 'Z':
		return int(c - 'A')
	case c >= 'a' && c <= 'z':
		return int(c-'a') + 32
	case c >= '0' && c <= '9':
		return int(c - '0')
	}
	return 0
}
