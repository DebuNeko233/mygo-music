package app

import (
	"sync"

	"github.com/egoist/mygo"

	playbackview "netease-music-mygo/internal/desktop/view/playback"
	"netease-music-mygo/internal/diskcache"
	"netease-music-mygo/internal/mixvol"
	"netease-music-mygo/internal/navigation"
	"netease-music-mygo/internal/player"
	"netease-music-mygo/internal/speech"
	accountvm "netease-music-mygo/internal/viewmodel/account"
	albumvm "netease-music-mygo/internal/viewmodel/album"
	artistvm "netease-music-mygo/internal/viewmodel/artist"
	captionsvm "netease-music-mygo/internal/viewmodel/captions"
	cloudvm "netease-music-mygo/internal/viewmodel/cloud"
	commentsvm "netease-music-mygo/internal/viewmodel/comments"
	downloadsvm "netease-music-mygo/internal/viewmodel/downloads"
	homevm "netease-music-mygo/internal/viewmodel/home"
	libraryvm "netease-music-mygo/internal/viewmodel/library"
	linksvm "netease-music-mygo/internal/viewmodel/links"
	localvm "netease-music-mygo/internal/viewmodel/local"
	playbackvm "netease-music-mygo/internal/viewmodel/playback"
	podcastvm "netease-music-mygo/internal/viewmodel/podcast"
	radiovm "netease-music-mygo/internal/viewmodel/radio"
	recentvm "netease-music-mygo/internal/viewmodel/recent"
	searchvm "netease-music-mygo/internal/viewmodel/search"
	settingsvm "netease-music-mygo/internal/viewmodel/settings"
	uservm "netease-music-mygo/internal/viewmodel/user"
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
	views desktopViews
	mainViewState
	settingsViewState
	appDispatch
	appLifetime
	startup startupState
	account accountvm.Account
	library libraryvm.Library
	home    homevm.Home
	network networkState

	playlistWrites                         sync.Map // cacheRequest to the latest accepted write's done channel
	browseCache, imageCache, identityCache *diskcache.Store
	recovery                               recoveryState
	browseUsed, imagesUsed                 int64

	win    *mygo.Window
	notch  *playbackview.Notch
	lyrics *playbackview.DeskLyrics // the lyrics in their own window, once it is made
	tray   *mygo.Tray               // the icon in the menu bar or notification area, while it shows
	art    *artCache
	pl     *player.Player

	nav navigation.Navigator

	picked playbackview.PickedUp // the song a login picked up, waiting to play

	cloud cloudvm.Cloud

	// What the listener played lately, on any of their devices, and what
	// the page lists: see ui_recent.go.
	recent recentvm.Recent

	// heard is the actual audio session; reports leave in playback order.
	heard           heardPlay
	reportQueue     chan func(playReporter)
	reportDone      chan struct{}
	newPlayReporter func() playReporter

	artists  artistvm.Artists
	albums   albumvm.Albums
	users    uservm.Users
	podcasts podcastvm.Podcasts
	local    localvm.LocalLibrary

	searches searchvm.Search // the results of each search, by its page
	links    linksvm.Links

	playbackState playbackvm.Playback
	lyricReading  playbackvm.LyricReading
	comments      commentsvm.Comments

	radio radiovm.Radio

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

	captions      captionsvm.Captions // the episode playing's captions, and the model that writes them
	captionEngine speech.Engine
	quitting      bool

	// Settings navigation stays in memory; only user preferences are persisted.
	downloads   downloadsvm.Downloads
	preferences settingsvm.Preferences

	settings      settings
	settingsWin   *mygo.Window
	cacheSettings settingsvm.CacheSettings
	updates       bool // the update plugin is used (useUpdater)

	restored    bool // playback picked up where it was left
	sessionSave sessionSaver
	sessionDue  bool // a save of the session is due
}
