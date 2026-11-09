package app

import (
	"errors"
	"testing"

	"github.com/egoist/mygo/ui"
)

// The keys the settings keep play the actions: those they start with, and
// the ones the listener gives them in their place.
func TestTheKeysTheSettingsKeepRunTheActions(t *testing.T) {
	a := newTestApp()
	a.settings = defaultSettings()
	a.volume = 0.5
	tt := ui.NewTester(a.view, 1200, 800)
	tt.Frame()
	// Volume up is CmdOrCtrl+Shift+Up until the listener changes it.
	tt.Key(ui.Cmd|ui.Shift, ui.KeyUp)
	tt.Frame()
	if a.volume != 0.55 {
		t.Fatalf("CmdOrCtrl+Shift+Up made the volume %v, want 0.55", a.volume)
	}
	a.setBinding(kindInApp, "volumeUp", "CmdOrCtrl+Alt+Up")
	tt.Frame()
	tt.Key(ui.Cmd|ui.Shift, ui.KeyUp)
	tt.Frame()
	if a.volume != 0.55 {
		t.Errorf("the key the action no longer has changed the volume to %v", a.volume)
	}
	tt.Key(ui.Cmd|ui.Alt, ui.KeyUp)
	tt.Frame()
	if a.volume != 0.6 {
		t.Errorf("CmdOrCtrl+Alt+Up made the volume %v, want 0.6", a.volume)
	}
}

// Recording a binding gives the action the key pressed, refuses one another
// action has or one the system will not take from other apps, and takes the
// action's own away with Backspace.
func TestRecordingABindingTakesTheKeyPressed(t *testing.T) {
	a := newTestApp()
	a.settings = defaultSettings()
	press := func(kind keyKind, name string, mods ui.Modifiers, key ui.Key) {
		t.Helper()
		a.recordKey(kind, name, ui.InputEvent{Kind: ui.InputKeyDown, Key: key, Mods: mods})
	}

	a.recording = "inApp/next"
	press(kindInApp, "next", ui.Cmd, ui.KeyJ)
	if got := a.binding(kindInApp, "next"); got != "CmdOrCtrl+j" {
		t.Fatalf("next took %q, want CmdOrCtrl+j", got)
	}
	if a.recording != "" {
		t.Errorf("the recording went on after taking a key")
	}

	// Prev has CmdOrCtrl+Shift+Left, and Cmd+J is next's now.
	a.recording = "inApp/prev"
	press(kindInApp, "prev", ui.Cmd, ui.KeyJ)
	if got := a.binding(kindInApp, "prev"); got != "CmdOrCtrl+Shift+Left" {
		t.Errorf("prev took a key next has: %q", got)
	}
	if a.recordingNote == "" {
		t.Errorf("nothing said why the key was refused")
	}

	press(kindInApp, "prev", 0, ui.KeyBackspace)
	if got := a.binding(kindInApp, "prev"); got != "" {
		t.Errorf("Backspace left prev with %q", got)
	}

	// A global key needs a modifier: a bare letter would take that letter
	// from every other app.
	a.recording = "global/next"
	press(kindGlobal, "next", 0, ui.KeyJ)
	if got := a.binding(kindGlobal, "next"); got != "MediaNextTrack" {
		t.Errorf("a bare letter became a global key: %q", got)
	}
	press(kindGlobal, "next", ui.Cmd|ui.Alt, ui.KeyJ)
	if got := a.binding(kindGlobal, "next"); got != "CmdOrCtrl+Alt+j" {
		t.Errorf("next took %q as its global key, want CmdOrCtrl+Alt+j", got)
	}
}

// The system is given the keys the global bindings ask for, and given back
// the ones they no longer do, as it lets one app take a key.
func TestGlobalKeysAreClaimedAndGivenBack(t *testing.T) {
	a := newTestApp()
	a.settings = defaultSettings()
	state := &keySystem{}
	oldSysKeys, oldRegister, oldUnregister := sysKeys, globalRegister, globalUnregister
	sysKeys = func(*app) bool { return true }
	globalRegister = state.register
	globalUnregister = state.unregister
	defer func() {
		sysKeys, globalRegister, globalUnregister = oldSysKeys, oldRegister, oldUnregister
	}()

	a.registerGlobals()
	for _, h := range hotkeys {
		if !state.holds(h.global) {
			t.Errorf("%s did not claim %s", h.label, h.global)
		}
	}

	// An action that loses its key gives the system's back, and keeps it no
	// longer.
	state.held, state.released = nil, nil
	a.setBinding(kindGlobal, "next", "")
	a.registerGlobals()
	if state.holds("MediaNextTrack") {
		t.Errorf("next kept MediaNextTrack after losing it")
	}
	if !contains(state.released, "MediaNextTrack") {
		t.Errorf("next did not give MediaNextTrack back: %v", state.released)
	}
	if len(state.held) != len(hotkeys)-1 {
		t.Errorf("claimed %d keys, want %d: %v", len(state.held), len(hotkeys)-1, state.held)
	}

	// A key another app holds is not claimed, and the settings say so.
	state.refuse = "CmdOrCtrl+Alt+L"
	a.registerGlobals()
	if state.holds("CmdOrCtrl+Alt+L") {
		t.Errorf("claimed a key another app holds")
	}
	if a.hotkeyErrs["deskLyrics"] == "" {
		t.Errorf("the settings say nothing of the key desk lyrics could not take")
	}
}

