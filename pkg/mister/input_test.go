package mister

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"testing"

	"github.com/bendahl/uinput"
)

// mockKeyboard records all key events for verification.
type mockKeyboard struct {
	mu     sync.Mutex
	events []keyEvent
	err    error // if set, all calls return this error
}

type keyEvent struct {
	action string // "press", "down", "up"
	code   int
}

func (m *mockKeyboard) KeyPress(key int) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.err != nil {
		return m.err
	}
	m.events = append(m.events, keyEvent{"press", key})
	return nil
}

func (m *mockKeyboard) KeyDown(key int) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.err != nil {
		return m.err
	}
	m.events = append(m.events, keyEvent{"down", key})
	return nil
}

func (m *mockKeyboard) KeyUp(key int) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.err != nil {
		return m.err
	}
	m.events = append(m.events, keyEvent{"up", key})
	return nil
}

func (m *mockKeyboard) Close() error {
	return nil
}

func (m *mockKeyboard) getEvents() []keyEvent {
	m.mu.Lock()
	defer m.mu.Unlock()
	cp := make([]keyEvent, len(m.events))
	copy(cp, m.events)
	return cp
}

// withMockKeyboard installs a mock keyboard and returns it.
// Restores the original creator on cleanup.
func withMockKeyboard(t *testing.T) *mockKeyboard {
	t.Helper()
	mock := &mockKeyboard{}
	origCreator := keyboardCreator
	origInst := kbInst

	keyboardCreator = func() (KeyboardDevice, error) {
		return mock, nil
	}
	kbInst = nil // force re-creation

	t.Cleanup(func() {
		keyboardCreator = origCreator
		kbMu.Lock()
		kbInst = origInst
		kbMu.Unlock()
	})

	return mock
}

func TestPressKey_Named(t *testing.T) {
	mock := withMockKeyboard(t)

	if err := PressKey("osd"); err != nil {
		t.Fatalf("PressKey(osd): %v", err)
	}

	events := mock.getEvents()
	if len(events) != 2 {
		t.Fatalf("expected 1 event, got %d: %v", len(events), events)
	}
	if events[0].action != "down" {
		t.Errorf("expected down, got %s", events[0].action)
	}
	if events[0].code != KeyNames["f12"] {
		t.Errorf("expected F12 code %d, got %d", KeyNames["f12"], events[0].code)
	}
}

func TestPressKey_NamedCombo(t *testing.T) {
	mock := withMockKeyboard(t)

	if err := PressKey("core_select"); err != nil {
		t.Fatalf("PressKey(core_select): %v", err)
	}

	events := mock.getEvents()
	// core_select = leftalt + f12 → down, down, up, up
	if len(events) != 4 {
		t.Fatalf("expected 4 events, got %d: %v", len(events), events)
	}
	if events[0].action != "down" || events[1].action != "down" {
		t.Error("expected two key-down events first")
	}
	if events[2].action != "up" || events[3].action != "up" {
		t.Error("expected two key-up events after")
	}
}

func TestPressKey_CaseInsensitive(t *testing.T) {
	mock := withMockKeyboard(t)

	if err := PressKey("OSD"); err != nil {
		t.Fatalf("PressKey(OSD): %v", err)
	}
	if len(mock.getEvents()) != 2 {
		t.Error("expected 2 events for case-insensitive key")
	}
}

func TestPressKey_Unknown(t *testing.T) {
	withMockKeyboard(t)

	err := PressKey("nonexistent_key_xyz")
	if err == nil {
		t.Fatal("expected error for unknown key")
	}
}

func TestPressRawKey(t *testing.T) {
	mock := withMockKeyboard(t)

	if err := PressRawKey(28); err != nil {
		t.Fatalf("PressRawKey(28): %v", err)
	}

	events := mock.getEvents()
	if len(events) != 2 {
		t.Fatalf("expected 1 event, got %d", len(events))
	}
	if events[0].code != 28 {
		t.Errorf("expected code 28, got %d", events[0].code)
	}
}

