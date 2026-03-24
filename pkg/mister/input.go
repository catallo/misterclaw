package mister

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"os"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/bendahl/uinput"
)

// GamepadDevice abstracts uinput.Gamepad for testing.
type GamepadDevice interface {
	ButtonPress(key int) error
	ButtonDown(key int) error
	ButtonUp(key int) error
	HatPress(direction uinput.HatDirection) error
	HatRelease(direction uinput.HatDirection) error
	Close() error
}

// gamepadCreator is the function used to create a gamepad device.
// Override in tests to inject a mock.
var gamepadCreator = func() (GamepadDevice, error) {
	return createMiSTerGamepad()
}

var (
	gpMu   sync.Mutex
	gpInst GamepadDevice
)

// getGamepad returns the lazily-created shared gamepad instance.
func getGamepad() (GamepadDevice, error) {
	gpMu.Lock()
	defer gpMu.Unlock()
	if gpInst != nil {
		return gpInst, nil
	}
	gp, err := gamepadCreator()
	if err != nil {
		return nil, fmt.Errorf("creating gamepad device: %w", err)
	}
	gpInst = gp
	// MiSTer needs time to register the new input device
	time.Sleep(200 * time.Millisecond)
	return gpInst, nil
}

// InitGamepad eagerly creates the gamepad device so MiSTer can register it.
func InitGamepad() (GamepadDevice, error) {
	return getGamepad()
}

// CloseGamepad closes the shared gamepad device if open.
func CloseGamepad() {
	gpMu.Lock()
	defer gpMu.Unlock()
	if gpInst != nil {
		gpInst.Close()
		gpInst = nil
	}
}

// GamepadButtons maps friendly button names to Linux BTN codes.
// MiSTer maps the generic 0079:0006 controller using joystick button codes
// (BTN_TRIGGER through BTN_BASE4), NOT standard gamepad codes.
// Default map slot order: [4]=A [5]=B [6]=X [7]=Y [8]=L [9]=R [10]=Select [11]=Start
var GamepadButtons = map[string]int{
	"a":      293, // BTN_PINKIE  (0x125) - mapped as button A/Fire
	"b":      292, // BTN_TOP2    (0x124)
	"x":      291, // BTN_TOP     (0x123)
	"y":      290, // BTN_THUMB2  (0x122)
	"l":      289, // BTN_THUMB   (0x121)
	"r":      288, // BTN_TRIGGER (0x120)
	"select": 296, // BTN_BASE3   (0x128)
	"coin":   296, // BTN_BASE3   (0x128) - same as select
	"start":  297, // BTN_BASE4   (0x129)
}

// DPadDirections maps direction names to uinput HatDirection values.
var DPadDirections = map[string]uinput.HatDirection{
	"up":    uinput.HatUp,
	"down":  uinput.HatDown,
	"left":  uinput.HatLeft,
	"right": uinput.HatRight,
}

// PressGamepadButton presses a named gamepad button (down + 40ms + up).
func PressGamepadButton(name string) error {
	name = strings.ToLower(name)
	code, ok := GamepadButtons[name]
	if !ok {
		return fmt.Errorf("unknown gamepad button: %q", name)
	}
	return PressGamepadRaw(code)
}

// PressGamepadRaw presses a gamepad button by raw code (down + 40ms + up).
func PressGamepadRaw(code int) error {
	gp, err := getGamepad()
	if err != nil {
		return err
	}
	if err := gp.ButtonDown(code); err != nil {
		return err
	}
	time.Sleep(40 * time.Millisecond)
	return gp.ButtonUp(code)
}

// GamepadDPad presses a d-pad direction via Hat (press + 40ms + release).
func GamepadDPad(direction string) error {
	direction = strings.ToLower(direction)
	dir, ok := DPadDirections[direction]
	if !ok {
		return fmt.Errorf("unknown d-pad direction: %q (use: up, down, left, right)", direction)
	}
	gp, err := getGamepad()
	if err != nil {
		return err
	}
	if err := gp.HatPress(dir); err != nil {
		return err
	}
	time.Sleep(40 * time.Millisecond)
	return gp.HatRelease(dir)
}