// errKeyHeld is what the system says of a key another app holds.
var errKeyHeld = errors.New("the key is another app's")

// keySystem is the system's shortcuts as a test keeps them: the keys it
// holds for the app, those it gave back, and the one it refuses.
type keySystem struct {
	held, released []string
	refuse         string
}

func (s *keySystem) register(acc string, fn func()) error {
	if acc == s.refuse {
		return errKeyHeld
	}
	s.held = append(s.held, acc)
	return nil
}

func (s *keySystem) unregister(acc string) {
	s.released = append(s.released, acc)
	for i, held := range s.held {
		if held == acc {
			s.held = append(s.held[:i], s.held[i+1:]...)
			return
		}
	}
}

// holds reports whether the system holds acc for the app.
func (s *keySystem) holds(acc string) bool {
	return contains(s.held, acc)
}

// The keys the settings start with are the actions' own, and one the settings
// hold as empty or as something the set cannot take keeps the action's own.
func TestTheShortcutSettingsFillInTheDefaults(t *testing.T) {
	filled := shortcutKeys{}.filled()
	if len(filled.InApp) != len(hotkeys) || len(filled.Global) != len(hotkeys) {
		t.Fatalf("filled %d in-app and %d global keys, want %d each",
			len(filled.InApp), len(filled.Global), len(hotkeys))
	}
	if filled.InApp["playPause"] != "Space" || filled.Global["playPause"] != "MediaPlayPause" {
		t.Errorf("play/pause has %q and %q", filled.InApp["playPause"], filled.Global["playPause"])
	}

	kept := shortcutKeys{
		InApp:  map[string]string{"playPause": "", "next": "Bogus+Q"},
		Global: map[string]string{"next": "J", "prev": ""},
	}.filled()
	if kept.InApp["playPause"] != "" {
		t.Errorf("a key the listener took away came back as %q", kept.InApp["playPause"])
	}
	if kept.InApp["next"] != "CmdOrCtrl+Shift+Right" {
		t.Errorf("a key the app cannot take was kept: %q", kept.InApp["next"])
	}
	if kept.Global["next"] != "MediaNextTrack" {
		t.Errorf("a global key without a modifier was kept: %q", kept.Global["next"])
	}
	if kept.Global["prev"] != "" {
		t.Errorf("a global key the listener took away came back as %q", kept.Global["prev"])
	}
}

// Accelerators go back and forth between the keys the window binds and the
// names the settings keep; the ones only the system sees name no key.
func TestAcceleratorsGoBackAndForth(t *testing.T) {
	for _, c := range []struct {
		mods ui.Modifiers
		key  ui.Key
		acc  string
	}{
		{0, ui.KeySpace, "space"},
		{ui.Cmd, ui.KeyL, "CmdOrCtrl+l"},
		{ui.Cmd | ui.Alt, ui.KeyUp, "CmdOrCtrl+Alt+up"},
		{ui.Cmd | ui.Shift, ui.KeyEqual, "CmdOrCtrl+Shift+="},
		{ui.Alt, ui.KeyMinus, "Alt+-"},
		{ui.Shift, ui.KeyF5, "Shift+f5"},
	} {
		acc, ok := acceleratorFor(c.mods, c.key)
		if !ok || acc != c.acc {
			t.Errorf("%v and %v gave %q, want %q", c.mods, c.key, acc, c.acc)
			continue
		}
		mods, key, ok := parseAccelerator(acc)
		if !ok || mods != c.mods || key != c.key {
			t.Errorf("%q went back to %v and %v, want %v and %v", acc, mods, key, c.mods, c.key)
		}
	}
	if _, _, ok := parseAccelerator(""); ok {
		t.Errorf("an empty accelerator parsed")
	}
	if _, _, ok := parseAccelerator("MediaPlayPause"); ok {
		t.Errorf("a media key parsed as a key of the window")
	}
	if _, ok := acceleratorFor(ui.Shift, ui.KeyUnknown); ok {
		t.Errorf("a modifier on its own made an accelerator")
	}
}

// contains reports whether a holds s.
func contains(a []string, s string) bool {
	for _, v := range a {
		if v == s {
			return true
		}
	}
	return false
}

// The settings record a shortcut: clicking the key an action has focuses it,
// and the key pressed next becomes the action's, shown as the platform names
// it.
func TestTheSettingsRecordAShortcut(t *testing.T) {
	isolateUserData(t)
	a := newTestApp()
	a.settings = defaultSettings()
	a.settingsPage = "keys"
	tt := ui.NewTester(a.settingsView, 780, 640)
	tt.Frame()
	if !tt.HasText("Space") {
		t.Fatalf("the page shows no key of play/pause")
	}
	if err := tt.Click("Space"); err != nil {
		t.Fatalf("clicking the key of play/pause: %v", err)
	}
	tt.Frame()
	if !tt.HasText("按下按键…") {
		t.Errorf("the field does not say it records")
	}
	tt.Key(ui.Cmd|ui.Alt, ui.KeyK)
	tt.Frame()
	if got := a.binding(kindInApp, "playPause"); got != "CmdOrCtrl+Alt+k" {
		t.Fatalf("play/pause took %q, want CmdOrCtrl+Alt+k", got)
	}
	if !tt.HasText(hotkeyLabel("CmdOrCtrl+Alt+k")) {
		t.Errorf("the settings do not show the key play/pause has")
	}
	if a.recording != "" {
		t.Errorf("the settings record a key still")
	}
}