func TestPressCombo(t *testing.T) {
	mock := withMockKeyboard(t)

	if err := PressCombo([]string{"leftalt", "f12"}); err != nil {
		t.Fatalf("PressCombo: %v", err)
	}

	events := mock.getEvents()
	if len(events) != 4 {
		t.Fatalf("expected 4 events (2 down + 2 up), got %d: %v", len(events), events)
	}
	// Down in order
	if events[0].action != "down" || events[0].code != KeyNames["leftalt"] {
		t.Errorf("event[0]: expected down leftalt, got %v", events[0])
	}
	if events[1].action != "down" || events[1].code != KeyNames["f12"] {
		t.Errorf("event[1]: expected down f12, got %v", events[1])
	}
	// Up in reverse
	if events[2].action != "up" || events[2].code != KeyNames["f12"] {
		t.Errorf("event[2]: expected up f12, got %v", events[2])
	}
	if events[3].action != "up" || events[3].code != KeyNames["leftalt"] {
		t.Errorf("event[3]: expected up leftalt, got %v", events[3])
	}
}

func TestPressCombo_Empty(t *testing.T) {
	withMockKeyboard(t)

	err := PressCombo(nil)
	if err == nil {
		t.Fatal("expected error for empty combo")
	}
}

func TestPressCombo_UnknownKey(t *testing.T) {
	withMockKeyboard(t)

	err := PressCombo([]string{"leftalt", "nonexistent"})
	if err == nil {
		t.Fatal("expected error for unknown key in combo")
	}
}

func TestKeyboardLazyInit(t *testing.T) {
	created := 0
	mock := &mockKeyboard{}

	origCreator := keyboardCreator
	origInst := kbInst

	keyboardCreator = func() (KeyboardDevice, error) {
		created++
		return mock, nil
	}
	kbMu.Lock()
	kbInst = nil
	kbMu.Unlock()

	t.Cleanup(func() {
		keyboardCreator = origCreator
		kbMu.Lock()
		kbInst = origInst
		kbMu.Unlock()
	})

	// Multiple calls should only create once
	PressKey("osd")
	PressKey("menu")
	PressRawKey(1)

	if created != 1 {
		t.Errorf("expected keyboard created once, got %d", created)
	}
}

func TestKeyboardCreationError(t *testing.T) {
	origCreator := keyboardCreator
	origInst := kbInst

	keyboardCreator = func() (KeyboardDevice, error) {
		return nil, fmt.Errorf("no /dev/uinput")
	}
	kbMu.Lock()
	kbInst = nil
	kbMu.Unlock()

	t.Cleanup(func() {
		keyboardCreator = origCreator
		kbMu.Lock()
		kbInst = origInst
		kbMu.Unlock()
	})

	err := PressKey("osd")
	if err == nil {
		t.Fatal("expected error when keyboard creation fails")
	}
}

func TestCloseKeyboard(t *testing.T) {
	mock := withMockKeyboard(t)

	// Use the keyboard to force creation
	PressKey("osd")
	_ = mock

	// Close should nil out the instance
	CloseKeyboard()

	kbMu.Lock()
	inst := kbInst
	kbMu.Unlock()

	if inst != nil {
		t.Error("expected kbInst to be nil after CloseKeyboard")
	}
}

func TestKeyNames_AllNamedKeys(t *testing.T) {
	// Verify all required named keys exist
	required := []string{
		"up", "down", "left", "right",
		"confirm", "menu", "osd", "pair_bluetooth",
		"console", "back",
		"volume_up", "volume_down", "volume_mute",
	}
	for _, name := range required {
		if _, ok := KeyNames[name]; !ok {
			t.Errorf("missing required key name: %s", name)
		}
	}
}

func TestNamedCombos_AllRequired(t *testing.T) {
	required := []string{
		"core_select", "screenshot", "user", "reset",
	}
	for _, name := range required {
		if _, ok := namedCombos[name]; !ok {
			t.Errorf("missing required named combo: %s", name)
		}
	}
}

func TestPressCombo_KeyDownError(t *testing.T) {
	// Test that partial key-down errors release already-pressed keys
	callCount := 0
	mock := &mockKeyboard{}
	origErr := mock.err

	origCreator := keyboardCreator
	origInst := kbInst

	// Create a keyboard that fails on second KeyDown
	failOnSecond := &failingKeyboard{failAt: 1}
	keyboardCreator = func() (KeyboardDevice, error) {
		callCount++
		return failOnSecond, nil
	}
	kbMu.Lock()
	kbInst = nil
	kbMu.Unlock()

	t.Cleanup(func() {
		keyboardCreator = origCreator
		kbMu.Lock()
		kbInst = origInst
		kbMu.Unlock()
		mock.err = origErr
	})

	err := PressCombo([]string{"leftalt", "f12"})
	if err == nil {
		t.Fatal("expected error from failing KeyDown")
	}

	// Should have released the first key
	if len(failOnSecond.upCalls) == 0 {
		t.Error("expected KeyUp to be called for cleanup")
	}
}

