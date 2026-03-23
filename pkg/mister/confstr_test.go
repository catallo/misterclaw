package mister

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sync"
	"testing"
)

func TestParseConfStr_SNES(t *testing.T) {
	raw := "SNES;;" +
		"FS0,SFCSMCBS ,Load;" +
		"FC0,IPS,Load Cheat;" +
		"-;" +
		"O89,Aspect Ratio,Original,Full Screen,Custom;" +
		"O46,Scandoubler Fx,None,HQ2x,CRT 25%,CRT 50%,CRT 75%;" +
		"OAB,Video Mode,NTSC,PAL;" +
		"T0,Reset;" +
		"V,v1.0"

	items := ParseConfStr(raw)
	if len(items) == 0 {
		t.Fatal("expected items, got none")
	}

	// First item: core name label
	if items[0].Type != "label" || items[0].Name != "SNES" {
		t.Errorf("expected label SNES, got %s %s", items[0].Type, items[0].Name)
	}

	// File load
	found := false
	for _, item := range items {
		if item.Type == "file_load" && item.Label == "Load" {
			found = true
			if len(item.Extensions) < 2 {
				t.Errorf("expected at least 2 extensions, got %v", item.Extensions)
			}
			break
		}
	}
	if !found {
		t.Error("expected file_load item with label Load")
	}

	// Option: Aspect Ratio
	found = false
	for _, item := range items {
		if item.Type == "option" && item.Name == "Aspect Ratio" {
			found = true
			if item.Bit != 8 || item.BitHigh != 9 {
				t.Errorf("expected bits 8-9, got %d-%d", item.Bit, item.BitHigh)
			}
			if len(item.Values) != 3 {
				t.Errorf("expected 3 values, got %d: %v", len(item.Values), item.Values)
			}
			break
		}
	}
	if !found {
		t.Error("expected option Aspect Ratio")
	}

	// Trigger: Reset
	found = false
	for _, item := range items {
		if item.Type == "trigger" && item.Name == "Reset" {
			found = true
			if item.Bit != 0 {
				t.Errorf("expected bit 0, got %d", item.Bit)
			}
			break
		}
	}
	if !found {
		t.Error("expected trigger Reset")
	}

	// Version
	found = false
	for _, item := range items {
		if item.Type == "version" {
			found = true
			break
		}
	}
	if !found {
		t.Error("expected version item")
	}
}

func TestParseConfStr_Separator(t *testing.T) {
	items := ParseConfStr("CORE;;-;T0,Reset")
	hasSep := false
	for _, item := range items {
		if item.Type == "separator" {
			hasSep = true
		}
	}
	if !hasSep {
		t.Error("expected separator")
	}
}

func TestParseConfStr_DIP(t *testing.T) {
	items := ParseConfStr("CORE;;DIP;T0,Reset")
	hasDIP := false
	for _, item := range items {
		if item.Type == "dip" {
			hasDIP = true
		}
	}
	if !hasDIP {
		t.Error("expected DIP item")
	}
}

func TestParseConfStr_SubPage(t *testing.T) {
	items := ParseConfStr("CORE;;P1,Audio Settings;O12,Volume,Low,High;P1O34,Bass,Off,On")
	var subPage *MenuItem
	for i := range items {
		if items[i].Type == "sub_page" {
			subPage = &items[i]
			break
		}
	}
	if subPage == nil {
		t.Fatal("expected sub_page item")
	}
	if subPage.PageID != 1 || subPage.Name != "Audio Settings" {
		t.Errorf("expected page 1 Audio Settings, got page %d %s", subPage.PageID, subPage.Name)
	}
}

func TestParseConfStr_FileLoadCore(t *testing.T) {
	items := ParseConfStr("CORE;;FC0,BIN,Load ROM")
	var fc *MenuItem
	for i := range items {
		if items[i].Type == "file_load_core" {
			fc = &items[i]
			break
		}
	}
	if fc == nil {
		t.Fatal("expected file_load_core item")
	}
	if fc.Label != "Load ROM" {
		t.Errorf("expected label 'Load ROM', got '%s'", fc.Label)
	}
	if len(fc.Extensions) == 0 || fc.Extensions[0] != "BIN" {
		t.Errorf("expected extension BIN, got %v", fc.Extensions)
	}
}

