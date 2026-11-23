package app

import (
	"log"
	"runtime"
	"sync"
	"time"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"

	"netease-music-mygo/internal/diskcache"
	"netease-music-mygo/internal/lyrics"
	"netease-music-mygo/internal/mixvol"
	"netease-music-mygo/internal/navigation"
	"netease-music-mygo/internal/player"
	"netease-music-mygo/internal/preferences"
	"netease-music-mygo/internal/viewmodel"
)

type pageKind = navigation.Page
type route = navigation.Route

const (
	pageHome     = navigation.Home
	pageDaily    = navigation.Daily
	pageRecent   = navigation.Recent
	pageCharts   = navigation.Charts
	pagePlaylist = navigation.Playlist
	pageSearch   = navigation.Search
	pageFM       = navigation.FM
	pageArtist   = navigation.Artist
	pageAlbum    = navigation.Album
	pageUser     = navigation.User
	pagePodcast  = navigation.Podcast
	pagePodcasts = navigation.Podcasts
	pageLocal    = navigation.Local
	pageCloud    = navigation.Cloud
)

// app is the state both windows show. Everything in it is read and changed
// on the main thread: by the views, and by goroutines through post.
type app struct {
	account viewmodel.Account
	library viewmodel.Library
	home    viewmodel.Home
	network networkState

	playlistWrites                         sync.Map // cacheRequest to the latest accepted write's done channel
	browseCache, imageCache, identityCache *diskcache.Store
	recovery                               recoveryState
	browseUsed, imagesUsed                 int64

	wasBehind bool

	win    *mygo.Window
	notch  *notch
	lyrics *deskLyrics // the lyrics in their own window, once it is made
	tray   *mygo.Tray  // the icon in the menu bar or notification area, while it shows
	art    *artCache
	pl     *player.Player
	pal    palette
	pics   map[string]*backdropEntry // shared by the home and playing backdrops

	nav navigation.Navigator

	accountOpen bool // the dropdown of the account row shows
	intro       descriptionDialog
	toast       string   // a message to show once
	picked      pickedUp // the song a login picked up, waiting to play

	cloud viewmodel.Cloud

	// What the listener played lately, on any of their devices, and what
	// the page lists: see ui_recent.go.
	recent viewmodel.Recent

	// heard is the actual audio session; reports leave in playback order.
	heard           heardPlay
	reportQueue     chan func(playReporter)
	reportDone      chan struct{}
	newPlayReporter func() playReporter

	dailySpin      spinState // the record on the daily card
	dailyBars      barState  // the bars on its play button
	artists        viewmodel.Artists
	albums         viewmodel.Albums
	users          viewmodel.Users
	podcasts       viewmodel.Podcasts
	trackList      ui.ListState
	trackRowsCache trackRowsCache
	local          viewmodel.LocalLibrary
	localView      localViewState
	trackArt       trackArtworkState
	// Where the page and the sidebar are scrolled, kept while Now Playing
	// covers them, unbuilt.
	pageScroll ui.ScrollState
	navScroll  ui.ScrollState
	scrolled   route // the page trackList and pageScroll are of

	locateViewport trackViewport

	mineCollapsed, savedCollapsed, albumsCollapsed bool
	searchBox                                      ui.Handle
	searches                                       viewmodel.Search // the results of each search, by its page
	links                                          viewmodel.Links

	playbackState                  viewmodel.Playback
	npOpen                         bool      // Now Playing as it last showed: open or not
	npChangedAt                    time.Time // when it last opened or closed
	npPointer                      ui.Point
	npPointerUntil                 time.Time
	lyricScroll                    ui.ScrollState
	lyricGlide                     lyricGlide
	lyricBrowseUntil               time.Time
	lyricResizeAnchor              lyricReadingAnchor
	lyricBlurPass                  *ui.Canvas
	paintedLyric                   paintedLyric
	playbackFont                   lyricFontSelection
	timedList                      timedListLayout
	lyricAt                        int
	lyricTrack                     int64
	lyricSeekRevision              uint64
	lyricClickSeekRevision         uint64
	lyricFirst                     *lyrics.Line
	lyricViewportW, lyricViewportH float32 // last viewport followed; resize can reflow lines
	lyricWindowW, lyricWindowH     float32 // window size whose reflow has been scheduled
	queueList                      ui.ListState
	queueAt                        int // the row of the queue pane last scrolled to, -1 while it is hidden
	comments                       commentState
	tabCount                       rollState // the count on the comments' tab, as it rolls
	stageWords                     stageWordLayout

	radio viewmodel.Radio

	volumeOpen       bool    // the volume popover of the player bar shows
	volumeScroll     float64 // precise scroll DIPs saved toward the next ten-percent step
	volumeScrollDown bool    // the direction those DIPs were going
	// mix is the app's volume in the system's mixer, when the system keeps
	// one: it applies the volume the app shows, and what it is set to there
	// comes back to volume.
	mix *mixvol.Mixer
	// globalKeys are the keys the system holds for the app, which they give
	// back when the bindings change and when the app quits.
	globalKeys []string
	// hotkeyErrs says which of the global bindings the system refused, by
	// action: another app holds them.
	hotkeyErrs map[string]string
	// recording is the binding the settings are recording a key for, as
	// "kind/action", empty while they record none, and recordingNote is what
	// the recording has to say about the last key pressed.
	recording     string
	recordingNote string

	rowBars barState
	// underTop and underBottom say whether the page has content under its
	// bars, which show their scroll edges then.
	underTop, underBottom bool
	barScrub              scrubState
	barCover              coverFadeState
	stageCover, wallCover coverFadeState
	npScrub               scrubState
	backdropCache         backdropCache // the backdrop of Now Playing, as last painted
	npSpectrum            lyricSpectrum // the filled frequency spectrum behind the lyrics
	npArt                 *artwork      // the cover whose colors Now Playing shows
	npCoverState          coverState    // the cover of Now Playing, as it is tilted and flicked
	wallState             wallState     // the wall of the queue's covers, as it is dragged
	quiet                 quietState    // the quiet parts of the song playing, for its hot comments
	captions              captionState  // the episode playing's captions, and the model that writes them
	sig                   playerSig
	quitting              bool

	// Settings navigation stays in memory; only user preferences are persisted.
	downloads         viewmodel.Downloads
	downloadList      ui.ListState
	preferences       viewmodel.Preferences
	settingsSearchBox ui.Handle
	settingsUnderTop  bool

	settings      settings
	settingsWin   *mygo.Window
	cacheSettings viewmodel.CacheSettings
	updates       bool // the update plugin is used (useUpdater)

	restored    bool // playback picked up where it was left
	sessionSave sessionSaver
	sessionDue  bool // a save of the session is due

	// Workers can finish before NewWindow returns. Protect their early queue
	// and dispatcher publication; callbacks always run unlocked.
	postMu sync.Mutex
	postTo func(func(), bool) // main-thread dispatcher, published with the window
	kept   []func()
}