// ---------------------------------------------------------------------------
// Custom uinput gamepad device for MiSTer
//
// MiSTer's controller map for the generic 0079:0006 device expects old-style
// joystick button codes (BTN_TRIGGER=288 through BTN_BASE4=297). The standard
// uinput.CreateGamepad only registers modern gamepad codes (BTN_SOUTH=304+),
// so those events are silently dropped by the kernel. We create the device
// manually, registering both sets of button codes.
// ---------------------------------------------------------------------------

// uinput ioctl constants (from linux/uinput.h).
const (
	uiDevCreate = 0x5501
	uiDevDestroy = 0x5502
	uiSetEvBit  = 0x40045564
	uiSetKeyBit = 0x40045565
	uiSetAbsBit = 0x40045567

	evSyn = 0x00
	evKey = 0x01
	evAbs = 0x03

	synReport = 0

	btnStatePressed  = 1
	btnStateReleased = 0

	busUsb = 0x03

	uinputMaxNameSize = 80
	absSize           = 64
)

// Absolute axis codes.
const (
	axAbsX     = 0x00
	axAbsY     = 0x01
	axAbsZ     = 0x02
	axAbsRX    = 0x03
	axAbsRY    = 0x04
	axAbsRZ    = 0x05
	axAbsHat0X = 0x10
	axAbsHat0Y = 0x11
)

type uiInputID struct {
	Bustype uint16
	Vendor  uint16
	Product uint16
	Version uint16
}

type uiUserDev struct {
	Name       [uinputMaxNameSize]byte
	ID         uiInputID
	EffectsMax uint32
	Absmax     [absSize]int32
	Absmin     [absSize]int32
	Absfuzz    [absSize]int32
	Absflat    [absSize]int32
}

type uiInputEvent struct {
	Time  syscall.Timeval
	Type  uint16
	Code  uint16
	Value int32
}

// misterGamepad implements GamepadDevice using raw uinput syscalls.
type misterGamepad struct {
	fd *os.File
}

func createMiSTerGamepad() (GamepadDevice, error) {
	fd, err := os.OpenFile("/dev/uinput", syscall.O_WRONLY|syscall.O_NONBLOCK, 0660)
	if err != nil {
		return nil, fmt.Errorf("open /dev/uinput: %w", err)
	}

	// Register EV_KEY
	if err := uiIoctl(fd, uiSetEvBit, uintptr(evKey)); err != nil {
		fd.Close()
		return nil, fmt.Errorf("register EV_KEY: %w", err)
	}

	// Old joystick buttons: BTN_TRIGGER(288) through BTN_BASE4(297)
	// Modern gamepad buttons: BTN_GAMEPAD(304) through BTN_MODE(316)
	for code := uint16(288); code <= 297; code++ {
		if err := uiIoctl(fd, uiSetKeyBit, uintptr(code)); err != nil {
			fd.Close()
			return nil, fmt.Errorf("register key %d: %w", code, err)
		}
	}
	for code := uint16(304); code <= 316; code++ {
		if err := uiIoctl(fd, uiSetKeyBit, uintptr(code)); err != nil {
			fd.Close()
			return nil, fmt.Errorf("register key %d: %w", code, err)
		}
	}
	// D-pad buttons (0x220-0x223)
	for code := uint16(0x220); code <= 0x223; code++ {
		if err := uiIoctl(fd, uiSetKeyBit, uintptr(code)); err != nil {
			fd.Close()
			return nil, fmt.Errorf("register key %d: %w", code, err)
		}
	}

	// Register EV_ABS
	if err := uiIoctl(fd, uiSetEvBit, uintptr(evAbs)); err != nil {
		fd.Close()
		return nil, fmt.Errorf("register EV_ABS: %w", err)
	}
	for _, axis := range []uint16{axAbsX, axAbsY, axAbsZ, axAbsRX, axAbsRY, axAbsRZ, axAbsHat0X, axAbsHat0Y} {
		if err := uiIoctl(fd, uiSetAbsBit, uintptr(axis)); err != nil {
			fd.Close()
			return nil, fmt.Errorf("register abs %d: %w", axis, err)
		}
	}

	// Write the uinput_user_dev struct
	var name [uinputMaxNameSize]byte
	copy(name[:], "misterclaw-pad")
	dev := uiUserDev{
		Name: name,
		ID: uiInputID{
			Bustype: busUsb,
			Vendor:  0x0079,
			Product: 0x0006,
			Version: 1,
		},
	}
	buf := new(bytes.Buffer)
	if err := binary.Write(buf, binary.LittleEndian, dev); err != nil {
		fd.Close()
		return nil, fmt.Errorf("serialize uinput dev: %w", err)
	}
	if _, err := fd.Write(buf.Bytes()); err != nil {
		fd.Close()
		return nil, fmt.Errorf("write uinput dev: %w", err)
	}

	// Create the device
	if err := uiIoctl(fd, uiDevCreate, 0); err != nil {
		fd.Close()
		return nil, fmt.Errorf("UI_DEV_CREATE: %w", err)
	}

	// Give kernel time to register the device
	time.Sleep(200 * time.Millisecond)

	return &misterGamepad{fd: fd}, nil
}

