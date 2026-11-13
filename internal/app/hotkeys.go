package app

import (
	"math"
	"runtime"
	"strings"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"
)

// keyKind is one of the two sets of shortcuts: the ones that work while the
// app's window has the focus, and the ones that work wherever it is.
type keyKind string

const (
	kindInApp  keyKind = "inApp"
	kindGlobal keyKind = "global"
)

// hotkey is an action the listener can give a key to, in the app and out of it.
type hotkey struct {
	name  string
	label string
	// inApp and global are the keys the action has until the listener gives
	// it others. A global key needs a modifier, or a media or function key,
	// as it is taken from every other app.
	inApp, global string
	run           func(*app)
}

// hotkeys are the actions that take a key, in the order the settings show
// them. The playback keys are the systems' own media keys, which every
// system sends to the app playing.
var hotkeys = []hotkey{
	{name: "playPause", label: "播放/暂停", inApp: "Space", global: "MediaPlayPause",
		run: func(a *app) { a.pl.TogglePlay() }},
	{name: "prev", label: "上一首", inApp: "CmdOrCtrl+Shift+Left", global: "MediaPreviousTrack",
		run: func(a *app) { a.pl.Prev() }},
	{name: "next", label: "下一首", inApp: "CmdOrCtrl+Shift+Right", global: "MediaNextTrack",
		run: func(a *app) { a.pl.Next() }},
	{name: "deskLyrics", label: "桌面歌词", inApp: "CmdOrCtrl+Shift+L", global: "CmdOrCtrl+Alt+L",
		run: func(a *app) { a.toggleDeskLyrics() }},
	{name: "lyricBigger", label: "歌词字号变大", inApp: "CmdOrCtrl+Shift+=", global: "CmdOrCtrl+Alt+Up",
		run: func(a *app) { a.stepLyricSize(2) }},
	{name: "lyricSmaller", label: "歌词字号变小", inApp: "CmdOrCtrl+Shift+-", global: "CmdOrCtrl+Alt+Down",
		run: func(a *app) { a.stepLyricSize(-2) }},
	{name: "volumeUp", label: "音量增大", inApp: "CmdOrCtrl+Shift+Up", global: "CmdOrCtrl+Alt+Right",
		run: func(a *app) { a.stepVolumeBy(0.05) }},
	{name: "volumeDown", label: "音量减小", inApp: "CmdOrCtrl+Shift+Down", global: "CmdOrCtrl+Alt+Left",
		run: func(a *app) { a.stepVolumeBy(-0.05) }},
}

// stepVolumeBy turns the volume up or down by d, as the shortcut keys do.
func (a *app) stepVolumeBy(d float64) {
	a.setVolume(math.Round(min(1, max(0, a.volume+d))*100) / 100)
}

// stepLyricSize makes the lines of the lyrics window d points larger or
// smaller, within the sizes it takes.
func (a *app) stepLyricSize(d float64) {
	size := min(lyricSizeMax, max(lyricSizeMin, a.settings.LyricSize+d))
	if size == a.settings.LyricSize {
		return
	}
	a.settings.LyricSize = size
	a.settings.saveSoon()
	if a.lyrics != nil {
		a.lyrics.apply()
	}
	a.redraw()
}

// binding is the accelerator the action has in one of the sets, empty when
// the listener gave it none.
func (a *app) binding(kind keyKind, name string) string {
	keys := a.settings.Shortcuts.InApp
	if kind == kindGlobal {
		keys = a.settings.Shortcuts.Global
	}
	if acc, ok := keys[name]; ok {
		return acc
	}
	for _, h := range hotkeys {
		if h.name == name {
			if kind == kindGlobal {
				return h.global
			}
			return h.inApp
		}
	}
	return ""
}

// setBinding gives the action acc as its key, empty for none, and keeps it.
func (a *app) setBinding(kind keyKind, name, acc string) {
	keys := &a.settings.Shortcuts.InApp
	if kind == kindGlobal {
		keys = &a.settings.Shortcuts.Global
	}
	if *keys == nil {
		*keys = map[string]string{}
	}
	(*keys)[name] = acc
	a.settings.saveSoon()
	if kind == kindGlobal {
		a.registerGlobals()
	}
	a.redraw()
}