func (a *app) post(fn func()) { a.dispatch(fn, true) }

// postIfChanged lets a worker finish bookkeeping without rebuilding the view
// when the result does not change what any window shows.
func (a *app) postIfChanged(update func() bool) {
	a.dispatch(func() {
		if update() {
			a.redraw()
		}
	}, false)
}

// dispatch queues early results until a dispatcher exists. Native windows and
// headless tests each own their queue; a late worker cannot use another app's
// dispatcher. Never invoke callbacks while holding postMu.
func (a *app) dispatch(fn func(), redraw bool) {
	a.postMu.Lock()
	send := a.postTo
	if send == nil {
		a.kept = append(a.kept, fn)
		a.postMu.Unlock()
		return
	}
	a.postMu.Unlock()
	send(fn, redraw)
}

// flushKept runs what post kept while the window was being made.
func (a *app) flushKept() {
	a.postMu.Lock()
	kept := a.kept
	a.kept = nil
	a.postMu.Unlock()
	for _, fn := range kept {
		fn()
	}
	if len(kept) > 0 {
		a.redraw()
	}
}

func (a *app) redraw() {
	if a.win != nil {
		a.win.Invalidate()
	}
	if a.notch != nil && a.notch.win != nil {
		a.notch.win.Invalidate()
	}
	if a.lyrics != nil && a.lyrics.win != nil {
		a.lyrics.win.Invalidate()
	}
}

// shotHook, when set, drives the app to check its design.
var shotHook func(a *app)

