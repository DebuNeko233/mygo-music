package app

import (
	"fmt"
	"reflect"
	"slices"
	"time"

	"netease-music-mygo/internal/netease"
	"netease-music-mygo/internal/player"
	"netease-music-mygo/internal/viewmodel"
)

// Opening a list or returning to the foreground revalidates it once per interval.
const listsFresh = 30 * time.Second

var (
	likedIDs         = netease.LikedIDs
	subscribedAlbums = netease.SubscribedAlbums
)

func listDue(at, now time.Time) bool { return at.IsZero() || now.Sub(at) >= listsFresh }
func (a *app) listenerID() int64 {
	if a.account.Profile != nil {
		return a.account.Profile.UserID
	}
	return 0
}
func (a *app) playlistRequest(id int64) *viewmodel.ListRequest {
	if a.library.Refresh.Playlists == nil {
		a.library.Refresh.Playlists = map[int64]*viewmodel.ListRequest{}
	}
	r := a.library.Refresh.Playlists[id]
	if r == nil {
		r = &viewmodel.ListRequest{}
		a.library.Refresh.Playlists[id] = r
	}
	return r
}

// forgetPlaylist invalidates the pending request as well as its cached data;
// an older reply cannot restore a deleted list or clear a newer load's flags.
func (a *app) forgetPlaylist(id int64) {
	req := a.cacheRequest(fmt.Sprintf("playlist/%d", id))
	req.Store.Forget(req.Scope, req.Key)
	delete(a.library.Playlists, id)
	delete(a.library.Refresh.Playlists, id)
	delete(a.library.Refresh.EditedLists, id)
	delete(a.library.Loading, id)
	delete(a.library.PlayWhenLoaded, id)
}
func (a *app) touchPlaylist(id int64) {
	a.usePlaylist(id)
	a.playlistRequest(id).At = a.listsNow()
}

// revalidatePlaylist makes an edited list due without discarding its snapshot.
func (a *app) revalidatePlaylist(id int64) {
	r := a.playlistRequest(id)
	r.Stale()
	if a.library.Refresh.EditedLists == nil {
		a.library.Refresh.EditedLists = map[int64]struct{}{}
	}
	a.library.Refresh.EditedLists[id] = struct{}{}
	if !r.Busy && a.library.Refresh.Edits == 0 {
		a.loadPlaylist(id, false)
	}
}

// loadPlaylist shows a cached list while an expired copy is fetched anew.
// A play request takes the displayed queue immediately; refreshing changes
// the page alone, never the queue currently playing.
func (a *app) loadPlaylist(id int64, thenPlay bool) {
	a.usePlaylist(id)
	was := a.library.Playlists[id]
	if was != nil && thenPlay {
		a.pl.PlayQueueFrom(was.Tracks, firstPlayable(was.Tracks), player.PlaySource{Source: "list", ID: id})
	}
	r := a.playlistRequest(id)
	if r.Busy {
		if was == nil && thenPlay {
			a.library.PlayWhenLoaded[id] = true
		}
		return
	}
	if was != nil && (a.library.Refresh.Edits != 0 || !listDue(r.At, a.listsNow())) {
		return
	}
	r.At, r.Busy, r.Retry = a.listsNow(), true, false
	r.Failed, r.Failure = false, nil
	if was == nil {
		a.library.Loading[id] = true
	}
	var before *netease.Playlist
	if was != nil {
		snapshot := copyPlaylist(*was)
		before = &snapshot
	}
	uid, gen, revision := a.listenerID(), a.account.Generation, a.library.Refresh.Revision
	req := a.cacheRequest(fmt.Sprintf("playlist/%d", id))
	readDisk := was == nil
	fetch := playlistDetail
	go func(baseline *netease.Playlist) {
		if readDisk {
			a.waitForPlaylistSnapshot(req)
			var kept *netease.Playlist
			if at := req.Read(&kept); !at.IsZero() && kept != nil && kept.ID == id {
				snapshot := copyPlaylist(*kept)
				baseline = &snapshot
				a.post(func() {
					if a.account.Generation == gen && a.library.Refresh.Playlists[id] == r && a.library.Playlists[id] == nil && a.library.Refresh.Edits == 0 && a.library.Refresh.Revision == revision {
						a.library.Playlists[id] = kept
						was = kept
						before = &snapshot
						delete(a.library.Loading, id)
						if thenPlay || a.library.PlayWhenLoaded[id] {
							a.pl.PlayQueueFrom(kept.Tracks, firstPlayable(kept.Tracks), player.PlaySource{Source: "list", ID: id})
							thenPlay = false
							delete(a.library.PlayWhenLoaded, id)
						}
					}
				})
			}
		}
		pl, err := fetch(id)
		var sameTracks, unchanged bool
		if err == nil && pl != nil && baseline != nil {
			sameTracks = len(baseline.Tracks) == 0 && len(pl.Tracks) == 0 || reflect.DeepEqual(baseline.Tracks, pl.Tracks)
			if sameTracks && slices.Equal(baseline.TrackIDs, pl.TrackIDs) {
				old, fresh := *baseline, *pl
				old.Tracks, fresh.Tracks = nil, nil
				old.TrackIDs, fresh.TrackIDs = nil, nil
				unchanged = reflect.DeepEqual(old, fresh)
			}
		}
		var bytes []byte
		if err == nil && pl != nil && req.Store != nil && !(readDisk && unchanged) {
			bytes = cacheBytes(pl)
		}
		a.postIfChanged(func() bool {
			if a.listenerID() != uid || a.account.Generation != gen || a.library.Refresh.Playlists[id] != r {
				return false
			}
			r.Busy = false
			defer a.trimPlaylistCache()
			play := thenPlay || a.library.PlayWhenLoaded[id]
			delete(a.library.Loading, id)
			delete(a.library.PlayWhenLoaded, id)
			if err != nil || pl == nil {
				if !r.Retry {
					delete(a.library.Refresh.EditedLists, id)
				}
				r.Failed, r.Failure = true, err
				if was == nil {
					a.library.Errors[id] = "歌单加载失败"
				}
				return true // the error or network recovery can change the UI
			}
			if a.library.Refresh.Edits != 0 || a.library.Refresh.Revision != revision {
				if was == nil && play {
					a.library.PlayWhenLoaded[id] = true
				}
				r.Stale()
				return true
			}
			if was != nil {
				// A local rename, subscription, addition/removal or a replacement wins.
				if a.library.Playlists[id] != was || !reflect.DeepEqual(*was, *before) {
					r.Stale()
					return true
				}
			} else if a.library.Playlists[id] != nil {
				delete(a.library.Refresh.EditedLists, id)
				return false
			}
			delete(a.library.Refresh.EditedLists, id)
			if r.Retry {
				r.At, r.Retry = a.listsNow(), false
			}
			delete(a.library.Errors, id)
			if bytes != nil {
				a.savePlaylistSnapshot(req, func() []byte { return bytes })
			}
			if was != nil && unchanged {
				return a.network.recovering && a.route.page == pagePlaylist && a.route.id == id
			}
			if was != nil && sameTracks {
				pl.Tracks = was.Tracks
			}
			a.library.Playlists[id] = pl
			if was == nil && play {
				a.pl.PlayQueueFrom(pl.Tracks, firstPlayable(pl.Tracks), player.PlaySource{Source: "list", ID: id})
			}
			return true
		})
	}(before)
}