func (g *misterGamepad) ButtonPress(key int) error {
	if err := g.ButtonDown(key); err != nil {
		return err
	}
	return g.ButtonUp(key)
}

func (g *misterGamepad) ButtonDown(key int) error {
	return g.sendKeyEvent(uint16(key), btnStatePressed)
}

func (g *misterGamepad) ButtonUp(key int) error {
	return g.sendKeyEvent(uint16(key), btnStateReleased)
}

func (g *misterGamepad) HatPress(direction uinput.HatDirection) error {
	return g.sendHatEvent(direction, true)
}

func (g *misterGamepad) HatRelease(direction uinput.HatDirection) error {
	return g.sendHatEvent(direction, false)
}

func (g *misterGamepad) Close() error {
	if err := uiIoctl(g.fd, uiDevDestroy, 0); err != nil {
		g.fd.Close()
		return err
	}
	return g.fd.Close()
}

func (g *misterGamepad) sendKeyEvent(code uint16, state int) error {
	ev := uiInputEvent{Type: evKey, Code: code, Value: int32(state)}
	if err := g.writeEvent(ev); err != nil {
		return err
	}
	return g.syncEvents()
}

func (g *misterGamepad) sendHatEvent(direction uinput.HatDirection, press bool) error {
	var code uint16
	var value int32

	switch direction {
	case uinput.HatUp:
		code, value = axAbsHat0Y, -1
	case uinput.HatDown:
		code, value = axAbsHat0Y, 1
	case uinput.HatLeft:
		code, value = axAbsHat0X, -1
	case uinput.HatRight:
		code, value = axAbsHat0X, 1
	default:
		return fmt.Errorf("unknown hat direction: %d", direction)
	}
	if !press {
		value = 0
	}

	ev := uiInputEvent{Type: evAbs, Code: code, Value: value}
	if err := g.writeEvent(ev); err != nil {
		return err
	}
	return g.syncEvents()
}

func (g *misterGamepad) writeEvent(ev uiInputEvent) error {
	buf := new(bytes.Buffer)
	if err := binary.Write(buf, binary.LittleEndian, ev); err != nil {
		return fmt.Errorf("serialize input event: %w", err)
	}
	_, err := g.fd.Write(buf.Bytes())
	return err
}

func (g *misterGamepad) syncEvents() error {
	return g.writeEvent(uiInputEvent{Type: evSyn, Code: synReport, Value: 0})
}

func uiIoctl(f *os.File, cmd, ptr uintptr) error {
	_, _, errno := syscall.Syscall(syscall.SYS_IOCTL, f.Fd(), cmd, ptr)
	if errno != 0 {
		return errno
	}
	return nil
}

// KeyboardDevice abstracts uinput.Keyboard for testing.
type KeyboardDevice interface {
	KeyPress(key int) error
	KeyDown(key int) error
	KeyUp(key int) error
	Close() error
}

// keyboardCreator is the function used to create a keyboard device.
// Override in tests to inject a mock.
var keyboardCreator = func() (KeyboardDevice, error) {
	return uinput.CreateKeyboard("/dev/uinput", []byte("misterclaw"))
}

var (
	kbMu   sync.Mutex
	kbInst KeyboardDevice
)