func TestParseConfStr_Mount(t *testing.T) {
	items := ParseConfStr("CORE;;S0,VHD,Mount VHD")
	var mount *MenuItem
	for i := range items {
		if items[i].Type == "mount" {
			mount = &items[i]
			break
		}
	}
	if mount == nil {
		t.Fatal("expected mount item")
	}
	if mount.Label != "Mount VHD" {
		t.Errorf("expected label 'Mount VHD', got '%s'", mount.Label)
	}
}

func TestParseConfStr_HideDisable(t *testing.T) {
	items := ParseConfStr("CORE;;H1O34,Hidden Opt,A,B;D2O56,Disabled Opt,C,D")
	var hide, disable *MenuItem
	for i := range items {
		if items[i].Name == "Hidden Opt" {
			hide = &items[i]
		}
		if items[i].Name == "Disabled Opt" {
			disable = &items[i]
		}
	}
	if hide == nil {
		t.Fatal("expected hide item (Hidden Opt)")
	}
	// H1O34 should parse as an option with a hide condition
	if hide.Type != "option" {
		t.Errorf("expected type option, got %s", hide.Type)
	}
	if len(hide.HideConditions) != 1 {
		t.Fatalf("expected 1 hide condition, got %d", len(hide.HideConditions))
	}
	if hide.HideConditions[0].Bit != 1 || hide.HideConditions[0].Type != "hide" {
		t.Errorf("expected hide condition bit 1, got bit %d type %s", hide.HideConditions[0].Bit, hide.HideConditions[0].Type)
	}
	if hide.Bit != 3 || hide.BitHigh != 4 {
		t.Errorf("expected option bits 3-4, got %d-%d", hide.Bit, hide.BitHigh)
	}
	if len(hide.Values) != 2 {
		t.Errorf("expected 2 values, got %d: %v", len(hide.Values), hide.Values)
	}

	if disable == nil {
		t.Fatal("expected disable item (Disabled Opt)")
	}
	if disable.Type != "option" {
		t.Errorf("expected type option, got %s", disable.Type)
	}
	if len(disable.HideConditions) != 1 {
		t.Fatalf("expected 1 disable condition, got %d", len(disable.HideConditions))
	}
	if disable.HideConditions[0].Bit != 2 || disable.HideConditions[0].Type != "disable" {
		t.Errorf("expected disable condition bit 2, got bit %d type %s", disable.HideConditions[0].Bit, disable.HideConditions[0].Type)
	}
}

func TestParseConfStr_Joystick(t *testing.T) {
	items := ParseConfStr("CORE;;J,A,B,X,Y,L,R,Select,Start")
	var joy *MenuItem
	for i := range items {
		if items[i].Type == "joystick" {
			joy = &items[i]
			break
		}
	}
	if joy == nil {
		t.Fatal("expected joystick item")
	}
	if len(joy.Values) < 8 {
		t.Errorf("expected at least 8 joystick values, got %d: %v", len(joy.Values), joy.Values)
	}
}

func TestParseConfStr_Info(t *testing.T) {
	items := ParseConfStr("CORE;;IVersion 1.0 by Author")
	var info *MenuItem
	for i := range items {
		if items[i].Type == "info" {
			info = &items[i]
			break
		}
	}
	if info == nil {
		t.Fatal("expected info item")
	}
	if info.Name != "Version 1.0 by Author" {
		t.Errorf("unexpected info name: %s", info.Name)
	}
}

func TestParseBitRange(t *testing.T) {
	tests := []struct {
		input    string
		wantLow  int
		wantHigh int
	}{
		{"0", 0, 0},
		{"3", 3, 3},
		{"89", 8, 9},
		{"AB", 10, 11},
		{"9A", 9, 10},
		{"AV", 10, 31},
		{"", 0, 0},
	}
	for _, tt := range tests {
		low, high := parseBitRange(tt.input)
		if low != tt.wantLow || high != tt.wantHigh {
			t.Errorf("parseBitRange(%q) = (%d,%d), want (%d,%d)", tt.input, low, high, tt.wantLow, tt.wantHigh)
		}
	}
}