// failingKeyboard fails KeyDown on the Nth call.
type failingKeyboard struct {
	downCount int
	failAt    int
	upCalls   []int
}

func (f *failingKeyboard) KeyPress(key int) error { return nil }

func (f *failingKeyboard) KeyDown(key int) error {
	if f.downCount == f.failAt {
		f.downCount++
		return fmt.Errorf("simulated failure")
	}
	f.downCount++
	return nil
}

func (f *failingKeyboard) KeyUp(key int) error {
	f.upCalls = append(f.upCalls, key)
	return nil
}

func (f *failingKeyboard) Close() error { return nil }

// --- Gamepad tests ---

// mockGamepad records all gamepad events for verification.
type mockGamepad struct {
	mu     sync.Mutex
	events []gpEvent
	err    error
}

type gpEvent struct {
	action string // "press", "down", "up", "hat_press", "hat_release"
	code   int
}

func (m *mockGamepad) ButtonPress(key int) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.err != nil {
		return m.err
	}
	m.events = append(m.events, gpEvent{"press", key})
	return nil
}

func (m *mockGamepad) ButtonDown(key int) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.err != nil {
		return m.err
	}
	m.events = append(m.events, gpEvent{"down", key})
	return nil
}

func (m *mockGamepad) ButtonUp(key int) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.err != nil {
		return m.err
	}
	m.events = append(m.events, gpEvent{"up", key})
	return nil
}

func (m *mockGamepad) HatPress(direction uinput.HatDirection) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.err != nil {
		return m.err
	}
	m.events = append(m.events, gpEvent{"hat_press", int(direction)})
	return nil
}

func (m *mockGamepad) HatRelease(direction uinput.HatDirection) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.err != nil {
		return m.err
	}
	m.events = append(m.events, gpEvent{"hat_release", int(direction)})
	return nil
}

func (m *mockGamepad) Close() error { return nil }

func (m *mockGamepad) getEvents() []gpEvent {
	m.mu.Lock()
	defer m.mu.Unlock()
	cp := make([]gpEvent, len(m.events))
	copy(cp, m.events)
	return cp
}

// withMockGamepad installs a mock gamepad and returns it.
func withMockGamepad(t *testing.T) *mockGamepad {
	t.Helper()
	mock := &mockGamepad{}
	origCreator := gamepadCreator
	origInst := gpInst

	gamepadCreator = func() (GamepadDevice, error) {
		return mock, nil
	}
	gpInst = nil

	t.Cleanup(func() {
		gamepadCreator = origCreator
		gpMu.Lock()
		gpInst = origInst
		gpMu.Unlock()
	})

	return mock
}

func TestPressGamepadButton_Named(t *testing.T) {
	mock := withMockGamepad(t)

	if err := PressGamepadButton("a"); err != nil {
		t.Fatalf("PressGamepadButton(a): %v", err)
	}

	events := mock.getEvents()
	if len(events) != 2 {
		t.Fatalf("expected 2 events, got %d: %v", len(events), events)
	}
	if events[0].action != "down" {
		t.Errorf("expected down, got %s", events[0].action)
	}
	if events[0].code != GamepadButtons["a"] {
		t.Errorf("expected button A code %d, got %d", GamepadButtons["a"], events[0].code)
	}
	if events[1].action != "up" {
		t.Errorf("expected up, got %s", events[1].action)
	}
}

func TestPressGamepadButton_AllNames(t *testing.T) {
	required := []string{"a", "b", "x", "y", "start", "select", "l", "r", "coin"}
	for _, name := range required {
		if _, ok := GamepadButtons[name]; !ok {
			t.Errorf("missing required gamepad button: %s", name)
		}
	}
}