// getKeyboard returns the lazily-created shared keyboard instance.
func getKeyboard() (KeyboardDevice, error) {
	kbMu.Lock()
	defer kbMu.Unlock()
	if kbInst != nil {
		return kbInst, nil
	}
	kb, err := keyboardCreator()
	if err != nil {
		return nil, fmt.Errorf("creating keyboard device: %w", err)
	}
	kbInst = kb
	// MiSTer needs time to register the new input device
	time.Sleep(200 * time.Millisecond)
	return kbInst, nil
}

// InitKeyboard eagerly creates the keyboard device so MiSTer can register it.
func InitKeyboard() (KeyboardDevice, error) {
	return getKeyboard()
}

// CloseKeyboard closes the shared keyboard device if open.
func CloseKeyboard() {
	kbMu.Lock()
	defer kbMu.Unlock()
	if kbInst != nil {
		kbInst.Close()
		kbInst = nil
	}
}

// KeyNames maps friendly names to uinput key codes.
// Includes all standard keys plus MiSTer-specific named actions.
var KeyNames = map[string]int{
	// Arrow keys
	"up":    uinput.KeyUp,
	"down":  uinput.KeyDown,
	"left":  uinput.KeyLeft,
	"right": uinput.KeyRight,

	// MiSTer named actions (single key)
	"confirm":           uinput.KeyEnter,
	"menu":              uinput.KeyEsc,
	"osd":               uinput.KeyF12,
	"pair_bluetooth":    uinput.KeyF11,
	"change_background": uinput.KeyF1,
	"toggle_core_dates": uinput.KeyF2,
	"console":           uinput.KeyF9,
	"exit_console":      uinput.KeyF12,
	"back":              uinput.KeyBackspace,
	"cancel":            uinput.KeyEsc,

	// Volume
	"volume_up":   uinput.KeyVolumeup,
	"volume_down": uinput.KeyVolumedown,
	"volume_mute": uinput.KeyMute,

	// Standard key names (for use in combos and raw access)
	"esc":         uinput.KeyEsc,
	"enter":       uinput.KeyEnter,
	"space":       uinput.KeySpace,
	"tab":         uinput.KeyTab,
	"backspace":   uinput.KeyBackspace,
	"delete":      uinput.KeyDelete,
	"insert":      uinput.KeyInsert,
	"home":        uinput.KeyHome,
	"end":         uinput.KeyEnd,
	"pageup":      uinput.KeyPageup,
	"pagedown":    uinput.KeyPagedown,
	"scrolllock":  uinput.KeyScrolllock,
	"pause":       uinput.KeyPause,
	"sysrq":       uinput.KeySysrq,

	// Function keys
	"f1":  uinput.KeyF1,
	"f2":  uinput.KeyF2,
	"f3":  uinput.KeyF3,
	"f4":  uinput.KeyF4,
	"f5":  uinput.KeyF5,
	"f6":  uinput.KeyF6,
	"f7":  uinput.KeyF7,
	"f8":  uinput.KeyF8,
	"f9":  uinput.KeyF9,
	"f10": uinput.KeyF10,
	"f11": uinput.KeyF11,
	"f12": uinput.KeyF12,

	// Modifiers
	"leftshift":  uinput.KeyLeftshift,
	"rightshift": uinput.KeyRightshift,
	"leftctrl":   uinput.KeyLeftctrl,
	"rightctrl":  uinput.KeyRightctrl,
	"leftalt":    uinput.KeyLeftalt,
	"rightalt":   uinput.KeyRightalt,
	"leftmeta":   uinput.KeyLeftmeta,
	"rightmeta":  uinput.KeyRightmeta,
	"win":        uinput.KeyLeftmeta,

	// Arcade keyboard aliases (default MAME-style mappings)
	"coin":    6, // KEY_5
	"start":   2, // KEY_1
	"p2start": 3, // KEY_2
	"p2coin":  7, // KEY_6
}

// namedCombos maps MiSTer action names that require key combos.
var namedCombos = map[string][]int{
	"core_select":    {uinput.KeyLeftalt, uinput.KeyF12},
	"screenshot":     {uinput.KeyLeftalt, uinput.KeyScrolllock},
	"raw_screenshot": {uinput.KeyLeftalt, uinput.KeyLeftshift, uinput.KeyScrolllock},
	"user":           {uinput.KeyLeftctrl, uinput.KeyLeftalt, uinput.KeyRightalt},
	"reset":          {uinput.KeyLeftshift, uinput.KeyLeftctrl, uinput.KeyLeftalt, uinput.KeyRightalt},
	"computer_osd":   {uinput.KeyLeftmeta, uinput.KeyF12},
}

