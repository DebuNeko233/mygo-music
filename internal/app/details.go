package app

import (
	"netease-music-mygo/internal/netease"
	albumvm "netease-music-mygo/internal/viewmodel/album"
	artistvm "netease-music-mygo/internal/viewmodel/artist"
	uservm "netease-music-mygo/internal/viewmodel/user"
)

// detailPost publishes only into the account session that started the work.
func (a *app) detailPost() func(func()) {
	uid, generation := a.listenerID(), a.account.Generation
	return func(update func()) {
		a.post(func() {
			if a.listenerID() == uid && a.account.Generation == generation {
				update()
			}
		})
	}
}

func (a *app) artistServices() artistvm.ArtistServices {
	return artistvm.ArtistServices{
		Post: a.detailPost(), Cache: a.cacheRequest, Detail: artistDetail, Latest: artistLatestSongs,
		Albums: albumsBy, Events: accountEvents, Follow: followArtist,
		Visible: func() int64 {
			if a.nav.Current.Page == pageArtist {
				return a.nav.Current.ID
			}
			return 0
		},
		Notify: func(message string) { a.toast = message },
	}
}

func (a *app) loadArtist(id int64) {
	if !a.artists.NeedsLoad(id) {
		a.artists.Pages.Get(id)
		return
	}
	a.artists.Load(id, a.artistServices())
}

func (a *app) loadArtistProfile(id int64) { a.artists.LoadProfile(id, a.artistServices()) }

func (a *app) loadLatest(id int64) { a.artists.LoadLatest(id, a.artistServices()) }

func (a *app) loadAlbums(id int64) { a.artists.LoadAlbums(id, a.artistServices()) }

func (a *app) loadEvents(id int64) { a.artists.LoadEvents(id, a.artistServices()) }

func (a *app) toggleFollow(id int64) {
	d := a.artists.Page(id)
	if d.Profile == nil || d.Following {
		return
	}
	if a.account.Profile == nil {
		a.openLogin()
		return
	}
	a.artists.ToggleFollow(id, a.artistServices())
}

func (a *app) loadAlbum(id int64) {
	if !a.albums.NeedsLoad(id) {
		a.albums.Pages.Get(id)
		return
	}
	a.albums.Load(id, albumvm.AlbumServices{Post: a.detailPost(), Cache: a.cacheRequest, Detail: albumDetail})
}

func (a *app) loadUser(id int64) {
	uid := a.listenerID()
	if uid != 0 && uid == id {
		a.freshenAlbums()
		a.podcasts.LoadSubscriptions(a.podcastServices())
	}
	if !a.users.NeedsLoad(id, uid) {
		a.users.Pages.Get(id)
		return
	}
	a.users.Load(id, uid, uservm.UserServices{Post: a.detailPost(), Detail: userDetail, Playlists: userPlaylists, Artists: followedArtists})
}

// albumDetail loads an album from NetEase; tests replace it.
var (
	albumDetail = netease.AlbumDetail
)

// userDetail and userPlaylists load a user and their playlists from
// NetEase, and followedArtists and subscribedPodcasts what the listener
// follows; tests replace them.
var (
	userDetail         = netease.UserDetail
	userPlaylists      = netease.UserPlaylists
	followedArtists    = netease.FollowedArtists
	subscribedPodcasts = netease.SubscribedPodcasts
)

// openPage opens a page in the main window, from wherever a link to it
// shows: over Now Playing, which closes, or the notch.
func (a *app) openPage(r route) {
	a.open(r)
	a.showMain()
	a.redraw()
}

// openArtist opens an artist's page in the main window.
func (a *app) openArtist(id int64) { a.openPage(route{Page: pageArtist, ID: id}) }

// These load an artist, their songs, albums and events from NetEase, and
// follow them; tests replace them.
var (
	artistDetail      = netease.ArtistDetail
	artistLatestSongs = netease.ArtistLatestSongs
	albumsBy          = netease.ArtistAlbums
	accountEvents     = netease.Events
	followArtist      = netease.FollowArtist
)
