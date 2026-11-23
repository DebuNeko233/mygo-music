package app

import (
	"netease-music-mygo/internal/viewmodel"
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
func (a *app) artistServices() viewmodel.ArtistServices {
	return viewmodel.ArtistServices{
		Post: a.detailPost(), Cache: a.cacheRequest, Detail: artistDetail, Latest: artistLatestSongs,
		Albums: albumsBy, Events: accountEvents, Follow: followArtist,
		Visible: func() int64 {
			if a.route.page == pageArtist {
				return a.route.id
			}
			return 0
		},
		Notify: func(message string) { a.toast = message },
	}
}
func (a *app) artist(id int64) *viewmodel.ArtistPage { return a.artists.Page(id) }
func (a *app) loadArtist(id int64) {
	if !a.artists.NeedsLoad(id) {
		a.artists.Pages.Get(id)
		return
	}
	a.artists.Load(id, a.artistServices())
}
func (a *app) loadArtistProfile(id int64) { a.artists.LoadProfile(id, a.artistServices()) }
func (a *app) loadLatest(id int64)        { a.artists.LoadLatest(id, a.artistServices()) }
func (a *app) loadAlbums(id int64)        { a.artists.LoadAlbums(id, a.artistServices()) }
func (a *app) loadEvents(id int64)        { a.artists.LoadEvents(id, a.artistServices()) }
func (a *app) toggleFollow(id int64) {
	d := a.artist(id)
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
	a.albums.Load(id, viewmodel.AlbumServices{Post: a.detailPost(), Cache: a.cacheRequest, Detail: albumDetail})
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
	a.users.Load(id, uid, viewmodel.UserServices{Post: a.detailPost(), Detail: userDetail, Playlists: userPlaylists, Artists: followedArtists})
}