// changedBinding reports whether the action has a key other than the one it
// comes with, as the settings offer to give the default back.
func (a *app) changedBinding(kind keyKind, name string) bool {
	for _, h := range hotkeys {
		if h.name != name {
			continue
		}
		own := h.inApp
		if kind == kindGlobal {
			own = h.global
		}
		return canonicalAccelerator(a.binding(kind, name)) != canonicalAccelerator(own)
	}
	return false
}

// restoreBinding gives the action the key it comes with, forgetting the one
// the settings kept for it.
func (a *app) restoreBinding(kind keyKind, name string) {
	keys := &a.settings.Shortcuts.InApp
	if kind == kindGlobal {
		keys = &a.settings.Shortcuts.Global
	}
	delete(*keys, name)
	a.settings.saveSoon()
	if kind == kindGlobal {
		a.registerGlobals()
	}
	a.recording, a.recordingNote = "", ""
	a.redraw()
}

// taken reports whether another action of the set already has acc, and whose.
func (a *app) taken(kind keyKind, name, acc string) (string, bool) {
	canonical := canonicalAccelerator(acc)
	for _, h := range hotkeys {
		if h.name == name {
			continue
		}
		other := a.binding(kind, h.name)
		if other != "" && canonicalAccelerator(other) == canonical {
			return h.label, true
		}
	}
	return "", false
}

// globalRegister and globalUnregister are the system's shortcuts: the tests
// put their own in their place, as there is no system to claim keys from.
var (
	globalRegister   = func(acc string, fn func()) error { return mygo.GlobalShortcut.Register(acc, fn) }
	globalUnregister = func(acc string) { mygo.GlobalShortcut.Unregister(acc) }
)

// sysKeys says whether the app has a system to claim keys from: it needs a
// window, and the tests, which have none, put their own answer in its place.
var sysKeys = func(a *app) bool { return a.win != nil }

// registerGlobals claims the keys the global bindings ask for, and gives back
// the ones they no longer do: the system lets one app take a key, so what a
// key is given to has to be claimed again whenever it changes.
func (a *app) registerGlobals() {
	if !sysKeys(a) {
		return
	}
	for _, acc := range a.globalKeys {
		globalUnregister(acc)
	}
	a.globalKeys = a.globalKeys[:0]
	if a.hotkeyErrs == nil {
		a.hotkeyErrs = map[string]string{}
	}
	clear(a.hotkeyErrs)
	for _, h := range hotkeys {
		acc := a.binding(kindGlobal, h.name)
		if acc == "" {
			continue
		}
		if err := globalRegister(acc, func() { a.runHotkey(h) }); err != nil {
			// The system keeps the key for another app, or another action of
			// this one asked for it first: the settings say so.
			a.hotkeyErrs[h.name] = "被其他程序占用"
			continue
		}
		a.globalKeys = append(a.globalKeys, acc)
	}
}

// unregisterGlobals gives the system back every key the app claimed.
func (a *app) unregisterGlobals() {
	for _, acc := range a.globalKeys {
		globalUnregister(acc)
	}
	a.globalKeys = nil
}

// runHotkey runs the action a key was given to, on the window's thread.
func (a *app) runHotkey(h hotkey) {
	a.post(func() {
		h.run(a)
		a.redraw()
	})
}

// bindInApp gives the keys of the in-app bindings to the window: root takes
// them before a focused button does, and c where nothing has the focus at
// all, as once Now Playing covers the button that had it.
func (a *app) bindInApp(c *ui.Context, root ui.Element) {
	// A key being recorded is not an action.
	if a.recording != "" {
		return
	}
	for _, h := range hotkeys {
		mods, key, ok := parseAccelerator(a.binding(kindInApp, h.name))
		if !ok {
			continue
		}
		if key == ui.KeySpace && c.Focused(a.searchBox) {
			// Space types a space in the search field.
			continue
		}
		run := func() {
			h.run(a)
			a.redraw()
		}
		root.OnShortcut(mods, key, run)
		c.OnShortcut(mods, key, run)
	}
}