func TestParseExtensions(t *testing.T) {
	tests := []struct {
		input string
		want  []string
	}{
		{"BIN", []string{"BIN"}},
		{"BINSFC", []string{"BIN", "SFC"}},
		{"SFCSMCBS", []string{"SFC", "SMC", "BS"}},
		{"", nil},
	}
	for _, tt := range tests {
		got := parseExtensions(tt.input)
		if len(got) != len(tt.want) {
			t.Errorf("parseExtensions(%q) = %v, want %v", tt.input, got, tt.want)
			continue
		}
		for i := range got {
			if got[i] != tt.want[i] {
				t.Errorf("parseExtensions(%q)[%d] = %q, want %q", tt.input, i, got[i], tt.want[i])
			}
		}
	}
}

func TestExtractConfStr_BraceFormat(t *testing.T) {
	sv := `
module top(
    input clk
);

localparam CONF_STR = {
    "SNES;;",
    "FS0,SFCSMCBS ,Load;",
    "O89,Aspect Ratio,Original,Full Screen;",
    "T0,Reset;"
};

endmodule
`
	got := ExtractConfStr(sv)
	if got == "" {
		t.Fatal("expected non-empty CONF_STR")
	}
	if !containsStr(got, "SNES") {
		t.Errorf("expected SNES in result, got: %s", got)
	}
	if !containsStr(got, "Aspect Ratio") {
		t.Errorf("expected Aspect Ratio in result, got: %s", got)
	}
}

func TestExtractConfStr_PlainString(t *testing.T) {
	sv := `parameter CONF_STR = "CORE;;O1,Aspect,4:3,16:9;T0,Reset;";`
	got := ExtractConfStr(sv)
	if got == "" {
		t.Fatal("expected non-empty CONF_STR")
	}
	if !containsStr(got, "CORE") {
		t.Errorf("expected CORE in result, got: %s", got)
	}
}

func TestExtractConfStr_NotFound(t *testing.T) {
	sv := `module top(); endmodule`
	got := ExtractConfStr(sv)
	if got != "" {
		t.Errorf("expected empty result, got: %s", got)
	}
}

func TestExtractCoreNameFromConfStr(t *testing.T) {
	tests := []struct {
		raw  string
		want string
	}{
		{"SNES;;O1,Foo,A,B", "SNES"},
		{"MegaDrive;", "MegaDrive"},
		{"NES", "NES"},
		{"", ""},
	}
	for _, tt := range tests {
		got := ExtractCoreName(tt.raw)
		if got != tt.want {
			t.Errorf("ExtractCoreName(%q) = %q, want %q", tt.raw, got, tt.want)
		}
	}
}

func TestRepoToCoreName(t *testing.T) {
	tests := []struct {
		input string
		want  string
	}{
		{"SNES_MiSTer", "SNES"},
		{"Arcade-DonkeyKong_MiSTer", "DonkeyKong"},
		{"MegaDrive_MiSTer", "MegaDrive"},
		{"jtcps1", "jtcps1"},
	}
	for _, tt := range tests {
		got := RepoToCoreName(tt.input)
		if got != tt.want {
			t.Errorf("RepoToCoreName(%q) = %q, want %q", tt.input, got, tt.want)
		}
	}
}

func TestLookupCoreOSD(t *testing.T) {
	db := &ConfStrDB{
		Cores: []CoreOSD{
			{CoreName: "SNES", Repo: "MiSTer-devel/SNES_MiSTer"},
			{CoreName: "Genesis", Repo: "MiSTer-devel/MegaDrive_MiSTer"},
		},
	}

	// By core name
	if got := LookupCoreOSD(db, "snes"); got == nil || got.CoreName != "SNES" {
		t.Error("expected to find SNES by name")
	}

	// By repo suffix
	if got := LookupCoreOSD(db, "megadrive"); got == nil || got.CoreName != "Genesis" {
		t.Error("expected to find Genesis by repo name megadrive")
	}

	// Not found
	if got := LookupCoreOSD(db, "nonexistent"); got != nil {
		t.Error("expected nil for nonexistent core")
	}
}

