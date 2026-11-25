package shortcuts

import (
	"netease-music-mygo/internal/preferences"
)

// InApp and Global identify independently configurable shortcut sets.
const (
	InApp  = "inApp"
	Global = "global"
)

// Action describes a configurable command; the application supplies its execution.
type Action struct{ Name, Label, InApp, Global string }

// Actions returns the ordered command definitions. Callers must not modify them.
func Actions() []Action { return actions[:] }

var actions = [...]Action{
	{Name: "playPause", Label: "播放/暂停", InApp: "Space", Global: "MediaPlayPause"},
	{Name: "prev", Label: "上一首", InApp: "CmdOrCtrl+Shift+Left", Global: "MediaPreviousTrack"},
	{Name: "next", Label: "下一首", InApp: "CmdOrCtrl+Shift+Right", Global: "MediaNextTrack"},
	{Name: "deskLyrics", Label: "桌面歌词", InApp: "CmdOrCtrl+Shift+L", Global: "CmdOrCtrl+Alt+L"},
	{Name: "lyricBigger", Label: "歌词字号变大", InApp: "CmdOrCtrl+Shift+=", Global: "CmdOrCtrl+Alt+Up"},
	{Name: "lyricSmaller", Label: "歌词字号变小", InApp: "CmdOrCtrl+Shift+-", Global: "CmdOrCtrl+Alt+Down"},
	{Name: "volumeUp", Label: "音量增大", InApp: "CmdOrCtrl+Shift+Up", Global: "CmdOrCtrl+Alt+Right"},
	{Name: "volumeDown", Label: "音量减小", InApp: "CmdOrCtrl+Shift+Down", Global: "CmdOrCtrl+Alt+Left"},
}

// Binding is the accelerator the action has in one of the sets, empty when
// the listener gave it none.
func Binding(keys preferences.ShortcutKeys, kind, name string) string {
	bindings := keys.InApp
	if kind == Global {
		bindings = keys.Global
	}
	if acc, ok := bindings[name]; ok {
		return acc
	}
	for _, h := range actions {
		if h.Name == name {
			if kind == Global {
				return h.Global
			}
			return h.InApp
		}
	}
	return ""
}

// Changed reports whether the action has a key other than the one it
// comes with, as the settings offer to give the default back.
func Changed(keys preferences.ShortcutKeys, kind, name string) bool {
	for _, h := range actions {
		if h.Name != name {
			continue
		}
		own := h.InApp
		if kind == Global {
			own = h.Global
		}
		return Canonical(Binding(keys, kind, name)) != Canonical(own)
	}
	return false
}

// Taken reports whether another action of the set already has acc, and whose.
func Taken(keys preferences.ShortcutKeys, kind, name, acc string) (string, bool) {
	canonical := Canonical(acc)
	for _, h := range actions {
		if h.Name == name {
			continue
		}
		other := Binding(keys, kind, h.Name)
		if other != "" && Canonical(other) == canonical {
			return h.Label, true
		}
	}
	return "", false
}

// Set records an explicit binding, including an empty binding that disables it.
func Set(keys *preferences.ShortcutKeys, kind, name, acc string) {
	bindings := &keys.InApp
	if kind == Global {
		bindings = &keys.Global
	}
	if *bindings == nil {
		*bindings = map[string]string{}
	}
	(*bindings)[name] = acc
}

// Restore forgets an override so the command uses its default again.
func Restore(keys *preferences.ShortcutKeys, kind, name string) {
	bindings := keys.InApp
	if kind == Global {
		bindings = keys.Global
	}
	delete(bindings, name)
}

// Fill gives every action the key it has: the one the settings keep, when
// it is a key that set takes, else its default. An action the settings keep
// as empty has none.
func Fill(k preferences.ShortcutKeys) preferences.ShortcutKeys {
	filled := preferences.ShortcutKeys{InApp: map[string]string{}, Global: map[string]string{}}
	for _, h := range actions {
		filled.InApp[h.Name] = h.InApp
		filled.Global[h.Name] = h.Global
		if acc, ok := k.InApp[h.Name]; ok {
			if _, _, parsed := Parse(acc); parsed || acc == "" {
				filled.InApp[h.Name] = acc
			}
		}
		if acc, ok := k.Global[h.Name]; ok {
			if ValidGlobal(acc) || acc == "" {
				filled.Global[h.Name] = acc
			}
		}
	}
	return filled
}
