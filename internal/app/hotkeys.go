package app

import (
	"math"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"

	"netease-music-mygo/internal/desktop/shortcuts"
	playbackview "netease-music-mygo/internal/desktop/view/playback"
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
		run: func(a *app) { a.views.playback.ToggleDeskLyrics() }},
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
	a.setVolume(math.Round(min(1, max(0, a.playbackState.Volume+d))*100) / 100)
}

// stepLyricSize makes the lines of the lyrics window d points larger or
// smaller, within the sizes it takes.
func (a *app) stepLyricSize(d float64) {
	size := min(playbackview.LyricSizeMax, max(playbackview.LyricSizeMin, a.settings.LyricSize+d))
	if size == a.settings.LyricSize {
		return
	}
	a.settings.LyricSize = size
	a.settings.saveSoon()
	if a.lyrics != nil {
		a.lyrics.Apply()
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
		return shortcuts.Canonical(a.binding(kind, name)) != shortcuts.Canonical(own)
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
	a.Recording, a.RecordingNote = "", ""
	a.redraw()
}

// taken reports whether another action of the set already has acc, and whose.
func (a *app) taken(kind keyKind, name, acc string) (string, bool) {
	canonical := shortcuts.Canonical(acc)
	for _, h := range hotkeys {
		if h.name == name {
			continue
		}
		other := a.binding(kind, h.name)
		if other != "" && shortcuts.Canonical(other) == canonical {
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
	if a.Recording != "" {
		return
	}
	// A field that has the focus is typed into, and a key without a
	// modifier is a letter, a space or a digit to it, not an action: the
	// search can be given K to play, but then there is no typing k in it.
	// Only this window's search is bound here, so no other field of this
	// view needs to be kept out.
	typing := c.Focused(a.shell.SearchBox)
	for _, h := range hotkeys {
		mods, key, ok := shortcuts.Parse(a.binding(kindInApp, h.name))
		if !ok {
			continue
		}
		if typing && mods&(ui.Cmd|ui.Ctrl|ui.Alt|ui.Super) == 0 {
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