// PressKey presses a named key or named combo action.
func PressKey(name string) error {
	name = strings.ToLower(name)

	// Check if it's a named combo first
	if codes, ok := namedCombos[name]; ok {
		return pressCombo(codes)
	}

	code, ok := KeyNames[name]
	if !ok {
		return fmt.Errorf("unknown key name: %q", name)
	}

	kb, err := getKeyboard()
	if err != nil {
		return err
	}
	if err := kb.KeyDown(code); err != nil {
		return err
	}
	time.Sleep(40 * time.Millisecond)
	return kb.KeyUp(code)
}

// PressRawKey presses a key by its raw Linux keycode.
func PressRawKey(code int) error {
	kb, err := getKeyboard()
	if err != nil {
		return err
	}
	if err := kb.KeyDown(code); err != nil {
		return err
	}
	time.Sleep(40 * time.Millisecond)
	return kb.KeyUp(code)
}

// PressCombo presses a combination of keys by name (e.g. ["leftalt", "f12"]).
func PressCombo(names []string) error {
	if len(names) == 0 {
		return fmt.Errorf("combo requires at least one key")
	}
	codes := make([]int, len(names))
	for i, name := range names {
		name = strings.ToLower(name)
		code, ok := KeyNames[name]
		if !ok {
			return fmt.Errorf("unknown key name in combo: %q", name)
		}
		codes[i] = code
	}
	return pressCombo(codes)
}

// pressCombo holds down all keys in order, then releases in reverse.
func pressCombo(codes []int) error {
	kb, err := getKeyboard()
	if err != nil {
		return err
	}

	// Press all keys down with 40ms delay between each
	for i, code := range codes {
		if err := kb.KeyDown(code); err != nil {
			// Release any already-pressed keys on error
			for j := i - 1; j >= 0; j-- {
				kb.KeyUp(codes[j])
			}
			return fmt.Errorf("key down %d: %w", code, err)
		}
		if i < len(codes)-1 {
			time.Sleep(40 * time.Millisecond)
		}
	}

	// Small delay before releasing
	time.Sleep(40 * time.Millisecond)

	// Release all keys in reverse order
	for i := len(codes) - 1; i >= 0; i-- {
		if err := kb.KeyUp(codes[i]); err != nil {
			return fmt.Errorf("key up %d: %w", codes[i], err)
		}
	}

	return nil
}

// charKeyMapping holds the keycode and whether shift is needed for a character.
type charKeyMapping struct {
	code      int
	needShift bool
}