// acceleratorKeys are the keys an accelerator names, other than the letters
// and the digits it spells as themselves.
var acceleratorKeys = map[string]ui.Key{
	"space":     ui.KeySpace,
	"tab":       ui.KeyTab,
	"enter":     ui.KeyEnter,
	"return":    ui.KeyEnter,
	"backspace": ui.KeyBackspace,
	"delete":    ui.KeyDelete,
	"del":       ui.KeyDelete,
	"insert":    ui.KeyInsert,
	"ins":       ui.KeyInsert,
	"escape":    ui.KeyEscape,
	"esc":       ui.KeyEscape,
	"left":      ui.KeyLeft,
	"right":     ui.KeyRight,
	"up":        ui.KeyUp,
	"down":      ui.KeyDown,
	"home":      ui.KeyHome,
	"end":       ui.KeyEnd,
	"pageup":    ui.KeyPageUp,
	"pagedown":  ui.KeyPageDown,
	"f1":        ui.KeyF1,
	"f2":        ui.KeyF2,
	"f3":        ui.KeyF3,
	"f4":        ui.KeyF4,
	"f5":        ui.KeyF5,
	"f6":        ui.KeyF6,
	"f7":        ui.KeyF7,
	"f8":        ui.KeyF8,
	"f9":        ui.KeyF9,
	"f10":       ui.KeyF10,
	"f11":       ui.KeyF11,
	"f12":       ui.KeyF12,
	"minus":     ui.KeyMinus,
	"-":         ui.KeyMinus,
	"equal":     ui.KeyEqual,
	"=":         ui.KeyEqual,
	"plus":      ui.KeyEqual,
	"+":         ui.KeyEqual,
	",":         ui.KeyComma,
	".":         ui.KeyPeriod,
	"/":         ui.KeySlash,
	";":         ui.KeySemicolon,
	"'":         ui.KeyQuote,
	"[":         ui.KeyBracketLeft,
	"]":         ui.KeyBracketRight,
	"\\":        ui.KeyBackslash,
	"`":         ui.KeyBackquote,
}

// keyNames are the names the app writes keys by, acceleratorKeys' names
// without its aliases.
var keyNames = map[ui.Key]string{
	ui.KeySpace: "space", ui.KeyTab: "tab", ui.KeyEnter: "enter",
	ui.KeyBackspace: "backspace", ui.KeyDelete: "delete", ui.KeyInsert: "insert",
	ui.KeyEscape: "escape", ui.KeyLeft: "left", ui.KeyRight: "right",
	ui.KeyUp: "up", ui.KeyDown: "down", ui.KeyHome: "home", ui.KeyEnd: "end",
	ui.KeyPageUp: "pageup", ui.KeyPageDown: "pagedown",
	ui.KeyF1: "f1", ui.KeyF2: "f2", ui.KeyF3: "f3", ui.KeyF4: "f4",
	ui.KeyF5: "f5", ui.KeyF6: "f6", ui.KeyF7: "f7", ui.KeyF8: "f8",
	ui.KeyF9: "f9", ui.KeyF10: "f10", ui.KeyF11: "f11", ui.KeyF12: "f12",
	ui.KeyMinus: "-", ui.KeyEqual: "=", ui.KeyComma: ",", ui.KeyPeriod: ".",
	ui.KeySlash: "/", ui.KeySemicolon: ";", ui.KeyQuote: "'",
	ui.KeyBracketLeft: "[", ui.KeyBracketRight: "]", ui.KeyBackslash: "\\",
	ui.KeyBackquote: "`",
}

// letterKeys and digitKeys are the keys that name themselves.
var letterKeys = []ui.Key{ui.KeyA, ui.KeyB, ui.KeyC, ui.KeyD, ui.KeyE,
	ui.KeyF, ui.KeyG, ui.KeyH, ui.KeyI, ui.KeyJ, ui.KeyK,
	ui.KeyL, ui.KeyM, ui.KeyN, ui.KeyO, ui.KeyP, ui.KeyQ,
	ui.KeyR, ui.KeyS, ui.KeyT, ui.KeyU, ui.KeyV, ui.KeyW,
	ui.KeyX, ui.KeyY, ui.KeyZ}