func TestPressGamepadButton_CaseInsensitive(t *testing.T) {
	mock := withMockGamepad(t)

	if err := PressGamepadButton("START"); err != nil {
		t.Fatalf("PressGamepadButton(START): %v", err)
	}
	if len(mock.getEvents()) != 2 {
		t.Error("expected 2 events for case-insensitive button")
	}
}

func TestPressGamepadButton_Unknown(t *testing.T) {
	withMockGamepad(t)

	err := PressGamepadButton("nonexistent")
	if err == nil {
		t.Fatal("expected error for unknown gamepad button")
	}
}

func TestPressGamepadRaw(t *testing.T) {
	mock := withMockGamepad(t)

	if err := PressGamepadRaw(0x130); err != nil {
		t.Fatalf("PressGamepadRaw(0x130): %v", err)
	}

	events := mock.getEvents()
	if len(events) != 2 {
		t.Fatalf("expected 2 events, got %d", len(events))
	}
	if events[0].code != 0x130 {
		t.Errorf("expected code 0x130, got %d", events[0].code)
	}
}

func TestGamepadDPad(t *testing.T) {
	for _, dir := range []string{"up", "down", "left", "right"} {
		t.Run(dir, func(t *testing.T) {
			mock := withMockGamepad(t)

			if err := GamepadDPad(dir); err != nil {
				t.Fatalf("GamepadDPad(%s): %v", dir, err)
			}

			events := mock.getEvents()
			if len(events) != 2 {
				t.Fatalf("expected 2 events, got %d: %v", len(events), events)
			}
			if events[0].action != "hat_press" {
				t.Errorf("expected hat_press, got %s", events[0].action)
			}
			if events[1].action != "hat_release" {
				t.Errorf("expected hat_release, got %s", events[1].action)
			}
			expected := DPadDirections[dir]
			if events[0].code != int(expected) {
				t.Errorf("expected direction %d, got %d", expected, events[0].code)
			}
		})
	}
}

func TestGamepadDPad_Unknown(t *testing.T) {
	withMockGamepad(t)

	err := GamepadDPad("diagonal")
	if err == nil {
		t.Fatal("expected error for unknown d-pad direction")
	}
}

func TestGamepadDPad_CaseInsensitive(t *testing.T) {
	mock := withMockGamepad(t)

	if err := GamepadDPad("UP"); err != nil {
		t.Fatalf("GamepadDPad(UP): %v", err)
	}
	if len(mock.getEvents()) != 2 {
		t.Error("expected 2 events for case-insensitive direction")
	}
}

func TestGamepadLazyInit(t *testing.T) {
	created := 0
	mock := &mockGamepad{}

	origCreator := gamepadCreator
	origInst := gpInst

	gamepadCreator = func() (GamepadDevice, error) {
		created++
		return mock, nil
	}
	gpMu.Lock()
	gpInst = nil
	gpMu.Unlock()

	t.Cleanup(func() {
		gamepadCreator = origCreator
		gpMu.Lock()
		gpInst = origInst
		gpMu.Unlock()
	})

	PressGamepadButton("a")
	PressGamepadButton("b")
	GamepadDPad("up")

	if created != 1 {
		t.Errorf("expected gamepad created once, got %d", created)
	}
}

func TestGamepadCreationError(t *testing.T) {
	origCreator := gamepadCreator
	origInst := gpInst

	gamepadCreator = func() (GamepadDevice, error) {
		return nil, fmt.Errorf("no /dev/uinput")
	}
	gpMu.Lock()
	gpInst = nil
	gpMu.Unlock()

	t.Cleanup(func() {
		gamepadCreator = origCreator
		gpMu.Lock()
		gpInst = origInst
		gpMu.Unlock()
	})

	err := PressGamepadButton("a")
	if err == nil {
		t.Fatal("expected error when gamepad creation fails")
	}
}

func TestCloseGamepad(t *testing.T) {
	mock := withMockGamepad(t)

	PressGamepadButton("a")
	_ = mock

	CloseGamepad()

	gpMu.Lock()
	inst := gpInst
	gpMu.Unlock()

	if inst != nil {
		t.Error("expected gpInst to be nil after CloseGamepad")
	}
}