func TestLoadConfStrDB(t *testing.T) {
	// Create a temp DB file
	db := ConfStrDB{
		Version: "test",
		Cores: []CoreOSD{
			{
				CoreName:   "TestCore",
				Repo:       "test/TestCore_MiSTer",
				ConfStrRaw: "TestCore;;T0,Reset",
				Menu: []MenuItem{
					{Type: "label", Name: "TestCore"},
					{Type: "trigger", Bit: 0, Name: "Reset"},
				},
			},
		},
	}
	data, _ := json.Marshal(db)
	tmpDir := t.TempDir()
	tmpFile := filepath.Join(tmpDir, "confstr_db.json")
	if err := os.WriteFile(tmpFile, data, 0644); err != nil {
		t.Fatal(err)
	}

	// Override path and reset the once
	old := confstrDBPath
	confstrDBPath = tmpFile
	confstrDBOnce = syncOnce()
	defer func() {
		confstrDBPath = old
	}()

	loaded, err := LoadConfStrDB()
	if err != nil {
		t.Fatal(err)
	}
	if loaded.Version != "test" {
		t.Errorf("expected version test, got %s", loaded.Version)
	}
	if len(loaded.Cores) != 1 {
		t.Errorf("expected 1 core, got %d", len(loaded.Cores))
	}
}

func TestParseConfStr_RealGenesis(t *testing.T) {
	// Real-world CONF_STR from Genesis/MegaDrive core
	raw := "GENESIS;ACTIVE;" +
		"FS0,BINGENMD ,Load ROM;" +
		"O67,Region,Auto,EU,JP,US;" +
		"ODE,Aspect Ratio,Original,Full Screen,Custom;" +
		"O23,Scandoubler Fx,None,HQ2x,CRT 25%,CRT 50%;" +
		"-;" +
		"P1,Audio Settings;" +
		"P1O4,FM Chip,YM2612,YM3438;" +
		"P1O5,Audio Filter,On,Off;" +
		"-;" +
		"R0,Reset;" +
		"J1,A,B,C,X,Y,Z,Mode,Start;" +
		"V,v1.0"

	items := ParseConfStr(raw)

	// Check we got a reasonable number of items
	if len(items) < 10 {
		t.Fatalf("expected at least 10 items, got %d", len(items))
	}

	// Check region option with letter bits
	var region *MenuItem
	for i := range items {
		if items[i].Type == "option" && items[i].Name == "Region" {
			region = &items[i]
			break
		}
	}
	if region == nil {
		t.Fatal("expected Region option")
	}
	if region.Bit != 6 || region.BitHigh != 7 {
		t.Errorf("expected bits 6-7, got %d-%d", region.Bit, region.BitHigh)
	}
	if len(region.Values) != 4 {
		t.Errorf("expected 4 region values, got %d", len(region.Values))
	}

	// Check aspect ratio with letter bits (D,E = 13,14)
	var aspect *MenuItem
	for i := range items {
		if items[i].Type == "option" && items[i].Name == "Aspect Ratio" {
			aspect = &items[i]
			break
		}
	}
	if aspect == nil {
		t.Fatal("expected Aspect Ratio option")
	}
	if aspect.Bit != 13 || aspect.BitHigh != 14 {
		t.Errorf("expected bits 13-14 (D,E), got %d-%d", aspect.Bit, aspect.BitHigh)
	}

	// Check reset
	var reset *MenuItem
	for i := range items {
		if items[i].Type == "reset" {
			reset = &items[i]
			break
		}
	}
	if reset == nil {
		t.Fatal("expected reset item")
	}
}

func containsStr(s, sub string) bool {
	return len(s) >= len(sub) && (s == sub || len(s) > 0 && findStr(s, sub))
}

func findStr(s, sub string) bool {
	for i := 0; i <= len(s)-len(sub); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}

// syncOnce returns a fresh sync.Once (for testing).
func syncOnce() sync.Once { return sync.Once{} }

