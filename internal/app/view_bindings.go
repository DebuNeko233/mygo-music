package app

import (
	"time"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"

	desktopview "netease-music-mygo/internal/desktop/view"
	artistview "netease-music-mygo/internal/desktop/view/artist"
	"netease-music-mygo/internal/desktop/view/components"
	libraryview "netease-music-mygo/internal/desktop/view/library"
	localview "netease-music-mygo/internal/desktop/view/local"
	"netease-music-mygo/internal/desktop/view/page"
	playbackview "netease-music-mygo/internal/desktop/view/playback"
	searchview "netease-music-mygo/internal/desktop/view/search"
	settingsview "netease-music-mygo/internal/desktop/view/settings"
	"netease-music-mygo/internal/desktop/view/tracklist"
	"netease-music-mygo/internal/netease"
	"netease-music-mygo/internal/player"
	"netease-music-mygo/internal/preferences"
)

type desktopViews struct {
	playback playbackview.View
	page     page.Page
	album    desktopview.Album
	user     desktopview.User
	recent   desktopview.Recent
	search   searchview.View
	artist   artistview.View
	cloud    desktopview.Cloud
	local    localview.View
	podcast  desktopview.Podcast
	artwork  components.Artwork
	cards    desktopview.Cards
	tracks   tracklist.View
	account  desktopview.Account
	library  libraryview.View
	like     components.Like
	songMenu desktopview.SongMenu
	settings settingsview.View
	daily    desktopview.Daily
	radio    desktopview.Radio
	comments desktopview.Comments
}