// charToKey maps runes to their Linux uinput keycode and shift state (US keyboard layout).
var charToKey = map[rune]charKeyMapping{
	// Letters (unshifted = lowercase)
	'a': {uinput.KeyA, false}, 'b': {uinput.KeyB, false}, 'c': {uinput.KeyC, false},
	'd': {uinput.KeyD, false}, 'e': {uinput.KeyE, false}, 'f': {uinput.KeyF, false},
	'g': {uinput.KeyG, false}, 'h': {uinput.KeyH, false}, 'i': {uinput.KeyI, false},
	'j': {uinput.KeyJ, false}, 'k': {uinput.KeyK, false}, 'l': {uinput.KeyL, false},
	'm': {uinput.KeyM, false}, 'n': {uinput.KeyN, false}, 'o': {uinput.KeyO, false},
	'p': {uinput.KeyP, false}, 'q': {uinput.KeyQ, false}, 'r': {uinput.KeyR, false},
	's': {uinput.KeyS, false}, 't': {uinput.KeyT, false}, 'u': {uinput.KeyU, false},
	'v': {uinput.KeyV, false}, 'w': {uinput.KeyW, false}, 'x': {uinput.KeyX, false},
	'y': {uinput.KeyY, false}, 'z': {uinput.KeyZ, false},

	// Letters (shifted = uppercase)
	'A': {uinput.KeyA, true}, 'B': {uinput.KeyB, true}, 'C': {uinput.KeyC, true},
	'D': {uinput.KeyD, true}, 'E': {uinput.KeyE, true}, 'F': {uinput.KeyF, true},
	'G': {uinput.KeyG, true}, 'H': {uinput.KeyH, true}, 'I': {uinput.KeyI, true},
	'J': {uinput.KeyJ, true}, 'K': {uinput.KeyK, true}, 'L': {uinput.KeyL, true},
	'M': {uinput.KeyM, true}, 'N': {uinput.KeyN, true}, 'O': {uinput.KeyO, true},
	'P': {uinput.KeyP, true}, 'Q': {uinput.KeyQ, true}, 'R': {uinput.KeyR, true},
	'S': {uinput.KeyS, true}, 'T': {uinput.KeyT, true}, 'U': {uinput.KeyU, true},
	'V': {uinput.KeyV, true}, 'W': {uinput.KeyW, true}, 'X': {uinput.KeyX, true},
	'Y': {uinput.KeyY, true}, 'Z': {uinput.KeyZ, true},

	// Numbers (unshifted)
	'1': {uinput.Key1, false}, '2': {uinput.Key2, false}, '3': {uinput.Key3, false},
	'4': {uinput.Key4, false}, '5': {uinput.Key5, false}, '6': {uinput.Key6, false},
	'7': {uinput.Key7, false}, '8': {uinput.Key8, false}, '9': {uinput.Key9, false},
	'0': {uinput.Key0, false},

	// Shifted number row symbols
	'!': {uinput.Key1, true}, '@': {uinput.Key2, true}, '#': {uinput.Key3, true},
	'$': {uinput.Key4, true}, '%': {uinput.Key5, true}, '^': {uinput.Key6, true},
	'&': {uinput.Key7, true}, '*': {uinput.Key8, true}, '(': {uinput.Key9, true},
	')': {uinput.Key0, true},

	// Punctuation (unshifted)
	'-':  {uinput.KeyMinus, false},
	'=':  {uinput.KeyEqual, false},
	'[':  {uinput.KeyLeftbrace, false},
	']':  {uinput.KeyRightbrace, false},
	';':  {uinput.KeySemicolon, false},
	'\'': {uinput.KeyApostrophe, false},
	'`':  {uinput.KeyGrave, false},
	'\\': {uinput.KeyBackslash, false},
	',':  {uinput.KeyComma, false},
	'.':  {uinput.KeyDot, false},
	'/':  {uinput.KeySlash, false},
	' ':  {uinput.KeySpace, false},

	// Shifted punctuation
	'_':  {uinput.KeyMinus, true},
	'+':  {uinput.KeyEqual, true},
	'{':  {uinput.KeyLeftbrace, true},
	'}':  {uinput.KeyRightbrace, true},
	':':  {uinput.KeySemicolon, true},
	'"':  {uinput.KeyApostrophe, true},
	'~':  {uinput.KeyGrave, true},
	'|':  {uinput.KeyBackslash, true},
	'<':  {uinput.KeyComma, true},
	'>':  {uinput.KeyDot, true},
	'?':  {uinput.KeySlash, true},

	// Special keys
	'\n': {uinput.KeyEnter, false},
	'\t': {uinput.KeyTab, false},
}

// TypeText types a string character by character using the virtual keyboard.
// Each character is mapped to the correct keycode with shift handling.
func TypeText(text string) error {
	kb, err := getKeyboard()
	if err != nil {
		return err
	}

	for _, ch := range text {
		mapping, ok := charToKey[ch]
		if !ok {
			return fmt.Errorf("unsupported character: %q (U+%04X)", ch, ch)
		}

		if mapping.needShift {
			if err := kb.KeyDown(uinput.KeyLeftshift); err != nil {
				return fmt.Errorf("shift down for %q: %w", ch, err)
			}
		}

		if err := kb.KeyDown(mapping.code); err != nil {
			if mapping.needShift {
				kb.KeyUp(uinput.KeyLeftshift)
			}
			return fmt.Errorf("key down for %q: %w", ch, err)
		}

		if err := kb.KeyUp(mapping.code); err != nil {
			if mapping.needShift {
				kb.KeyUp(uinput.KeyLeftshift)
			}
			return fmt.Errorf("key up for %q: %w", ch, err)
		}

		if mapping.needShift {
			if err := kb.KeyUp(uinput.KeyLeftshift); err != nil {
				return fmt.Errorf("shift up for %q: %w", ch, err)
			}
		}

		time.Sleep(50 * time.Millisecond)
	}

	return nil
}

