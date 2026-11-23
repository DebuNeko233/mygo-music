package app

import (
	"sync"

	"github.com/egoist/mygo"

	"netease-music-mygo/internal/diskcache"
	"netease-music-mygo/internal/mixvol"
	"netease-music-mygo/internal/navigation"
	"netease-music-mygo/internal/player"
	"netease-music-mygo/internal/speech"
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

// app composes shared models and services. Views own widget/animation state;
// startup, windows and shutdown coordinate their lifetimes on the UI thread.
type app struct {
	mainViewState
	settingsViewState
	appDispatch
	appLifetime
	startup startupState
	account viewmodel.Account
	library viewmodel.Library
	home    viewmodel.Home
	network networkState

	playlistWrites                         sync.Map // cacheRequest to the latest accepted write's done channel
	browseCache, imageCache, identityCache *diskcache.Store
	recovery                               recoveryState
	browseUsed, imagesUsed                 int64

	win    *mygo.Window
	notch  *notch
	lyrics *deskLyrics // the lyrics in their own window, once it is made
	tray   *mygo.Tray  // the icon in the menu bar or notification area, while it shows
	art    *artCache
	pl     *player.Player

	nav navigation.Navigator

	picked pickedUp // the song a login picked up, waiting to play

	cloud viewmodel.Cloud

	// What the listener played lately, on any of their devices, and what
	// the page lists: see ui_recent.go.
	recent viewmodel.Recent

	// heard is the actual audio session; reports leave in playback order.
	heard           heardPlay
	reportQueue     chan func(playReporter)
	reportDone      chan struct{}
	newPlayReporter func() playReporter

	artists  viewmodel.Artists
	albums   viewmodel.Albums
	users    viewmodel.Users
	podcasts viewmodel.Podcasts
	local    viewmodel.LocalLibrary

	searches viewmodel.Search // the results of each search, by its page
	links    viewmodel.Links

	playbackState viewmodel.Playback
	lyricReading  viewmodel.LyricReading
	comments      viewmodel.Comments

	radio viewmodel.Radio

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

	captions      viewmodel.Captions // the episode playing's captions, and the model that writes them
	captionEngine speech.Engine
	quitting      bool

	// Settings navigation stays in memory; only user preferences are persisted.
	downloads   viewmodel.Downloads
	preferences viewmodel.Preferences

	settings      settings
	settingsWin   *mygo.Window
	cacheSettings viewmodel.CacheSettings
	updates       bool // the update plugin is used (useUpdater)

	restored    bool // playback picked up where it was left
	sessionSave sessionSaver
	sessionDue  bool // a save of the session is due
}
