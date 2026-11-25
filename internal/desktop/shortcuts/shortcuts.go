package shortcuts

import (
	"runtime"
	"strings"

	"github.com/egoist/mygo/ui"
)

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

// Parse turns an accelerator into the modifiers and the key the
// window binds them with, false for one the window cannot take, as a media
// key, which only the system sees.
func Parse(acc string) (ui.Modifiers, ui.Key, bool) {
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

// Format is the accelerator of a key and the modifiers held with it,
// false for a modifier on its own, which is no key yet.
func Format(mods ui.Modifiers, key ui.Key) (string, bool) {
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

// Canonical is the accelerator as the app compares them: its
// modifiers in the app's order, and its key in lower case.
func Canonical(acc string) string {
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

// ValidGlobal reports whether a key can be claimed from the whole system: one
// with a modifier, or a media or function key, but not a bare letter, which
// would take that key from every other app.
func ValidGlobal(acc string) bool {
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

// Label is the accelerator as the settings show it: the symbols of the
// platform's shortcuts on macOS, its names elsewhere, and 未绑定 for none.
func Label(acc string) string {
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
