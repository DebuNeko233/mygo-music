package app

import (
	"runtime"

	"github.com/egoist/mygo"

	"netease-music-mygo/internal/mixvol"
	"netease-music-mygo/internal/player"
	playbackvm "netease-music-mygo/internal/viewmodel/playback"
	settingsvm "netease-music-mygo/internal/viewmodel/settings"
)

// Run owns the native application lifetime, including partial initialization.
func Run() error {
	if !mygo.App.RequestSingleInstanceLock() {
		return nil
	}
	a := newApp()
	defer a.release()
	a.startup.waiting = true
	a.bindApplication()
	repairFileAssociations()
	a.useUpdater()
	mygo.App.WhenReady(a.start)
	return mygo.App.Run()
}

func newApp() *app {
	a := &app{
		lyricReading:  playbackvm.LyricReading{At: -1},
		mainViewState: mainViewState{queueAt: -1},
		cacheSettings: settingsvm.CacheSettings{Used: -1},
	}
	a.library.Reset()
	a.artists.Reset()
	a.albums.Reset()
	a.users.Reset()
	a.podcasts.Reset()
	a.searches.Reset()
	return a
}

func (a *app) bindApplication() {
	a.native.listen(mygo.App.OnOpenURL(func(raw string) {
		a.showMain()
		if a.openMusicInput(raw) {
			a.redraw()
		}
	}))
	a.native.listen(mygo.App.OnOpenFile(a.openTemporaryAudio))
	a.native.listen(mygo.App.OnSecondInstance(func([]string, string) { a.showMain() }))
	a.native.listen(mygo.App.OnBeforeQuit(func(e *mygo.QuitEvent) {
		if !a.requestQuit(e.SessionEnding) {
			e.PreventDefault()
		}
	}))
	a.native.listen(mygo.App.OnWillQuit(func(*mygo.QuitEvent) { a.stopNative() }))
	a.native.listen(mygo.App.OnWindowAllClosed(func() {}))
	a.native.listen(mygo.App.OnActivate(func(bool) { a.activate() }))
}

func (a *app) start() {
	if a.quitting {
		return
	}
	a.settings = loadSettings()
	a.initialized = true
	a.settings.applyAppearance()
	a.playbackState.Pane = a.lyricsPaneChosen()
	a.preloadPictures()
	if runtime.GOOS == "darwin" {
		// Start Metal's shader and pipeline compilation before the first
		// spectrum draw, without making the window wait for it.
		go lyricSpectrumShader()
	}
	// Before the window: on Windows it lays out its content as it is
	// made, and the view asks the player what plays.
	a.art = newArtCache(a.post, a.postResource)
	a.pl = player.New(a.post, a.postResource)
	a.initLocalMusic()
	a.initOffline()
	a.pl.Quality = func() string { return a.settings.Quality }
	a.pl.Normalize = func() bool { return a.settings.Normalize }
	a.pl.Cache = newSongCache(a.settings)
	a.pl.Changed = func() {
		a.keepSession()
		a.syncCaptions()
		a.syncTrackArtwork()
	}
	a.pl.Playback = a.playback
	a.playbackState.Volume = a.settings.Volume
	// The system's mixer applies the volume too, where it keeps one for
	// the app, which is from the first sound on.
	a.mix = mixvol.Open(func(s mixvol.State) {
		a.post(func() { a.systemVolume(s) })
	})
	a.syncVolume()

	a.openMainWindow()
	a.boot()
	if shotHook != nil {
		shotHook(a)
	}
	if a.startup.show {
		a.showMain()
	}
}
