package app

import (
	"runtime"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"

	playbackview "netease-music-mygo/internal/desktop/view/playback"
)

// shotHook, when set, drives the app to check its design.
var shotHook func(a *app)

// Overlay windows can be visible while all ordinary windows are hidden.
// Reopening from the Dock restores the main window unless a utility is open.
func (a *app) activate() {
	if a.settingsWin == nil && a.localView.window == nil {
		a.showMain()
	}
}

func (a *app) settingsClosed(w *mygo.Window) {
	if a.settingsWin != w {
		return
	}
	a.settingsWin = nil
	a.cacheSettings.Confirm.Open = false
	a.settingsViewState = settingsViewState{}
	if !a.quitting {
		a.syncDock()
	}
}

func (a *app) showMain() {
	if a.quitting {
		return
	}
	a.startup.show = a.win == nil
	if a.win != nil {
		if a.win.IsMinimized() {
			a.win.Restore()
		}
		a.win.Show()
		a.win.Focus()
		// From the menu bar alone the app may be inactive.
		mygo.App.Focus()
	}
}

func (a *app) openMainWindow() {
	win := mygo.NewWindow(mygo.WindowOptions{
		Title:         mygo.App.Name(),
		Width:         1180,
		Height:        780,
		MinWidth:      880,
		MinHeight:     580,
		StateKey:      "main",
		TitleBarStyle: mygo.TitleBarHidden,
		// As tall as the edge over a page (Linux, Windows).
		TitleBarHeight: buttonsHeight,
		// Inset from the corner, within the sidebar, as macOS 26's
		// apps with a sidebar place them.
		TrafficLightPosition: &mygo.Point{X: 20, Y: 20},
		Vibrancy:             mygo.VibrancySidebar,
		BackgroundColor:      "light-dark(#ffffff, #141416)",
		Content:              ui.View(a.view),
	})
	a.win = win
	// Window.Update is asynchronous even on the owner thread. Its callback
	// still runs if the main window is hidden; other windows share the state.
	a.postMu.Lock()
	a.postTo = func(fn func(), redraw bool) {
		if redraw {
			win.Update(func() { fn(); a.redraw() })
		} else {
			mygo.RunOnMain(fn)
		}
	}
	a.postMu.Unlock()
	a.views.playback.SetupNativeRowBars()
	// The view the window laid out as it was made asked for pictures
	// before there was a window to read them back into.
	a.flushKept()
	a.setMenu()
	a.watchNetwork()
	a.recoverCacheDirectory()
	a.refreshLocalSongs()
	a.registerGlobals()
	a.openAuxiliaryPlayers()
}

func (a *app) openAuxiliaryPlayers() {
	// The notch player is macOS's, over its menu bar: elsewhere it would
	// hang at the top of the screen as a black box.
	if runtime.GOOS == "darwin" {
		a.notch = playbackview.NewNotch(&a.views.playback)
	}
	// The lyrics' own window is every platform's, hidden until the
	// listener asks for it.
	a.lyrics = playbackview.NewDeskLyrics(&a.views.playback)
	// From the menu bar alone, the Dock icon shows while the window
	// does.
	a.syncDock()
	a.native.Listen(a.win.OnShow(a.syncDock))
	a.native.Listen(a.win.OnHide(a.syncDock))
	a.native.Listen(a.win.OnHide(func() { a.recoveryFrame(false) }))
	// Shown or switched to the morning after, it shows the new day's.
	a.native.Listen(a.win.OnFocus(a.freshenHome))
	a.native.Listen(a.win.OnFocus(func() {
		if a.nav.Current.Page == pageLocal {
			a.refreshLocalSongs()
		}
	}))
	a.applyTray()
	// Closing the window hides it: music plays on, and the Dock icon
	// or the tray's brings it back. A Linux desktop without a tray
	// has neither, so there closing it quits.
	a.native.Listen(a.win.OnClose(func(e *mygo.CloseEvent) {
		if a.quitting {
			return
		}
		e.PreventDefault()
		if runtime.GOOS == "darwin" || a.tray != nil {
			a.win.Hide()
		} else {
			mygo.App.Quit()
		}
	}))
}
