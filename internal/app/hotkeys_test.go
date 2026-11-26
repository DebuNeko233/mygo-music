/*
验证快捷键录制、恢复、全局注册与释放，并隔离失效的 Wayland 注册结果。
*/

package app

import (
	"errors"
	"testing"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"

	"netease-music-mygo/internal/ui/view/shortcuts"
)

// The keys the settings keep play the actions: those they start with, and
// the ones the listener gives them in their place.
func TestTheKeysTheSettingsKeepRunTheActions(t *testing.T) {
	a := newTestApp()
	a.settings = defaultSettings()
	a.playbackState.Volume = 0.5
	tt := ui.NewTester(a.view, 1200, 800)
	tt.Frame()
	// Volume up is CmdOrCtrl+Shift+Up until the listener changes it.
	tt.Key(ui.Cmd|ui.Shift, ui.KeyUp)
	tt.Frame()
	if a.playbackState.Volume != 0.55 {
		t.Fatalf("CmdOrCtrl+Shift+Up made the volume %v, want 0.55", a.playbackState.Volume)
	}
	a.setBinding(shortcuts.InApp, "volumeUp", "CmdOrCtrl+Alt+Up")
	tt.Frame()
	tt.Key(ui.Cmd|ui.Shift, ui.KeyUp)
	tt.Frame()
	if a.playbackState.Volume != 0.55 {
		t.Errorf("the key the action no longer has changed the volume to %v", a.playbackState.Volume)
	}
	tt.Key(ui.Cmd|ui.Alt, ui.KeyUp)
	tt.Frame()
	if a.playbackState.Volume != 0.6 {
		t.Errorf("CmdOrCtrl+Alt+Up made the volume %v, want 0.6", a.playbackState.Volume)
	}
}