// OSDNavigateTo opens the OSD and navigates to a named menu item using
// the conf_str database to calculate the correct position.
// coreName is the core identifier (e.g. "PC88", "SNES") — date suffixes
// are stripped and fuzzy matching is used against the conf_str DB.
// Supports items on sub-pages: navigates to the page entry, presses Right
// to enter the sub-page, then navigates to the target item within it.
func OSDNavigateTo(coreName, target string) error {
	db, err := GetConfStrDB()
	if err != nil {
		return fmt.Errorf("loading confstr db: %w", err)
	}

	// Read core CFG to determine which items are visible
	cfgData, _ := ReadCFG(CFGPath(NormalizeCoreName(coreName)))
	loc, err := FindOSDItemPosition(db, coreName, target, cfgData)
	if err != nil {
		return err
	}

	// Ensure OSD is closed first, then open it.
	// If OSD was already open (e.g. from a previous navigate), F12 would close it.
	// Escape closes the OSD if open, does nothing if closed.
	PressKey("Escape")
	time.Sleep(200 * time.Millisecond)

	// Open OSD
	if err := PressKey("F12"); err != nil {
		return fmt.Errorf("OSD open: %w", err)
	}
	time.Sleep(500 * time.Millisecond)

	if loc.OnSubPage {
		// Navigate to the sub-page entry from the bottom (more reliable
		// because runtime-hidden items at the top can't be predicted).
		// Up from top wraps to bottom. MiSTer adds an implicit "Exit"
		// entry below the last conf_str item, so we need 2x Up to reach
		// the last real item (first Up → Exit, second Up → last item).
		for i := 0; i < 2; i++ {
			if err := PressKey("up"); err != nil {
				return err
			}
			time.Sleep(100 * time.Millisecond)
		}
		for i := 0; i < loc.BottomOffset; i++ {
			if err := PressKey("up"); err != nil {
				return err
			}
			time.Sleep(100 * time.Millisecond)
		}
		// Enter the sub-page
		if err := PressKey("enter"); err != nil {
			return err
		}
		time.Sleep(300 * time.Millisecond)
		// Navigate down to target within sub-page
		for i := 0; i < loc.Position; i++ {
			if err := PressKey("down"); err != nil {
				return err
			}
			time.Sleep(100 * time.Millisecond)
		}
	} else if loc.UseBottomNav {
		// For items near the bottom (Reset, sub_pages), navigate from bottom
		// +1 Up for implicit "Exit" entry
		for i := 0; i < 2; i++ {
			if err := PressKey("up"); err != nil {
				return err
			}
			time.Sleep(100 * time.Millisecond)
		}
		for i := 0; i < loc.BottomOffset; i++ {
			if err := PressKey("up"); err != nil {
				return err
			}
			time.Sleep(100 * time.Millisecond)
		}
	} else {
		// Navigate down from top for regular items
		for i := 0; i < loc.Position; i++ {
			if err := PressKey("down"); err != nil {
				return err
			}
			time.Sleep(100 * time.Millisecond)
		}
	}

	return nil
}

// OSDResetByCore performs a soft reset via the OSD menu for a specific core.
// Uses the conf_str database to find the correct Reset position.
// This preserves mounted disk images unlike the hardware reset combo.
func OSDResetByCore(coreName string) error {
	if err := OSDNavigateTo(coreName, "Reset"); err != nil {
		return err
	}
	return PressKey("enter")
}

// OSDReset performs a soft reset via the OSD menu using a hardcoded
// navigation path (2x Up from bottom). Deprecated: use OSDResetByCore
// for conf_str-based navigation.
func OSDReset() error {
	if err := PressKey("F12"); err != nil {
		return fmt.Errorf("OSD open: %w", err)
	}
	time.Sleep(500 * time.Millisecond)
	if err := PressKey("up"); err != nil {
		return err
	}
	time.Sleep(200 * time.Millisecond)
	if err := PressKey("up"); err != nil {
		return err
	}
	time.Sleep(200 * time.Millisecond)
	return PressKey("enter")
}