func TestParseReset_LetterBit(t *testing.T) {
	items := ParseConfStr("CORE;;RF,SYNC FD0;RG,SYNC FD1;R6,Reset")
	var resets []MenuItem
	for _, item := range items {
		if item.Type == "reset" {
			resets = append(resets, item)
		}
	}
	if len(resets) != 3 {
		t.Fatalf("expected 3 reset items, got %d", len(resets))
	}
	// RF → bit 15
	if resets[0].Name != "SYNC FD0" || resets[0].Bit != 15 {
		t.Errorf("RF: got name=%q bit=%d, want name=SYNC FD0 bit=15", resets[0].Name, resets[0].Bit)
	}
	// RG → bit 16
	if resets[1].Name != "SYNC FD1" || resets[1].Bit != 16 {
		t.Errorf("RG: got name=%q bit=%d, want name=SYNC FD1 bit=16", resets[1].Name, resets[1].Bit)
	}
	// R6 → bit 6
	if resets[2].Name != "Reset" || resets[2].Bit != 6 {
		t.Errorf("R6: got name=%q bit=%d, want name=Reset bit=6", resets[2].Name, resets[2].Bit)
	}
}

func TestStripCoreDateSuffix(t *testing.T) {
	tests := []struct {
		input, want string
	}{
		{"PC88_20250918", "PC88"},
		{"SNES_20250605", "SNES"},
		{"MegaDrive_20250707", "MegaDrive"},
		{"Menu", "Menu"},
		{"PC88", "PC88"},
		{"core_v2", "core_v2"},
	}
	for _, tt := range tests {
		got := StripCoreDateSuffix(tt.input)
		if got != tt.want {
			t.Errorf("StripCoreDateSuffix(%q) = %q, want %q", tt.input, got, tt.want)
		}
	}
}

func TestIsOSDTopLevelItem(t *testing.T) {
	tests := []struct {
		typ  string
		want bool
	}{
		{"option", true},
		{"trigger", true},
		{"reset", true},
		{"separator", true},
		{"mount", true},
		{"file_load", true},
		{"file_load_core", true},
		{"cheat", true},
		{"dip", true},
		{"label", false},
		{"joystick", false},
		{"version", false},
		{"info", false},
		{"option_hidden", false},
		{"trigger_hidden", false},
		{"sub_page", false},
		{"hide", false},
		{"hide_inverted", false},
		{"disable", false},
		{"disable_inverted", false},
	}
	for _, tt := range tests {
		got := isOSDTopLevelItem(MenuItem{Type: tt.typ})
		if got != tt.want {
			t.Errorf("isOSDTopLevelItem(%q) = %v, want %v", tt.typ, got, tt.want)
		}
	}
}

func TestFindOSDItemPosition_PC8801(t *testing.T) {
	raw := "PC8801;;-;O12,Aspect ratio,Original,Full Screen,[ARC1],[ARC2];O34,Scale,Normal,V-Integer,Narrower HV-Integer,Wider HV-Integer;OHJ,Scandoubler Fx,None,HQ2x,CRT 25%,CRT 50%,CRT 75%;-;O78,Mode,N88V2,N88V1H,N88V1S,N;O9,Speed,4MHz,8MHz;-;S0,D88,FDD0;S1,D88,FDD1;RF,SYNC FD0;RG,SYNC FD1;-;OA,Basic mode,Basic,Terminal;OB,Cols,80,40;OC,Lines,25,20;OD,Disk boot,Enable,Disable;-;OK,Input,Joypad,Mouse;OL,Sound Board 2,Expansion,Onboard;-;R6,Reset;J,Fire 1,Fire 2;V,v"
	db := &ConfStrDB{
		Cores: []CoreOSD{
			{CoreName: "PC8801", Repo: "MiSTer-devel/PC88_MiSTer", ConfStrRaw: raw},
		},
	}

	pos, err := FindOSDItemPosition(db, "PC8801", "Reset", nil)
	if err != nil {
		t.Fatal(err)
	}
	// Visible top-level items:
	// sep(0) Aspect(1) Scale(2) Scandoubler(3) sep(4) Mode(5) Speed(6)
	// sep(7) FDD0(8) FDD1(9) SYNC_FD0(10) SYNC_FD1(11)
	// sep(12) Basic(13) Cols(14) Lines(15) Disk_boot(16)
	// sep(17) Input(18) SoundBoard2(19) sep(20) Reset(21)
	if pos != 21 {
		t.Errorf("PC8801 Reset: expected position 21, got %d", pos)
	}

	// Test finding mount by label
	pos, err = FindOSDItemPosition(db, "PC8801", "FDD0", nil)
	if err != nil {
		t.Fatal(err)
	}
	if pos != 8 {
		t.Errorf("PC8801 FDD0: expected position 8, got %d", pos)
	}
}