var digitKeys = []ui.Key{ui.Key0, ui.Key1, ui.Key2, ui.Key3, ui.Key4,
	ui.Key5, ui.Key6, ui.Key7, ui.Key8, ui.Key9}

// keyName is the name an accelerator gives the key, empty for one it has none.
func keyName(key ui.Key) string {
	for i, k := range letterKeys {
		if k == key {
			return string(rune('a' + i))
		}
	}
	for i, k := range digitKeys {
		if k == key {
			return string(rune('0' + i))
		}
	}
	return keyNames[key]
}

// keyNamed is the key an accelerator's name gives, false for one it has none.
func keyNamed(name string) (ui.Key, bool) {
	if len(name) == 1 {
		c := name[0]
		if c >= 'a' && c <= 'z' {
			return letterKeys[c-'a'], true
		}
		if c >= 'A' && c <= 'Z' {
			return letterKeys[c-'A'], true
		}
		if c >= '0' && c <= '9' {
			return digitKeys[c-'0'], true
		}
	}
	key, ok := acceleratorKeys[strings.ToLower(name)]
	return key, ok
}

// splitAccelerator splits an accelerator into its parts, the modifiers and
// then the key, allowing "+" itself as the key, as in "CmdOrCtrl++".
func splitAccelerator(acc string) []string {
	parts := strings.Split(acc, "+")
	switch {
	case strings.HasSuffix(acc, "++"):
		parts = append(parts[:len(parts)-2], "+")
	case acc == "+":
		parts = []string{"+"}
	case len(parts) > 1 && parts[len(parts)-1] == "":
		// "CmdOrCtrl+" names no key.
		parts = parts[:len(parts)-1]
	}
	if len(parts) == 0 || parts[len(parts)-1] == "" {
		return nil
	}
	return parts
}

// acceleratorModifier is the modifier a part names, 0 for none.
func acceleratorModifier(part string) ui.Modifiers {
	switch strings.ToLower(part) {
	case "commandorcontrol", "cmdorctrl":
		return ui.Cmd
	case "command", "cmd":
		return ui.Cmd
	case "super", "meta", "win":
		return ui.Super
	case "control", "ctrl":
		return ui.Ctrl
	case "alt", "option", "altgr":
		return ui.Alt
	case "shift":
		return ui.Shift
	}
	return 0
}

// parseAccelerator turns an accelerator into the modifiers and the key the
// window binds them with, false for one the window cannot take, as a media
// key, which only the system sees.
func parseAccelerator(acc string) (ui.Modifiers, ui.Key, bool) {
	parts := splitAccelerator(acc)
	if len(parts) == 0 {
		return 0, 0, false
	}
	key, ok := keyNamed(parts[len(parts)-1])
	if !ok {
		return 0, 0, false
	}
	var mods ui.Modifiers
	for _, part := range parts[:len(parts)-1] {
		m := acceleratorModifier(part)
		if m == 0 {
			return 0, 0, false
		}
		mods |= m
	}
	return mods, key, true
}

// acceleratorFor is the accelerator of a key and the modifiers held with it,
// false for a modifier on its own, which is no key yet.
func acceleratorFor(mods ui.Modifiers, key ui.Key) (string, bool) {
	name := keyName(key)
	if name == "" {
		return "", false
	}
	var parts []string
	if mods&ui.Cmd != 0 {
		parts = append(parts, "CmdOrCtrl")
	}
	if mods&ui.Super != 0 && ui.Super != ui.Cmd {
		parts = append(parts, "Super")
	}
	if mods&ui.Ctrl != 0 && ui.Ctrl != ui.Cmd {
		parts = append(parts, "Ctrl")
	}
	if mods&ui.Alt != 0 {
		parts = append(parts, "Alt")
	}
	if mods&ui.Shift != 0 {
		parts = append(parts, "Shift")
	}
	return strings.Join(append(parts, name), "+"), true
}

