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

// --- TypeText tests ---

func TestCharToKey_AllPrintableASCII(t *testing.T) {
	// Verify all printable ASCII characters are mapped (space through ~)
	for ch := rune(' '); ch <= '~'; ch++ {
		if _, ok := charToKey[ch]; !ok {
			t.Errorf("missing charToKey mapping for %q (U+%04X)", ch, ch)
		}
	}
}

func TestCharToKey_SpecialChars(t *testing.T) {
	// Verify newline and tab are mapped
	if _, ok := charToKey['\n']; !ok {
		t.Error("missing charToKey mapping for newline")
	}
	if _, ok := charToKey['\t']; !ok {
		t.Error("missing charToKey mapping for tab")
	}
}

func TestCharToKey_ShiftCorrectness(t *testing.T) {
	// Uppercase letters need shift
	for ch := 'A'; ch <= 'Z'; ch++ {
		m := charToKey[ch]
		if !m.needShift {
			t.Errorf("expected shift for %q", ch)
		}
	}
	// Lowercase letters don't need shift
	for ch := 'a'; ch <= 'z'; ch++ {
		m := charToKey[ch]
		if m.needShift {
			t.Errorf("did not expect shift for %q", ch)
		}
	}
	// Digits don't need shift
	for ch := '0'; ch <= '9'; ch++ {
		m := charToKey[ch]
		if m.needShift {
			t.Errorf("did not expect shift for %q", ch)
		}
	}
	// Shifted symbols
	shiftedSymbols := "!@#$%^&*()_+{}|:\"<>?~"
	for _, ch := range shiftedSymbols {
		m, ok := charToKey[ch]
		if !ok {
			t.Errorf("missing mapping for shifted symbol %q", ch)
			continue
		}
		if !m.needShift {
			t.Errorf("expected shift for %q", ch)
		}
	}
}

func TestTypeText_Simple(t *testing.T) {
	mock := withMockKeyboard(t)

	if err := TypeText("ab"); err != nil {
		t.Fatalf("TypeText(ab): %v", err)
	}

	events := mock.getEvents()
	// 'a': down(a), up(a); 'b': down(b), up(b) = 4 events
	if len(events) != 4 {
		t.Fatalf("expected 4 events, got %d: %v", len(events), events)
	}
	if events[0].code != uinput.KeyA || events[0].action != "down" {
		t.Errorf("event[0]: expected down A, got %v", events[0])
	}
	if events[1].code != uinput.KeyA || events[1].action != "up" {
		t.Errorf("event[1]: expected up A, got %v", events[1])
	}
	if events[2].code != uinput.KeyB || events[2].action != "down" {
		t.Errorf("event[2]: expected down B, got %v", events[2])
	}
}

func TestTypeText_Shifted(t *testing.T) {
	mock := withMockKeyboard(t)

	if err := TypeText("A"); err != nil {
		t.Fatalf("TypeText(A): %v", err)
	}

	events := mock.getEvents()
	// 'A': down(shift), down(a), up(a), up(shift) = 4 events
	if len(events) != 4 {
		t.Fatalf("expected 4 events, got %d: %v", len(events), events)
	}
	if events[0].code != uinput.KeyLeftshift || events[0].action != "down" {
		t.Errorf("event[0]: expected down leftshift, got %v", events[0])
	}
	if events[1].code != uinput.KeyA || events[1].action != "down" {
		t.Errorf("event[1]: expected down A, got %v", events[1])
	}
	if events[2].code != uinput.KeyA || events[2].action != "up" {
		t.Errorf("event[2]: expected up A, got %v", events[2])
	}
	if events[3].code != uinput.KeyLeftshift || events[3].action != "up" {
		t.Errorf("event[3]: expected up leftshift, got %v", events[3])
	}
}

func TestTypeText_MixedCase(t *testing.T) {
	mock := withMockKeyboard(t)

	if err := TypeText("aB"); err != nil {
		t.Fatalf("TypeText(aB): %v", err)
	}

	events := mock.getEvents()
	// 'a': down(a), up(a); 'B': down(shift), down(b), up(b), up(shift) = 6 events
	if len(events) != 6 {
		t.Fatalf("expected 6 events, got %d: %v", len(events), events)
	}
}

func TestTypeText_SpecialChars(t *testing.T) {
	mock := withMockKeyboard(t)

	// Test newline sends Enter
	if err := TypeText("\n"); err != nil {
		t.Fatalf("TypeText(newline): %v", err)
	}

	events := mock.getEvents()
	if len(events) != 2 {
		t.Fatalf("expected 2 events, got %d: %v", len(events), events)
	}
	if events[0].code != uinput.KeyEnter {
		t.Errorf("expected Enter keycode, got %d", events[0].code)
	}
}

