/*
为首页、搜索和详情等浏览视图注入共享数据、渲染能力与应用命令。
*/

package app

import (
	"github.com/egoist/mygo/ui"

	"netease-music-mygo/internal/features/netease"
	"netease-music-mygo/internal/features/player"
	desktopview "netease-music-mygo/internal/ui/view"
	artistview "netease-music-mygo/internal/ui/view/artist"
	"netease-music-mygo/internal/ui/view/components"
	localview "netease-music-mygo/internal/ui/view/local"
	searchview "netease-music-mygo/internal/ui/view/search"
	"netease-music-mygo/internal/ui/view/tracklist"
)

func (a *app) bindBrowseViews() {
	p := &a.views.page
	a.views.album = desktopview.Album{Page: p, Model: &a.albums, Freshen: a.freshenAlbums, Load: a.loadAlbum, Saved: a.library.AlbumSaved, ToggleSaved: a.toggleAlbumSaved, Download: func(tracks []netease.Track) { a.enqueueDownloads(tracks) }, Open: a.open, Play: a.playPageTracks}
	a.views.user = desktopview.User{Page: p, Model: &a.users, Account: &a.account, Library: &a.library, Podcasts: &a.podcasts, Count: components.Count, Load: a.loadUser, Open: a.open}
	a.views.recent = desktopview.Recent{Page: p, Model: &a.recent, Account: &a.account, Load: a.loadRecent, LoadTab: a.loadRecentTab, PlayedAt: components.PlayedAt, Play: func(tracks []netease.Track) { a.pl.PlayQueue(tracks, 0) }}
	a.views.search = searchview.View{History: &a.settings.SearchHistory, Dismiss: a.searches.DismissSuggestions, ClearHistory: a.clearSearchHistory, Suggest: a.suggest, Search: a.search, Choose: a.choose, TrackSuggestions: a.trackSuggestions, Page: p, Model: &a.searches, More: a.moreResults, Show: a.show, Open: a.open, LoadPlaylist: a.loadPlaylist}
	a.views.artist = artistview.View{Page: p, Model: &a.artists, Lookup: a.artists.Page, Load: a.loadArtist, LoadAlbums: a.loadAlbums, LoadEvents: a.loadEvents, LoadLatest: a.loadLatest, ToggleFollow: a.toggleFollow, OpenPage: a.openPage, Count: components.Count, Play: a.playPageTracksMode}
	syncViewport := func(c *ui.Context, name, description string, tracks, at int) {
		a.views.tracks.SyncViewport(c, tracklist.Header{Name: name, Description: description, Tracks: tracks}, at)
	}
	a.views.cloud = desktopview.Cloud{Page: p, Model: &a.cloud, Account: &a.account, Load: a.loadCloud, OpenLogin: a.openLogin, Current: func() *netease.Track { return a.pl.Current() }, CurrentIndex: a.views.tracks.CurrentIndex, SyncViewport: syncViewport, Locate: a.views.tracks.Locate, LocatePlaying: a.views.tracks.LocatePlaying}
	a.views.local = localview.View{Player: func() *player.Player { return a.pl }, OpenLyricMatch: a.openLocalLyricMatch, SearchLyrics: a.findLocalLyricMatches, ConfirmLyrics: a.confirmLocalLyricMatch, Page: p, Model: &a.local, CurrentIndex: a.views.tracks.CurrentIndex, SyncViewport: syncViewport, Locate: a.views.tracks.Locate, AddFolders: a.addLocalFolders, Refresh: a.refreshLocalSongs, OpenFolders: a.openLocalFolders, ChooseFolders: a.chooseLocalFolders, RemoveFolder: a.removeLocalFolder, CloseFolders: func() {
		if a.localView.window != nil {
			a.localView.window.Close()
		}
	}}
	a.views.podcast = desktopview.Podcast{Page: p, Model: &a.podcasts, Library: &a.library, Load: a.loadPodcast, PlayPodcast: a.playPodcast, LoadPrograms: a.loadPrograms, ToggleSubscribe: a.toggleSubscribe, PlayProgram: a.playProgram, LoadHome: a.loadPodcastTab, LoadHot: a.loadHotPodcasts, Open: a.open, Count: components.Count, Current: func() *netease.Track { return a.pl.Current() }, Playing: func() bool { return a.pl.Playing() }, PlayingBars: a.views.playback.NowPlayingBars, LikeButton: a.views.like.Button, DescriptionMore: a.intro.More}
}
