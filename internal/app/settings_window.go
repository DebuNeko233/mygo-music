/*
管理设置窗口的打开与配置变更，将修改应用到各窗口和服务。
*/

package app

import (
	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"
)

// openSettings shows the settings window, opening it the first time.
func (a *app) openSettings() {
	if a.quitting {
		return
	}
	a.refreshMusicLinkHandler()
	// A key the settings were recording before they closed is not being
	// recorded now.
	a.Recording, a.RecordingNote = "", ""
	if a.settingsWin != nil {
		a.settingsWin.Show()
		a.settingsWin.Focus()
		a.measureCache()
		a.measureOfflineCache()
		return
	}
	a.measureCache()
	a.measureOfflineCache()
	a.settingsWin = mygo.NewWindow(mygo.WindowOptions{
		Title:                "设置",
		Width:                780,
		Height:               640,
		MinWidth:             640,
		MinHeight:            420,
		TitleBarStyle:        mygo.TitleBarHidden,
		TitleBarHeight:       buttonsHeight,
		TrafficLightPosition: &mygo.Point{X: 20, Y: 20},
		// Older saved states can restore fullscreen even when it is disabled.
		StateKey:          "settings-pages-windowed",
		DisableFullScreen: true,
		DisableMaximize:   true,
		DisableMinimize:   true,
		// The app's menu has nothing for this window: on Linux and
		// Windows it stays out of sight until Alt shows it.
		AutoHideMenuBar: true,
		BackgroundColor: "light-dark(#f4f4f6, #1c1c1f)",
		Content:         ui.View(a.views.settings.Build),
	})
	w := a.settingsWin
	w.OnFocus(a.refreshMusicLinkHandler)
	w.OnClosed(func() { a.settingsClosed(w) })
	a.syncDock()
}

// changed applies the settings everywhere and keeps them.
func (a *app) changed() {
	a.settings.saveSoon()
	a.settings.takeAccent()
	// The appearance is the windows' to take, and without one, as in
	// tests, there is nothing to take it.
	if a.win != nil {
		a.settings.applyAppearance()
	}
	if a.notch != nil {
		a.notch.SetEnabled(a.settings.Notch)
	}
	if a.lyrics != nil {
		a.lyrics.Apply()
	}
	a.redraw()
}