func TestTypeText_ShiftedSymbol(t *testing.T) {
	mock := withMockKeyboard(t)

	// '*' = shift+8
	if err := TypeText("*"); err != nil {
		t.Fatalf("TypeText(*): %v", err)
	}

	events := mock.getEvents()
	if len(events) != 4 {
		t.Fatalf("expected 4 events, got %d: %v", len(events), events)
	}
	if events[0].code != uinput.KeyLeftshift {
		t.Errorf("expected shift down first, got code %d", events[0].code)
	}
	if events[1].code != uinput.Key8 {
		t.Errorf("expected Key8, got code %d", events[1].code)
	}
}

func TestTypeText_C64Command(t *testing.T) {
	mock := withMockKeyboard(t)

	// Simulate typing LOAD"*",8,1\n
	if err := TypeText("LOAD\"*\",8,1\n"); err != nil {
		t.Fatalf("TypeText(LOAD...): %v", err)
	}

	events := mock.getEvents()
	// L(shift+l=4) O(shift+o=4) A(shift+a=4) D(shift+d=4)
	// "(shift+'=4) *(shift+8=4) "(shift+'=4)
	// ,(2) 8(2) ,(2) 1(2) Enter(2)
	// = 4*4 + 3*4 + 4*2 + 2 = 16 + 12 + 8 + 2 = 38
	expectedCount := 38
	if len(events) != expectedCount {
		t.Fatalf("expected %d events, got %d", expectedCount, len(events))
	}
}

func TestTypeText_Empty(t *testing.T) {
	withMockKeyboard(t)

	// Empty string should succeed with no events
	if err := TypeText(""); err != nil {
		t.Fatalf("TypeText(empty): %v", err)
	}
}

func TestTypeText_UnsupportedChar(t *testing.T) {
	withMockKeyboard(t)

	err := TypeText("hello\x01world")
	if err == nil {
		t.Fatal("expected error for unsupported character")
	}
}

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

func TestOSDNavigateTo_SubPage(t *testing.T) {
	mock := withMockKeyboard(t)

	// Core with sub-page: top-level has mount(0), sep(1), Aspect(2), VideoAudio(3), sep(4), Reset(5)
	// Sub-page 1 has Widescreen(0), VCrop(1)
	withTestConfStrDB(t, ConfStrDB{
		Cores: []CoreOSD{
			{CoreName: "TESTCORE", ConfStrRaw: "TESTCORE;;S0,CHDCUE,CD ROM;-;O12,Aspect Ratio,Original,Full Screen;P1,Video & Audio;-;R0,Reset;P1O34,Widescreen,No,Yes;P1O5,Vertical Crop,No,Yes"},
		},
	})

	err := OSDNavigateTo("TESTCORE", "Widescreen")
	if err != nil {
		t.Fatal(err)
	}

	events := mock.getEvents()
	// F12 (down+up=2) + 3x Down to page entry (3*2=6) + Right to enter sub-page (2)
	// + 0x Down (item is at position 0) + Enter (2) = 12 events
	expectedCount := 2 + 3*2 + 2 + 0 + 2
	if len(events) != expectedCount {
		t.Fatalf("expected %d events, got %d: %v", expectedCount, len(events), events)
	}
	// First event: F12
	if events[0].code != KeyNames["f12"] {
		t.Errorf("first event should be F12, got code %d", events[0].code)
	}
	// After 3 downs, should press Right
	rightIdx := 2 + 3*2 // F12(2) + 3 downs(6)
	if events[rightIdx].code != KeyNames["right"] {
		t.Errorf("expected Right arrow at index %d, got code %d", rightIdx, events[rightIdx].code)
	}
	// Last two events: Enter
	if events[len(events)-2].code != KeyNames["enter"] {
		t.Errorf("second-to-last event should be Enter, got code %d", events[len(events)-2].code)
	}
}

func TestOSDNavigateTo_SubPageDeepItem(t *testing.T) {
	mock := withMockKeyboard(t)

	withTestConfStrDB(t, ConfStrDB{
		Cores: []CoreOSD{
			{CoreName: "TESTCORE", ConfStrRaw: "TESTCORE;;S0,CHDCUE,CD ROM;P1,Settings;R0,Reset;P1O12,Opt1,A,B;P1O34,Opt2,C,D;P1O5,Opt3,E,F"},
		},
	})

	// Navigate to Opt3 which is at sub-page position 2
	err := OSDNavigateTo("TESTCORE", "Opt3")
	if err != nil {
		t.Fatal(err)
	}

	events := mock.getEvents()
	// F12(2) + 1x Down to page entry(2) + Right(2) + 2x Down within sub-page(4) + Enter(2) = 12
	expectedCount := 2 + 1*2 + 2 + 2*2 + 2
	if len(events) != expectedCount {
		t.Fatalf("expected %d events, got %d: %v", expectedCount, len(events), events)
	}
}