func TestFindOSDItemPosition_SimpleCore(t *testing.T) {
	raw := "MYCORE;;FS0,BIN,Load;-;O1,Opt,A,B;T0,Reset;R0,Reset;V,v1"
	db := &ConfStrDB{
		Cores: []CoreOSD{
			{CoreName: "MYCORE", ConfStrRaw: raw},
		},
	}

	// Visible: file_load(0), sep(1), option(2), trigger"Reset"(3), reset"Reset"(4)
	pos, err := FindOSDItemPosition(db, "MYCORE", "Reset", nil)
	if err != nil {
		t.Fatal(err)
	}
	// First "Reset" match is the trigger at position 3
	if pos != 3 {
		t.Errorf("SimpleCore Reset: expected position 3, got %d", pos)
	}
}

func TestFindOSDItemPosition_NotFound(t *testing.T) {
	db := &ConfStrDB{
		Cores: []CoreOSD{
			{CoreName: "TestCore", ConfStrRaw: "TestCore;;T0,Reset"},
		},
	}
	_, err := FindOSDItemPosition(db, "TestCore", "NonExistent", nil)
	if err == nil {
		t.Error("expected error for non-existent target")
	}
}

func TestFindOSDItemPosition_CoreNotFound(t *testing.T) {
	db := &ConfStrDB{}
	_, err := FindOSDItemPosition(db, "UnknownCore", "Reset", nil)
	if err == nil {
		t.Error("expected error for unknown core")
	}
}

func TestFindOSDItemPosition_FuzzyMatch(t *testing.T) {
	raw := "PC8801;;-;R6,Reset;V,v"
	db := &ConfStrDB{
		Cores: []CoreOSD{
			{CoreName: "PC8801", Repo: "MiSTer-devel/PC88_MiSTer", ConfStrRaw: raw},
		},
	}
	// PC88 should match PC8801 via repo name
	pos, err := FindOSDItemPosition(db, "PC88", "Reset", nil)
	if err != nil {
		t.Fatalf("fuzzy match PC88 → PC8801 failed: %v", err)
	}
	// sep(0), Reset(1)
	if pos != 1 {
		t.Errorf("expected position 1, got %d", pos)
	}
}

func TestNormalizeCoreName(t *testing.T) {
	// Set up a test DB with PC8801
	db := ConfStrDB{
		Cores: []CoreOSD{
			{CoreName: "PC8801", RbfName: "PC88", Repo: "MiSTer-devel/PC88_MiSTer", ConfStrRaw: "PC8801;;R6,Reset"},
			{CoreName: "SNES", RbfName: "SNES", Repo: "MiSTer-devel/SNES_MiSTer", ConfStrRaw: "SNES;;T0,Reset"},
		},
	}
	data, _ := json.Marshal(db)
	tmpDir := t.TempDir()
	tmpFile := filepath.Join(tmpDir, "confstr_db.json")
	os.WriteFile(tmpFile, data, 0644)

	oldPath := confstrDBPath
	confstrDBPath = tmpFile
	confstrDBOnce = syncOnce()
	confstrDB = nil
	defer func() {
		confstrDBPath = oldPath
		confstrDBOnce = syncOnce()
		confstrDB = nil
	}()

	tests := []struct {
		input, want string
	}{
		{"PC88_20250918", "PC8801"},  // strip date + fuzzy match via rbf_name
		{"SNES_20250605", "SNES"},    // strip date + exact match
		{"SNES", "SNES"},             // already clean
		{"UnknownCore_20250101", "UnknownCore"}, // strip date, no DB match
		{"Menu", "Menu"},             // no date suffix, no DB match
	}
	for _, tt := range tests {
		got := NormalizeCoreName(tt.input)
		if got != tt.want {
			t.Errorf("NormalizeCoreName(%q) = %q, want %q", tt.input, got, tt.want)
		}
	}
}