func (a *app) showMain() {
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

// saveFinalSettings reports a write failure before the process exits, when a
// later view can no longer carry a toast.
func (a *app) saveFinalSettings() {
	if err := a.settings.save(); err != nil {
		log.Printf("settings: final save: %v", err)
	}
}

// Run runs the app until it quits.
func Run() error {
	if !mygo.App.RequestSingleInstanceLock() {
		return nil
	}
	a := &app{
		lyricAt:       -1,
		queueAt:       -1,
		cacheSettings: viewmodel.CacheSettings{Used: -1},
	}
	a.library.Reset()
	a.artists.Reset()
	a.albums.Reset()
	a.users.Reset()
	a.podcasts.Reset()
	a.searches.Reset()
	a.links.Starting = true
	offURL := mygo.App.OnOpenURL(func(raw string) {
		a.showMain()
		if a.openMusicInput(raw) {
			a.redraw()
		}
	})
	defer offURL()
	offFile := mygo.App.OnOpenFile(a.openTemporaryAudio)
	defer offFile()
	offSecond := mygo.App.OnSecondInstance(func([]string, string) { a.showMain() })
	defer offSecond()
	mygo.App.OnWillQuit(func(*mygo.QuitEvent) { a.cancelMusicLink() })
	repairFileAssociations()
	a.useUpdater()
	mygo.App.WhenReady(func() {
		a.settings = loadSettings()
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
		a.art = newArtCache(a.post)
		a.pl = player.New(a.post)
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
		mygo.App.OnWillQuit(func(*mygo.QuitEvent) { a.flushPlayReports() })
		a.playbackState.Volume = a.settings.Volume
		// The system's mixer applies the volume too, where it keeps one for
		// the app, which is from the first sound on.
		a.mix = mixvol.Open(func(s mixvol.State) {
			a.post(func() { a.systemVolume(s) })
		})
		mygo.App.OnWillQuit(func(*mygo.QuitEvent) { a.mix.Close() })
		a.syncVolume()
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
		a.postMu.Lock()
		a.win = win
		a.postTo = func(fn func(), redraw bool) {
			if redraw {
				win.Update(func() { fn(); a.redraw() })
			} else {
				mygo.RunOnMain(fn)
			}
		}
		a.postMu.Unlock()
		if setupNativeRowBars != nil {
			setupNativeRowBars(a)
		}
		// The view the window laid out as it was made asked for pictures
		// before there was a window to read them back into.
		a.flushKept()
		a.setMenu()
		a.watchNetwork()
		a.recoverCacheDirectory()
		a.refreshLocalSongs()
		a.boot()
		// The system's keys are claimed once there is a window to say the
		// app runs, and given back as it quits.
		a.registerGlobals()
		mygo.App.OnWillQuit(func(*mygo.QuitEvent) { a.unregisterGlobals() })
		// The notch player is macOS's, over its menu bar: elsewhere it would
		// hang at the top of the screen as a black box.
		if runtime.GOOS == "darwin" {
			a.notch = newNotch(a)
		}
		// The lyrics' own window is every platform's, hidden until the
		// listener asks for it.
		a.lyrics = newDeskLyrics(a)
		// From the menu bar alone, the Dock icon shows while the window
		// does.
		a.syncDock()
		a.win.OnShow(a.syncDock)
		a.win.OnHide(a.syncDock)
		a.win.OnHide(func() { a.recoveryFrame(false) })
		// Shown or switched to the morning after, it shows the new day's.
		a.win.OnFocus(a.freshenHome)
		a.win.OnFocus(func() {
			if a.nav.Current.Page == pageLocal {
				a.refreshLocalSongs()
			}
		})
		a.applyTray()
		if shotHook != nil {
			shotHook(a)
		}
		// Closing the window hides it: music plays on, and the Dock icon
		// or the tray's brings it back. A Linux desktop without a tray
		// has neither, so there closing it quits.
		a.win.OnClose(func(e *mygo.CloseEvent) {
			if a.quitting {
				return
			}
			e.PreventDefault()
			if runtime.GOOS == "darwin" || a.tray != nil {
				a.win.Hide()
			} else {
				mygo.App.Quit()
			}
		})
	})
	mygo.App.OnBeforeQuit(func(e *mygo.QuitEvent) {
		if a.deferCacheQuit() {
			e.PreventDefault()
			return
		}
		if a.deferDownloadQuit() {
			e.PreventDefault()
			return
		}
		a.quitting = true
		a.account.CloseLogin()
		a.cancelCloudLoad()
		a.cancelDownloads()
		a.settings.Volume = a.playbackState.Volume
		if a.lyrics != nil {
			// Where the lyrics were left, before the window goes.
			a.lyrics.remember()
		}
		// A write still owed from a key repeat would otherwise land
		// after this one, with the settings as they were before it.
		preferences.Flush()
		a.saveFinalSettings()
		a.saveSession()
		a.sessionSave.wait()
		if write := a.transcriptWrite(); write != nil {
			write()
		}
	})
	// Windows leaves an icon in the notification area after its app until
	// the pointer passes over it.
	mygo.App.OnWillQuit(func(*mygo.QuitEvent) {
		if a.tray != nil {
			a.tray.Destroy()
		}
	})
	mygo.App.OnWindowAllClosed(func() {})
	mygo.App.OnActivate(func(bool) { a.showMain() })
	err := mygo.App.Run()
	a.art.close()
	if a.network.stop != nil {
		a.network.stop()
	}
	a.browseCache.Close()
	a.imageCache.Close()
	a.identityCache.Close()
	return err
}