// canonicalAccelerator is the accelerator as the app compares them: its
// modifiers in the app's order, and its key in lower case.
func canonicalAccelerator(acc string) string {
	parts := splitAccelerator(acc)
	if len(parts) == 0 {
		return ""
	}
	var mods []string
	for _, part := range parts[:len(parts)-1] {
		name := strings.ToLower(part)
		switch name {
		case "command", "cmd", "commandorcontrol", "cmdorctrl":
			name = "cmdorctrl"
		case "meta", "win":
			name = "super"
		case "control":
			name = "ctrl"
		case "option":
			name = "alt"
		}
		mods = append(mods, name)
	}
	return strings.Join(append(mods, strings.ToLower(parts[len(parts)-1])), "+")
}

// validGlobal reports whether a key can be claimed from the whole system: one
// with a modifier, or a media or function key, but not a bare letter, which
// would take that key from every other app.
func validGlobal(acc string) bool {
	parts := splitAccelerator(acc)
	if len(parts) == 0 {
		return false
	}
	if len(parts) > 1 {
		return true
	}
	name := strings.ToLower(parts[0])
	switch name {
	case "volumeup", "volumedown", "volumemute", "medianexttrack",
		"mediaprevioustrack", "mediastop", "mediaplaypause", "printscreen":
		return true
	}
	if len(name) >= 2 && name[0] == 'f' {
		if key, ok := keyNamed(name); ok {
			for _, f := range []ui.Key{ui.KeyF1, ui.KeyF2, ui.KeyF3, ui.KeyF4, ui.KeyF5, ui.KeyF6,
				ui.KeyF7, ui.KeyF8, ui.KeyF9, ui.KeyF10, ui.KeyF11, ui.KeyF12} {
				if key == f {
					return true
				}
			}
		}
	}
	return false
}

// hotkeyLabel is the accelerator as the settings show it: the symbols of the
// platform's shortcuts on macOS, its names elsewhere, and 未绑定 for none.
func hotkeyLabel(acc string) string {
	if acc == "" {
		return "未绑定"
	}
	parts := splitAccelerator(acc)
	if len(parts) == 0 {
		return acc
	}
	mac := runtime.GOOS == "darwin"
	var b strings.Builder
	for _, part := range parts[:len(parts)-1] {
		switch strings.ToLower(part) {
		case "commandorcontrol", "cmdorctrl", "command", "cmd":
			if mac {
				b.WriteString("⌘")
			} else {
				b.WriteString("Ctrl+")
			}
		case "super", "meta", "win":
			if mac {
				b.WriteString("⌘")
			} else {
				b.WriteString("Win+")
			}
		case "control", "ctrl":
			if mac {
				b.WriteString("⌃")
			} else {
				b.WriteString("Ctrl+")
			}
		case "alt", "option", "altgr":
			if mac {
				b.WriteString("⌥")
			} else {
				b.WriteString("Alt+")
			}
		case "shift":
			if mac {
				b.WriteString("⇧")
			} else {
				b.WriteString("Shift+")
			}
		default:
			b.WriteString(part + "+")
		}
	}
	b.WriteString(keyLabel(parts[len(parts)-1]))
	return b.String()
}

// keyLabels are the names the settings show keys by, other than the letters
// and the digits, which show as they are.
var keyLabels = map[string]string{
	"left": "←", "right": "→", "up": "↑", "down": "↓",
	"enter": "Enter", "space": "Space", "tab": "Tab", "escape": "Esc",
	"backspace": "Backspace", "delete": "Delete", "insert": "Insert",
	"home": "Home", "end": "End", "pageup": "PageUp", "pagedown": "PageDown",
	"mediaplaypause": "媒体 播放/暂停", "medianexttrack": "媒体 下一首",
	"mediaprevioustrack": "媒体 上一首", "mediastop": "媒体停止",
	"volumeup": "系统 音量加", "volumedown": "系统 音量减", "volumemute": "系统 静音",
}

// keyLabel is the name the settings show a key by.
func keyLabel(name string) string {
	lower := strings.ToLower(name)
	if label, ok := keyLabels[lower]; ok {
		return label
	}
	if len(name) == 1 {
		return strings.ToUpper(name)
	}
	if len(lower) <= 3 && strings.HasPrefix(lower, "f") {
		return strings.ToUpper(lower)
	}
	return name
}