// Recording a binding gives the action the key pressed, refuses one another
// action has or one the system will not take from other apps, and takes the
// action's own away with Backspace.
func TestRecordingABindingTakesTheKeyPressed(t *testing.T) {
	a := newTestApp()
	a.settings = defaultSettings()
	press := func(kind string, name string, mods ui.Modifiers, key ui.Key) {
		t.Helper()
		a.views.settings.RecordKey(string(kind), name, ui.InputEvent{Kind: ui.InputKeyDown, Key: key, Mods: mods})
	}

	a.Recording = "inApp/next"
	press(shortcuts.InApp, "next", ui.Cmd, ui.KeyJ)
	if got := shortcuts.Binding(a.settings.Shortcuts, shortcuts.InApp, "next"); got != "CmdOrCtrl+j" {
		t.Fatalf("next took %q, want CmdOrCtrl+j", got)
	}
	if a.Recording != "" {
		t.Errorf("the recording went on after taking a key")
	}

	// Prev has CmdOrCtrl+Shift+Left, and Cmd+J is next's now.
	a.Recording = "inApp/prev"
	press(shortcuts.InApp, "prev", ui.Cmd, ui.KeyJ)
	if got := shortcuts.Binding(a.settings.Shortcuts, shortcuts.InApp, "prev"); got != "CmdOrCtrl+Shift+Left" {
		t.Errorf("prev took a key next has: %q", got)
	}
	if a.RecordingNote == "" {
		t.Errorf("nothing said why the key was refused")
	}

	press(shortcuts.InApp, "prev", 0, ui.KeyBackspace)
	if got := shortcuts.Binding(a.settings.Shortcuts, shortcuts.InApp, "prev"); got != "" {
		t.Errorf("Backspace left prev with %q", got)
	}

	// A global key needs a modifier: a bare letter would take that letter
	// from every other app.
	a.Recording = "global/next"
	press(shortcuts.Global, "next", 0, ui.KeyJ)
	if got := shortcuts.Binding(a.settings.Shortcuts, shortcuts.Global, "next"); got != "MediaNextTrack" {
		t.Errorf("a bare letter became a global key: %q", got)
	}
	press(shortcuts.Global, "next", ui.Cmd|ui.Alt, ui.KeyJ)
	if got := shortcuts.Binding(a.settings.Shortcuts, shortcuts.Global, "next"); got != "CmdOrCtrl+Alt+j" {
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
	for _, h := range shortcuts.Actions() {
		if !state.holds(h.Global) {
			t.Errorf("%s did not claim %s", h.Label, h.Global)
		}
	}

	// An action that loses its key gives the system's back, and keeps it no
	// longer.
	state.held, state.released = nil, nil
	a.setBinding(shortcuts.Global, "next", "")
	a.registerGlobals()
	if state.holds("MediaNextTrack") {
		t.Errorf("next kept MediaNextTrack after losing it")
	}
	if !contains(state.released, "MediaNextTrack") {
		t.Errorf("next did not give MediaNextTrack back: %v", state.released)
	}
	if len(state.held) != len(shortcuts.Actions())-1 {
		t.Errorf("claimed %d keys, want %d: %v", len(state.held), len(shortcuts.Actions())-1, state.held)
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

func (s *keySystem) register(acc string, opts mygo.GlobalShortcutOptions, fn func()) error {
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
	a.preferences.Section = "keys"
	tt := ui.NewTester(a.views.settings.Build, 780, 640)
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
	if got := shortcuts.Binding(a.settings.Shortcuts, shortcuts.InApp, "playPause"); got != "CmdOrCtrl+Alt+k" {
		t.Fatalf("play/pause took %q, want CmdOrCtrl+Alt+k", got)
	}
	if !tt.HasText(shortcuts.Label("CmdOrCtrl+Alt+k")) {
		t.Errorf("the settings do not show the key play/pause has")
	}
	if a.Recording != "" {
		t.Errorf("the settings record a key still")
	}
}

// An action that took another key gets its own back from the button beside
// it, and the settings stop offering that button once it has.
func TestTheDefaultKeyComesBack(t *testing.T) {
	isolateUserData(t)
	a := newTestApp()
	a.settings = defaultSettings()
	a.preferences.Section = "keys"
	a.setBinding(shortcuts.InApp, "volumeUp", "CmdOrCtrl+Alt+Up")
	tt := ui.NewTester(a.views.settings.Build, 780, 640)
	tt.Frame()
	if !shortcuts.Changed(a.settings.Shortcuts, shortcuts.InApp, "volumeUp") {
		t.Errorf("the settings do not know the key was changed")
	}
	if err := tt.Click("恢复默认"); err != nil {
		t.Fatalf("clicking the button giving the default back: %v", err)
	}
	tt.Frame()
	if got := shortcuts.Binding(a.settings.Shortcuts, shortcuts.InApp, "volumeUp"); got != "CmdOrCtrl+Shift+Up" {
		t.Fatalf("volumeUp has %q after the default came back, want CmdOrCtrl+Shift+Up", got)
	}
	if shortcuts.Changed(a.settings.Shortcuts, shortcuts.InApp, "volumeUp") {
		t.Errorf("the settings still think the key was changed")
	}
	if err := tt.Click("恢复默认"); err == nil {
		t.Errorf("the settings still offer a default to give back")
	}
}

// A Wayland portal that binds no key explains the failure instead of
// blaming another application, and a later successful answer clears it.
func TestWaylandShortcutBindingResultIsVisible(t *testing.T) {
	a := newTestApp()
	a.settings = defaultSettings()
	posts := testPosts(a)
	oldSysKeys, oldRegister, oldUnregister := sysKeys, globalRegister, globalUnregister
	oldHyprland := hyprlandDesktop
	t.Cleanup(func() {
		sysKeys, globalRegister, globalUnregister = oldSysKeys, oldRegister, oldUnregister
		hyprlandDesktop = oldHyprland
	})
	hyprlandDesktop = func() bool { return false }
	sysKeys = func(*app) bool { return true }
	globalUnregister = func(string) {}
	var bound mygo.GlobalShortcutOptions
	globalRegister = func(acc string, opts mygo.GlobalShortcutOptions, fn func()) error {
		if acc == "CmdOrCtrl+Alt+L" {
			bound = opts
		}
		return nil
	}
	a.registerGlobals()
	if bound.Description == "" || bound.OnKeys == nil {
		t.Fatalf("the portal was not told what the shortcut does: %+v", bound)
	}
	bound.OnKeys("")
	wait(t, posts, "the portal's unbound shortcut")()
	if got := a.hotkeyErrs["deskLyrics"]; got == "" || got == "被其他程序占用" {
		t.Fatalf("unbound portal shortcut reported %q", got)
	}
	bound.OnKeys("Ctrl+Alt+L")
	wait(t, posts, "the portal's bound shortcut")()
	if got := a.hotkeyErrs["deskLyrics"]; got != "" {
		t.Fatalf("a bound shortcut kept its error %q", got)
	}
	hyprlandDesktop = func() bool { return true }
	bound.OnKeys("")
	wait(t, posts, "Hyprland's accepted shortcut")()
	if got := a.hotkeyErrs["deskLyrics"]; got != "" {
		t.Fatalf("Hyprland's empty trigger reported an error %q", got)
	}
}

// An old portal answer cannot overwrite the current binding after re-registration,
// unregistering or exit, even when it was queued before that lifetime ended.
func TestGlobalShortcutResultsIgnoreRetiredRegistration(t *testing.T) {
	for _, retire := range []string{"rebind", "unregister", "quit"} {
		t.Run(retire, func(t *testing.T) {
			a := newTestApp()
			posts := testPosts(a)
			oldSysKeys, oldRegister, oldUnregister, oldHyprland := sysKeys, globalRegister, globalUnregister, hyprlandDesktop
			t.Cleanup(func() {
				sysKeys, globalRegister, globalUnregister, hyprlandDesktop = oldSysKeys, oldRegister, oldUnregister, oldHyprland
			})
			sysKeys = func(*app) bool { return true }
			hyprlandDesktop = func() bool { return false }
			globalUnregister = func(string) {}
			var bound mygo.GlobalShortcutOptions
			globalRegister = func(acc string, opts mygo.GlobalShortcutOptions, fn func()) error {
				if acc == "CmdOrCtrl+Alt+L" {
					bound = opts
				}
				return nil
			}
			a.registerGlobals()
			old := bound
			old.OnKeys("")
			switch retire {
			case "rebind":
				a.registerGlobals()
			case "unregister":
				a.unregisterGlobals()
			case "quit":
				a.quitting = true
			}
			a.hotkeyErrs["deskLyrics"] = "current result"
			wait(t, posts, "queued stale binding")()
			old.OnKeys("Ctrl+Alt+L")
			wait(t, posts, "late stale binding")()
			if a.hotkeyErrs["deskLyrics"] != "current result" {
				t.Fatal("retired portal result changed the current binding")
			}
			if retire == "rebind" {
				bound.OnKeys("Ctrl+Alt+L")
				wait(t, posts, "current binding")()
				if a.hotkeyErrs["deskLyrics"] != "" {
					t.Fatal("current portal result was discarded")
				}
			}
		})
	}
}