func (a *app) bindDesktopViews() {
	p := &a.views.page
	*p = page.Page{Palette: &a.pal, Nav: &a.nav, ScrollState: &a.shell, Offline: func() bool { return a.network.offline }, Renderers: page.Renderers{
		Cover: a.views.artwork.Cover, CoverFill: a.views.artwork.Fill, Link: a.views.artist.Link, HeaderDescription: a.intro.Header, DescriptionSummary: a.intro.Summary,
		TrackRows: a.views.tracks.Rows, TrackRowsMeta: a.views.tracks.RowsMeta,
		Skeleton: func(c *ui.Context, covers bool, header func(*ui.Context)) {
			a.trackSkeleton(c, a.pageShimmer(c), covers, header)
		},
		HeaderSkeleton: func(c *ui.Context) { a.playlistHeaderSkeleton(c, a.pageShimmer(c)) },
		CardsSkeleton: func(c *ui.Context, n int, min float32, freq bool) {
			a.playlistCardsSkeleton(c, a.pageShimmer(c), n, min, freq)
		},
		PlaylistCard: a.views.cards.Playlist, AlbumGrid: a.views.album.Grid, PodcastGrid: a.views.podcast.Grid, ArtistCard: a.views.search.ArtistCard, UserRow: a.views.user.Row, PodcastRow: a.views.podcast.Row, CoverCard: a.views.cards.Cover,
	}}
	a.views.album = desktopview.Album{Page: p, Model: &a.albums, Freshen: a.freshenAlbums, Load: a.loadAlbum, Saved: a.albumSaved, ToggleSaved: a.toggleAlbumSaved, Download: func(tracks []netease.Track) { a.enqueueDownloads(tracks) }, Open: a.open, Play: func(tracks []netease.Track) {
		if a.pl.Shuffle() {
			a.pl.ToggleShuffle()
		}
		a.pl.PlayQueueFrom(tracks, firstPlayable(tracks), a.playSource())
	}}
	a.views.user = desktopview.User{Page: p, Model: &a.users, Account: &a.account, Library: &a.library, Podcasts: &a.podcasts, Count: count, Load: a.loadUser, Open: a.open}
	a.views.recent = desktopview.Recent{Page: p, Model: &a.recent, Account: &a.account, Load: a.loadRecent, LoadTab: a.loadRecentTab, PlayedAt: playedAt, Play: func(tracks []netease.Track) { a.pl.PlayQueue(tracks, 0) }}
	a.views.search = searchview.View{History: &a.settings.SearchHistory, Dismiss: a.dismissSuggestions, ClearHistory: a.clearSearchHistory, Suggest: a.suggest, Search: a.search, Choose: a.choose, TrackSuggestions: a.trackSuggestions, Page: p, Model: &a.searches, More: a.moreResults, Show: a.show, Open: a.open, LoadPlaylist: a.loadPlaylist}
	a.intro.Page = p
	a.views.artist = artistview.View{Page: p, Model: &a.artists, Lookup: a.artist, Load: a.loadArtist, LoadAlbums: a.loadAlbums, LoadEvents: a.loadEvents, LoadLatest: a.loadLatest, ToggleFollow: a.toggleFollow, OpenPage: a.openPage, Count: count, Play: func(tracks []netease.Track, shuffle bool) {
		if a.pl.Shuffle() != shuffle {
			a.pl.ToggleShuffle()
		}
		a.pl.PlayQueueFrom(tracks, firstPlayable(tracks), a.playSource())
	}}
	syncViewport := func(c *ui.Context, name, description string, tracks, at int) {
		a.views.tracks.SyncViewport(c, tracklist.Header{Name: name, Description: description, Tracks: tracks}, at)
	}
	a.views.cloud = desktopview.Cloud{Page: p, Model: &a.cloud, Account: &a.account, Load: a.loadCloud, OpenLogin: a.openLogin, Current: func() *netease.Track { return a.pl.Current() }, CurrentIndex: a.views.tracks.CurrentIndex, SyncViewport: syncViewport, Locate: a.views.tracks.Locate, LocatePlaying: a.views.tracks.LocatePlaying}
	a.views.local = localview.View{Player: func() *player.Player { return a.pl }, OpenLyricMatch: a.openLocalLyricMatch, SearchLyrics: a.findLocalLyricMatches, ConfirmLyrics: a.confirmLocalLyricMatch, Page: p, Model: &a.local, CurrentIndex: a.views.tracks.CurrentIndex, SyncViewport: syncViewport, Locate: a.views.tracks.Locate, AddFolders: a.addLocalFolders, Refresh: a.refreshLocalSongs, OpenFolders: a.openLocalFolders, ChooseFolders: a.chooseLocalFolders, RemoveFolder: a.removeLocalFolder, CloseFolders: func() {
		if a.localView.window != nil {
			a.localView.window.Close()
		}
	}}
	a.views.podcast = desktopview.Podcast{Page: p, Model: &a.podcasts, Library: &a.library, Load: a.loadPodcast, PlayPodcast: a.playPodcast, LoadPrograms: a.loadPrograms, ToggleSubscribe: a.toggleSubscribe, PlayProgram: a.playProgram, LoadHome: a.loadPodcastTab, LoadHot: a.loadHotPodcasts, Open: a.open, Count: count, Current: func() *netease.Track { return a.pl.Current() }, Playing: func() bool { return a.pl.Playing() }, PlayingBars: a.views.playback.NowPlayingBars, LikeButton: a.views.like.Button, DescriptionMore: a.intro.More}

	a.views.artwork = components.Artwork{Palette: &a.pal, PlayerOpen: func() bool { return a.playbackState.Open }, Load: func(url string, backdrop bool, el ui.Element) *ui.Bitmap {
		return bitmap(a.art.getInView(url, backdrop, el))
	}}
	a.views.cards = desktopview.Cards{Page: p, Open: a.open, LoadPlaylist: a.loadPlaylist}
	a.views.tracks = tracklist.View{Page: p, State: &a.tracks, Local: &a.local, Library: &a.library, Current: func() *netease.Track { return a.pl.Current() }, LocalItems: a.localSongItems, SongItems: a.views.songMenu.Items, PlayingBars: a.views.playback.NowPlayingBars, Cover: a.trackCover, ArtistLinks: a.views.artist.Links, Like: a.views.like.Button, Relink: a.chooseLocalFile, Play: a.playTrackQueue}

	a.views.like = components.Like{Palette: &a.pal, SignedIn: func() bool { return a.account.Profile != nil }, AskPrograms: a.askProgramLikes, IsLiked: a.isLiked, Toggle: a.toggleLike}
	a.views.account = desktopview.Account{Page: p, Model: &a.account, OpenLogin: a.openLogin, Settings: a.openSettings, SignOut: a.signOut, Open: a.open, Avatar: a.views.artwork.Avatar, AvatarImage: func(url string) *ui.Bitmap { return bitmap(a.art.get(url, false)) }}
	a.views.library = libraryview.View{Page: p, Model: &a.library, Account: &a.account, Playback: &a.playbackState, RadioState: &a.radio, Load: a.loadPlaylist, CurrentIndex: a.views.tracks.CurrentIndex, SyncViewport: a.views.tracks.SyncPlaylistViewport, Locate: a.views.tracks.Locate, Play: func(tracks []netease.Track) {
		if a.pl.Shuffle() {
			a.pl.ToggleShuffle()
		}
		a.pl.PlayQueueFrom(tracks, firstPlayable(tracks), a.playSource())
	}, Shuffle: func(tracks []netease.Track) { a.pl.PlayShuffledFrom(tracks, a.playSource()) }, Download: func(tracks []netease.Track) { a.enqueueDownloads(tracks) }, Savable: a.savable, Saved: a.isSaved, Editable: a.editable, ToggleSaved: a.toggleSaved, ToggleAlbumSaved: a.toggleAlbumSaved, Radio: func() string { return a.pl.Radio() }, ExitRadio: a.exitRadio, StartHeart: a.startHeartMode, SubmitName: a.submitName, DeleteChosen: a.deleteChosen, Rename: a.startRename, ConfirmDelete: a.confirmDelete, NewPlaylist: a.newPlaylist, AddTo: a.addTo, RemoveFrom: a.removeFrom, Open: a.open}
	a.views.songMenu = desktopview.SongMenu{Player: func() *player.Player { return a.pl }, Copy: func(text string) error { return copyText(text) }, LocalItems: a.localSongItems, Download: func(tracks []netease.Track) { a.enqueueDownloads(tracks) }, OpenArtist: a.openArtist, OpenPage: a.openPage, PlaylistItems: a.views.library.Items}

	a.views.settings = settingsview.View{Page: p, State: &a.settingsViewState, Config: (*preferences.Settings)(&a.settings), Preferences: &a.preferences, Cache: &a.cacheSettings, Account: &a.account, Links: &a.links, Captions: &a.captions, Downloads: &a.downloads, BrowseUsed: &a.browseUsed, ImagesUsed: &a.imagesUsed, PictureExtensions: pictureExts, SystemFontLabel: playbackview.LyricFontSystem,
		Theme: func(t *ui.Theme) ui.Theme { return a.settings.uiTheme(t) }, Colors: paletteFor, Close: func() {
			if a.settingsWin != nil {
				a.settingsWin.Close()
			}
		}, Changed: a.changed, Save: func() { a.settings.saveSoon() }, RegisterLink: a.registerMusicLinkHandler, ChangeAppIcon: a.changeAppIcon, NormalizeVolume: func() { a.pl.SetVolume(a.playbackState.Volume) }, ShowLogin: func() {
			a.showMain()
			a.openLogin()
			if a.win != nil {
				a.win.Invalidate()
			}
		}, SignOut: a.signOut,
		AddPictures: a.addPictures, ForgetPictures: a.forgetPictures, PictureList: pictureList, FontNames: playbackview.LyricFontNames, AccentColor: accentColor, LyricColor: playbackview.LyricColor, HotkeyLabel: hotkeyLabel, HotkeyError: func(name string) string { return a.hotkeyErrs[name] },
		Binding: func(kind, name string) string { return a.binding(keyKind(kind), name) }, ChangedBinding: func(kind, name string) bool { return a.changedBinding(keyKind(kind), name) }, RestoreBinding: func(kind, name string) { a.restoreBinding(keyKind(kind), name) }, SetBinding: func(kind, name, acc string) { a.setBinding(keyKind(kind), name, acc) }, Taken: func(kind, name, acc string) (string, bool) { return a.taken(keyKind(kind), name, acc) }, AcceleratorFor: acceleratorFor, ValidGlobal: validGlobal,
		HasNotch: func() bool { return a.notch != nil }, CaptionsShown: a.captionsShown, Updates: func() bool { return a.updates }, UpdateChecks: func() bool { return updateChecks() }, UpdateDownloads: func() bool { return updateDownloads() }, ModelReady: a.modelReady, LastUpdateCheck: func() time.Time { return lastUpdateCheck() }, SetUpdateChecks: func(on bool) { setUpdateChecks(on) }, SetUpdateDownloads: func(on bool) { setUpdateDownloads(on) }, CheckForUpdates: func() { checkForUpdates() },
		ChooseCacheDirectory: a.chooseCacheDirectory, ApplyCacheLimit: a.applyCacheLimit, ClearCache: a.clearCache, ClearBrowseCache: a.clearBrowseCache, ClearImageCache: a.clearImageCache, CacheUsage: a.cacheUsage, CacheDirectoryDescription: a.cacheDirectoryDescription, DownloadDirectory: a.downloadDirectory, CacheMoveLabel: a.cacheMoveLabel, ConfirmCacheDirectory: a.confirmCacheDirectory, SubmitCacheDirectory: a.submitCacheDirectory, CancelCacheChange: a.cancelCacheChange, ChooseDownloadDirectory: a.chooseDownloadDirectory, RevealDownload: func(path string) { revealDownload(path) }, CancelDownload: a.cancelDownload, RetryDownload: a.retryDownload, SyncCaptions: a.syncCaptions, StartModelDownload: a.startModelDownload, CancelModelDownload: a.cancelModelDownload, DeleteModel: a.deleteModel, CommentTime: commentTime, Avatar: a.views.artwork.Avatar,
	}
	for _, o := range appIcons {
		a.views.settings.AppIcons = append(a.views.settings.AppIcons, settingsview.Choice{Value: o.id, Label: o.label})
	}
	for _, o := range qualityLevels {
		a.views.settings.Qualities = append(a.views.settings.Qualities, settingsview.Choice{Value: o.level, Label: o.label})
	}
	for _, o := range hotkeys {
		a.views.settings.Hotkeys = append(a.views.settings.Hotkeys, settingsview.Choice{Value: o.name, Label: o.label})
	}
	for _, o := range accents {
		a.views.settings.Accents = append(a.views.settings.Accents, settingsview.Choice{Value: o.name, Label: o.name})
	}
	for _, o := range playbackview.LyricColors {
		a.views.settings.LyricColors = append(a.views.settings.LyricColors, settingsview.Choice{Value: o.Name, Label: o.Name})
	}
	for _, o := range cacheLimits {
		a.views.settings.CacheLimits = append(a.views.settings.CacheLimits, settingsview.CacheLimit{MB: o.mb, Label: o.label})
	}
	for _, o := range downloadNameFormats {
		a.views.settings.NameFormats = append(a.views.settings.NameFormats, settingsview.Choice{Value: o.value, Label: o.label})
	}

	p.Image = func(url string, blur bool) components.Image { return viewImage(a.art.get(url, blur)) }
	a.views.daily = desktopview.Daily{Page: p, Model: &a.home, Account: &a.account, Player: func() *player.Player { return a.pl }, Open: a.open, Load: a.loadHome, OpenLogin: a.openLogin, Play: func(tracks []netease.Track, at int) { a.pl.PlayQueueFrom(tracks, at, a.playSource()) }, Shuffle: func(tracks []netease.Track) { a.pl.PlayShuffledFrom(tracks, a.playSource()) }, ArtistLinks: a.views.artist.Links, SongItems: a.views.songMenu.Items}
	a.views.radio = desktopview.Radio{Page: p, Model: &a.radio, Account: &a.account, Player: func() *player.Player { return a.pl }, PlayFM: a.playFM, Trash: a.trashFM, Exit: a.exitRadio, StartHeart: a.startHeartMode, Like: a.views.like.Button, ArtistLinks: a.views.artist.Links}
	a.views.comments = desktopview.Comments{Page: p, For: a.commentsFor, Load: a.loadComments, ToggleLike: a.toggleCommentLike, Avatar: a.views.artwork.Avatar}

	a.views.playback = playbackview.View{
		Page: p, State: &a.mainViewState.State, Playback: &a.playbackState, Reading: &a.lyricReading,
		Captions: &a.captions, Comments: &a.comments, Config: (*preferences.Settings)(&a.settings), Picked: &a.picked,
		Player: func() *player.Player { return a.pl }, Window: func() *mygo.Window { return a.win }, Quitting: func() bool { return a.quitting }, Notch: func() *playbackview.Notch { return a.notch }, DesktopLyrics: func() *playbackview.DeskLyrics { return a.lyrics }, CursorPoint: func() mygo.Point { return cursorPoint() },
		Theme: func(t *ui.Theme) ui.Theme { return a.settings.uiTheme(t) }, SaveSettings: func() { a.settings.saveSoon() },
		ProgramPane: a.views.podcast.ProgramPane, CommentsPane: a.views.comments.Pane, ArtistLinks: a.views.artist.Links, TrackCover: a.trackCover, LikeButton: a.views.like.Button, SongMenu: a.views.songMenu.Button, SongItems: a.views.songMenu.Items, ModelCard: a.views.settings.ModelCard, LocalLyricMatchButton: a.views.local.LyricMatchButton, Avatar: a.views.artwork.Avatar,
		CommentsFor: a.commentsFor, CommentsTab: a.commentsTab, CommentCount: a.commentCount, CaptionsOn: a.captionsOn, CaptionsShown: a.captionsShown, CaptionsWritten: a.captionsWritten, ModelReady: a.modelReady, CaptionsWriting: a.captionsWriting,
		SyncCaptions: a.syncCaptions, SyncVolume: a.syncVolume, SettingsChanged: a.changed, Redraw: a.redraw, OpenSettings: a.openSettings, ShowMain: a.showMain, ExitRadio: a.exitRadio, SyncWindows: a.syncWindows, Dispatch: a.dispatch, VolumeServices: a.volumeServices, Pictures: a.pictures,
		TrackArtwork: func(tr *netease.Track, px int, blur bool) components.Image {
			return viewImage(a.trackArtwork(tr, px, blur))
		}, HasTrackCover: a.hasTrackCover,
		BarArtwork: func(tr *netease.Track) components.Image {
			return viewImage(a.playbackTrackArtwork(tr, barCoverRequest))
		}, StageArtwork: func(tr *netease.Track) components.Image {
			return viewImage(a.playbackTrackArtwork(tr, nowPlayingCoverRequest))
		},
		ListenNative: a.native.Listen,
	}

}

func (a *app) localFoldersView(c *ui.Context) {
	t := a.settings.uiTheme(c.Theme())
	a.views.local.Folders(c, t, paletteFor(&t))
}

func (a *app) coverImage(c *ui.Context, size, radius float32, load func(ui.Element) *artwork) ui.Element {
	return a.views.artwork.Image(c, size, radius, func(el ui.Element) *ui.Bitmap { return bitmap(load(el)) })
}

func (a *app) recordKey(kind keyKind, name string, event ui.InputEvent) bool {
	return a.views.settings.RecordKey(string(kind), name, event)
}

func count(n int) string                              { return components.Count(n) }
func commentTime(t, now time.Time) string             { return components.CommentTime(t, now) }
func hsv(r, g, b float64) (float64, float64, float64) { return components.HSV(r, g, b) }
