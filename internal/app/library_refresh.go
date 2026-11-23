package app

import (
	"fmt"
	"time"

	"netease-music-mygo/internal/netease"
	"netease-music-mygo/internal/player"
	"netease-music-mygo/internal/viewmodel"
)

var (
	likedIDs         = netease.LikedIDs
	subscribedAlbums = netease.SubscribedAlbums
)

func (a *app) listenerID() int64 {
	if a.account.Profile != nil {
		return a.account.Profile.UserID
	}
	return 0
}

func (a *app) visiblePlaylist() int64 {
	if a.nav.Current.Page == pagePlaylist {
		return a.nav.Current.ID
	}
	return 0
}

func (a *app) playlistServices() viewmodel.PlaylistServices {
	uid, generation := a.listenerID(), a.account.Generation
	return viewmodel.PlaylistServices{
		Post: func(update func() bool) {
			a.postIfChanged(func() bool {
				if a.listenerID() != uid || a.account.Generation != generation {
					return false
				}
				return update()
			})
		},
		Cache: a.cacheRequest, Detail: playlistDetail, WaitSnapshot: a.waitForPlaylistSnapshot, SaveSnapshot: a.savePlaylistSnapshot,
		Play: func(pl *netease.Playlist) {
			a.pl.PlayQueueFrom(pl.Tracks, firstPlayable(pl.Tracks), player.PlaySource{Source: "list", ID: pl.ID})
		},
		VisiblePlaylist: a.visiblePlaylist, Recovering: func() bool { return a.network.recovering },
	}
}

func (a *app) forgetPlaylistSnapshot(id int64) {
	req := a.cacheRequest(fmt.Sprintf("playlist/%d", id))
	req.Store.Forget(req.Scope, req.Key)
}

func (a *app) loadPlaylist(id int64, thenPlay bool) {
	if !thenPlay && !a.library.NeedsPlaylistLoad(id) {
		a.library.UsePlaylist(id)
		return
	}
	a.library.LoadPlaylist(id, thenPlay, a.playlistServices())
}

func (a *app) freshenLikes() {
	if a.library.LikesDue(a.listenerID()) {
		a.library.FreshenLikes(a.listenerID(), a.libraryServices())
	}
}
func (a *app) freshenAlbums() {
	if a.library.AlbumsDue(a.listenerID()) {
		a.library.FreshenAlbums(a.listenerID(), a.libraryServices())
	}
}
func (a *app) freshenLibrary() {
	a.library.Freshen(a.listenerID(), a.visiblePlaylist(), a.libraryServices())
}
func (a *app) retryLibraryRefresh() {
	if a.library.NeedsRefreshRetry(a.listenerID(), a.visiblePlaylist()) {
		a.library.RetryRefresh(a.listenerID(), a.visiblePlaylist(), a.libraryServices())
	}
}

// loadMine keeps the sidebar visible during refresh and refuses a response
// that belongs to another account, or predates a local sidebar edit.
func (a *app) loadMine(uid int64) {
	if a.account.Profile != nil && a.listenerID() == uid {
		a.library.LoadMine(uid, a.libraryServices())
	}
}

// loadLiked refreshes the actual liked IDs, not just the songs currently
// visible. An optimistic like/unlike made during the request takes priority.
func (a *app) loadLiked(uid int64) {
	if a.account.Profile != nil && a.listenerID() == uid {
		a.library.LoadLiked(uid, a.libraryServices())
	}
}

// loadSubscribedAlbums refreshes the albums the listener saved. An
// optimistic save or unsave made during the request takes priority.
func (a *app) loadSubscribedAlbums(uid int64) {
	if a.account.Profile != nil && a.listenerID() == uid {
		a.library.LoadAlbums(uid, a.libraryServices())
	}
}

func (a *app) listsNow() time.Time { return a.library.Now() }