// loadMine keeps the sidebar visible during refresh and refuses a response
// that belongs to another account, or predates a local sidebar edit.
// loadLiked refreshes the actual liked IDs, not just the songs currently
// visible. An optimistic like/unlike made during the request takes priority.
// loadSubscribedAlbums refreshes the albums the listener saved. An
// optimistic save or unsave made during the request takes priority.
func (a *app) freshenLikes() {
	if a.account.Profile != nil && a.library.Refresh.Edits == 0 && listDue(a.library.Refresh.Likes.At, a.listsNow()) {
		a.loadLiked(a.account.Profile.UserID)
	}
}
func (a *app) freshenAlbums() {
	if a.account.Profile != nil && a.library.Refresh.Edits == 0 && listDue(a.library.Refresh.Albums.At, a.listsNow()) {
		a.loadSubscribedAlbums(a.account.Profile.UserID)
	}
}
func (a *app) freshenLibrary() {
	if a.account.Profile != nil && a.library.Refresh.Edits == 0 {
		if listDue(a.library.Refresh.Mine.At, a.listsNow()) {
			a.loadMine(a.account.Profile.UserID)
		}
		a.freshenLikes()
		a.freshenAlbums()
	}
	if a.route.page == pagePlaylist {
		a.loadPlaylist(a.route.id, false)
	}
}

// retryLibraryRefresh runs on the next view after post, including the post
// that finishes an edit. Each load clears its flag before starting, so extra
// build passes coalesce; a failed follow-up does not start a retry loop.
func (a *app) retryLibraryRefresh() {
	if a.library.Refresh.Edits != 0 {
		return
	}
	if a.account.Profile != nil {
		if a.library.Refresh.Mine.Retry {
			a.loadMine(a.account.Profile.UserID)
		}
		if a.library.Refresh.Likes.Retry {
			a.loadLiked(a.account.Profile.UserID)
		}
		if a.library.Refresh.Albums.Retry {
			a.loadSubscribedAlbums(a.account.Profile.UserID)
		}
	}
	for id := range a.library.Refresh.EditedLists {
		if !a.library.Refresh.Playlists[id].Busy {
			a.loadPlaylist(id, false)
		}
	}
	if a.route.page == pagePlaylist {
		if r := a.library.Refresh.Playlists[a.route.id]; r != nil && r.Retry && !r.Busy {
			a.loadPlaylist(a.route.id, false)
		}
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

// beginLibraryEdit makes refreshes respect edits that are already pending,
// not only edits made after their snapshot. The ending revision rejects a
// server reply fetched before a rename/delete/like had finished committing.
func (a *app) beginLibraryEdit() func() {
	generation := a.account.Generation
	finish := a.library.BeginEdit()
	return func() {
		if a.account.Generation == generation {
			finish()
		}
	}
}
func (a *app) listsNow() time.Time { return a.library.Now() }
