/*
创建并启动应用，装配模型与窗口，并协调账号恢复和启动打开意图。
*/

package app

import (
	"runtime"

	"github.com/egoist/mygo"

	"netease-music-mygo/internal/features/mixvol"
	"netease-music-mygo/internal/features/netease"
	"netease-music-mygo/internal/features/player"
	artcache "netease-music-mygo/internal/ui/view/artwork"
	playbackview "netease-music-mygo/internal/ui/view/playback"
	playbackvm "netease-music-mygo/internal/ui/viewmodel/playback"
	settingsvm "netease-music-mygo/internal/ui/viewmodel/settings"
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
		mainViewState: mainViewState{State: playbackview.State{QueueAt: -1}},
		cacheSettings: settingsvm.CacheSettings{Used: -1},
	}
	a.library.Reset()
	a.artists.Reset()
	a.albums.Reset()
	a.users.Reset()
	a.podcasts.Reset()
	a.searches.Reset()
	a.bindDesktopViews()
	return a
}

func (a *app) bindApplication() {
	a.native.Listen(mygo.App.OnOpenURL(func(raw string) {
		a.showMain()
		if a.openMusicInput(raw) {
			a.redraw()
		}
	}))
	a.native.Listen(mygo.App.OnOpenFile(a.openTemporaryAudio))
	a.native.Listen(mygo.App.OnSecondInstance(func([]string, string) { a.showMain() }))
	a.native.Listen(mygo.App.OnBeforeQuit(func(e *mygo.QuitEvent) {
		if !a.requestQuit(e.SessionEnding) {
			e.PreventDefault()
		}
	}))
	a.native.Listen(mygo.App.OnWillQuit(func(*mygo.QuitEvent) { a.stopNative() }))
	a.native.Listen(mygo.App.OnWindowAllClosed(func() {}))
	a.native.Listen(mygo.App.OnActivate(func(bool) { a.activate() }))
}

func (a *app) start() {
	if a.quitting {
		return
	}
	a.settings = loadSettings()
	a.initialized = true
	a.settings.applyAppearance()
	a.playbackState.Pane = playbackview.LyricsPaneChosen(a.settings.LyricsStage)
	a.preloadPictures()
	if runtime.GOOS == "darwin" {
		// Start Metal's shader and pipeline compilation before the first
		// spectrum draw, without making the window wait for it.
		go playbackview.LyricSpectrumShader()
	}
	// Before the window: on Windows it lays out its content as it is
	// made, and the view asks the player what plays.
	a.art = artcache.New(artcache.Options{Post: a.post, PostResource: a.postResource})
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
	a.runDebugShots()
	if a.startup.show {
		a.showMain()
	}
}

// Startup keeps one opening intent across account and queue restoration.
// A newer file or link supersedes either kind of older request.
type startupState struct {
	waiting bool
	show    bool
	pending *openingIntent
}

type openingIntent struct {
	file string
	link netease.MusicLink
}

func (s *startupState) queue(intent openingIntent) { s.pending = &intent }

func (s *startupState) take() *openingIntent {
	s.waiting = false
	intent := s.pending
	s.pending = nil
	return intent
}

func (a *app) finishStartup() {
	if a.quitting || !a.restored || a.pl == nil {
		return
	}
	intent := a.startup.take()
	if intent == nil {
		return
	}
	if intent.file != "" {
		a.openTemporaryAudio(intent.file)
	} else {
		a.openMusicLink(intent.link)
	}
}
