/*
定义应用组合根，集中持有共享模型、服务及各窗口的协调状态。
*/

package app

import (
	"sync"

	"github.com/egoist/mygo"

	diskcache "netease-music-mygo/internal/features/cache/disk"
	"netease-music-mygo/internal/features/mixvol"
	"netease-music-mygo/internal/features/navigation"
	"netease-music-mygo/internal/features/player"
	sessionstore "netease-music-mygo/internal/features/player/session"
	"netease-music-mygo/internal/features/speech"
	artcache "netease-music-mygo/internal/ui/view/artwork"
	playbackview "netease-music-mygo/internal/ui/view/playback"
	accountvm "netease-music-mygo/internal/ui/viewmodel/account"
	albumvm "netease-music-mygo/internal/ui/viewmodel/album"
	artistvm "netease-music-mygo/internal/ui/viewmodel/artist"
	captionsvm "netease-music-mygo/internal/ui/viewmodel/captions"
	cloudvm "netease-music-mygo/internal/ui/viewmodel/cloud"
	commentsvm "netease-music-mygo/internal/ui/viewmodel/comments"
	downloadsvm "netease-music-mygo/internal/ui/viewmodel/downloads"
	homevm "netease-music-mygo/internal/ui/viewmodel/home"
	libraryvm "netease-music-mygo/internal/ui/viewmodel/library"
	linksvm "netease-music-mygo/internal/ui/viewmodel/links"
	localvm "netease-music-mygo/internal/ui/viewmodel/local"
	playbackvm "netease-music-mygo/internal/ui/viewmodel/playback"
	podcastvm "netease-music-mygo/internal/ui/viewmodel/podcast"
	radiovm "netease-music-mygo/internal/ui/viewmodel/radio"
	recentvm "netease-music-mygo/internal/ui/viewmodel/recent"
	searchvm "netease-music-mygo/internal/ui/viewmodel/search"
	settingsvm "netease-music-mygo/internal/ui/viewmodel/settings"
	uservm "netease-music-mygo/internal/ui/viewmodel/user"
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

	win      *mygo.Window
	notch    *playbackview.Notch
	lyrics   *playbackview.DeskLyrics // the lyrics in their own window, once it is made
	tray     *mygo.Tray               // the icon in the menu bar or notification area, while it shows
	art      *artcache.Cache
	pl       *player.Player
	dockMenu *mygo.Menu // owned separately from the tray and application menus

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
	globalKeys          []string
	globalKeyGeneration uint64
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

	restored        bool // playback picked up where it was left
	sessionSave     sessionstore.Saver
	sessionSavePath string
	sessionDue      bool // a save of the session is due
}
