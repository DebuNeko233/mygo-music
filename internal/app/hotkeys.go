package app

import (
	"math"
	"os"
	"runtime"
	"strings"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"

	"netease-music-mygo/internal/desktop/shortcuts"
	playbackview "netease-music-mygo/internal/desktop/view/playback"
)

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

// setBinding gives the action acc as its key, empty for none, and keeps it.
func (a *app) setBinding(kind string, name, acc string) {
	shortcuts.Set(&a.settings.Shortcuts, kind, name, acc)
	a.settings.saveSoon()
	if kind == shortcuts.Global {
		a.registerGlobals()
	}
	a.redraw()
}

// restoreBinding gives the action the key it comes with, forgetting the one
// the settings kept for it.
func (a *app) restoreBinding(kind string, name string) {
	shortcuts.Restore(&a.settings.Shortcuts, kind, name)
	a.settings.saveSoon()
	if kind == shortcuts.Global {
		a.registerGlobals()
	}
	a.Recording, a.RecordingNote = "", ""
	a.redraw()
}

// globalRegister and globalUnregister are the system's shortcuts: the tests
// put their own in their place, as there is no system to claim keys from.
var (
	globalRegister = func(acc string, opts mygo.GlobalShortcutOptions, fn func()) error {
		return mygo.GlobalShortcut.RegisterWith(acc, opts, fn)
	}
	globalUnregister = func(acc string) { mygo.GlobalShortcut.Unregister(acc) }
)

// sysKeys says whether the app has a system to claim keys from: it needs a
// window, and the tests, which have none, put their own answer in its place.
var sysKeys = func(a *app) bool { return a.win != nil }

// Hyprland accepts requested accelerators while reporting an empty trigger.
var hyprlandDesktop = func() bool {
	return runtime.GOOS == "linux" && strings.Contains(strings.ToLower(os.Getenv("XDG_CURRENT_DESKTOP")), "hyprland")
}

// registerGlobals claims the keys the global bindings ask for, and gives back
// the ones they no longer do: the system lets one app take a key, so what a
// key is given to has to be claimed again whenever it changes.
func (a *app) registerGlobals() {
	if !sysKeys(a) {
		return
	}
	a.unregisterGlobals()
	generation := a.globalKeyGeneration
	if a.hotkeyErrs == nil {
		a.hotkeyErrs = map[string]string{}
	}
	clear(a.hotkeyErrs)
	for _, h := range shortcuts.Actions() {
		acc := shortcuts.Binding(a.settings.Shortcuts, shortcuts.Global, h.Name)
		if acc == "" {
			continue
		}
		opts := mygo.GlobalShortcutOptions{Description: h.Label, OnKeys: func(keys string) {
			a.post(func() {
				// Portal answers can arrive after rebinding or closing the application.
				if a.quitting || a.globalKeyGeneration != generation {
					return
				}
				if keys == "" && !hyprlandDesktop() {
					a.hotkeyErrs[h.Name] = "快捷键未生效，请检查系统的应用权限和快捷键设置"
				} else {
					delete(a.hotkeyErrs, h.Name)
				}
				a.redraw()
			})
		}}
		if err := globalRegister(acc, opts, func() { a.runHotkey(h.Name) }); err != nil {
			// The system keeps the key for another app, or another action of
			// this one asked for it first: the settings say so.
			a.hotkeyErrs[h.Name] = "被其他程序占用"
			continue
		}
		a.globalKeys = append(a.globalKeys, acc)
	}
}

// unregisterGlobals gives the system back every key the app claimed.
func (a *app) unregisterGlobals() {
	a.globalKeyGeneration++
	for _, acc := range a.globalKeys {
		globalUnregister(acc)
	}
	a.globalKeys = nil
}

// runHotkey runs the action a key was given to, on the window's thread.
func (a *app) runHotkey(name string) {
	a.post(func() {
		hotkeyActions[name](a)
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
	for _, h := range shortcuts.Actions() {
		mods, key, ok := shortcuts.Parse(shortcuts.Binding(a.settings.Shortcuts, shortcuts.InApp, h.Name))
		if !ok {
			continue
		}
		if typing && mods&(ui.Cmd|ui.Ctrl|ui.Alt|ui.Super) == 0 {
			continue
		}
		run := func() {
			hotkeyActions[h.Name](a)
			a.redraw()
		}
		root.OnShortcut(mods, key, run)
		c.OnShortcut(mods, key, run)
	}
}

var hotkeyActions = map[string]func(*app){
	"playPause":    func(a *app) { a.pl.TogglePlay() },
	"prev":         func(a *app) { a.pl.Prev() },
	"next":         func(a *app) { a.pl.Next() },
	"deskLyrics":   func(a *app) { a.views.playback.ToggleDeskLyrics() },
	"lyricBigger":  func(a *app) { a.stepLyricSize(2) },
	"lyricSmaller": func(a *app) { a.stepLyricSize(-2) },
	"volumeUp":     func(a *app) { a.stepVolumeBy(0.05) },
	"volumeDown":   func(a *app) { a.stepVolumeBy(-0.05) },
}