func TestKeyboardBackwardCompat(t *testing.T) {
	// Verify existing keyboard commands still work alongside new aliases
	mock := withMockKeyboard(t)

	if err := PressKey("osd"); err != nil {
		t.Fatalf("PressKey(osd) failed after gamepad additions: %v", err)
	}
	if len(mock.getEvents()) != 2 {
		t.Error("keyboard osd should still produce 2 events")
	}
}

func TestKeyboardArcadeAliases(t *testing.T) {
	// Verify the new keyboard aliases exist
	aliases := map[string]int{
		"coin":    6,
		"start":   2,
		"p2start": 3,
		"p2coin":  7,
	}
	for name, expected := range aliases {
		code, ok := KeyNames[name]
		if !ok {
			t.Errorf("missing keyboard alias: %s", name)
			continue
		}
		if code != expected {
			t.Errorf("keyboard alias %s: expected %d, got %d", name, expected, code)
		}
	}
}

// withTestConfStrDB sets up a temporary confstr DB for testing.
func withTestConfStrDB(t *testing.T, db ConfStrDB) {
	t.Helper()
	data, err := json.Marshal(db)
	if err != nil {
		t.Fatal(err)
	}
	tmpDir := t.TempDir()
	tmpFile := filepath.Join(tmpDir, "confstr_db.json")
	if err := os.WriteFile(tmpFile, data, 0644); err != nil {
		t.Fatal(err)
	}
	oldPath := confstrDBPath
	confstrDBPath = tmpFile
	confstrDBOnce = syncOnce()
	confstrDB = nil
	t.Cleanup(func() {
		confstrDBPath = oldPath
		confstrDBOnce = syncOnce()
		confstrDB = nil
	})
}

func TestOSDNavigateTo(t *testing.T) {
	mock := withMockKeyboard(t)

	// MYCORE label(skip), file_load(0), sep(1), option(2), trigger Reset(3)
	withTestConfStrDB(t, ConfStrDB{
		Cores: []CoreOSD{
			{CoreName: "MYCORE", ConfStrRaw: "MYCORE;;FS0,BIN,Load;-;O1,Opt,A,B;T0,Reset"},
		},
	})

	err := OSDNavigateTo("MYCORE", "Reset")
	if err != nil {
		t.Fatal(err)
	}

	events := mock.getEvents()
	// F12 (down+up) + 3x Down (down+up each) + Enter (down+up) = 10 events
	expectedCount := 2 + 3*2 + 2
	if len(events) != expectedCount {
		t.Fatalf("expected %d events, got %d: %v", expectedCount, len(events), events)
	}
	// First event: F12 key down
	if events[0].code != KeyNames["f12"] {
		t.Errorf("first event should be F12, got code %d", events[0].code)
	}
	// Last two events: Enter down/up
	if events[len(events)-2].code != KeyNames["enter"] {
		t.Errorf("second-to-last event should be Enter, got code %d", events[len(events)-2].code)
	}
}

func TestOSDResetByCore(t *testing.T) {
	mock := withMockKeyboard(t)

	// MYCORE label(skip), sep(0), Reset(1)
	withTestConfStrDB(t, ConfStrDB{
		Cores: []CoreOSD{
			{CoreName: "MYCORE", ConfStrRaw: "MYCORE;;-;R0,Reset"},
		},
	})

	err := OSDResetByCore("MYCORE")
	if err != nil {
		t.Fatal(err)
	}

	events := mock.getEvents()
	// F12 (2) + 1x Down (2) + Enter (2) = 6 events
	if len(events) != 6 {
		t.Fatalf("expected 6 events, got %d: %v", len(events), events)
	}
	_ = mock
}

func TestOSDNavigateTo_CoreNotFound(t *testing.T) {
	withMockKeyboard(t)
	withTestConfStrDB(t, ConfStrDB{})

	err := OSDNavigateTo("NonExistent", "Reset")
	if err == nil {
		t.Fatal("expected error for non-existent core")
	}
}

func TestOSDNavigateTo_TargetNotFound(t *testing.T) {
	withMockKeyboard(t)
	withTestConfStrDB(t, ConfStrDB{
		Cores: []CoreOSD{
			{CoreName: "MYCORE", ConfStrRaw: "MYCORE;;T0,Reset"},
		},
	})

	err := OSDNavigateTo("MYCORE", "NonExistent")
	if err == nil {
		t.Fatal("expected error for non-existent target")
	}
}
