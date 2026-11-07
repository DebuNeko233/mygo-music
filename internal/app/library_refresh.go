package app

import (
	"maps"
	"reflect"
	"slices"
	"time"

	"netease-music-mygo/internal/netease"
)

// Event-driven refresh, not a periodic poll: returning to the front or opening
// a list checks once per interval, including after a failed request.
const listsFresh = 30 * time.Second

var likedIDs = netease.LikedIDs

type listRequest struct {
	at    time.Time
	busy  bool
	retry bool
}
type libraryRefreshState struct {
	edits       int
	revision    uint64
	playlists   map[int64]*listRequest
	mine, likes listRequest
	now         func() time.Time // optional test clock; never a UI context
}

// beginLibraryEdit makes refreshes respect edits that are already pending,
// not only edits made after their snapshot. The ending revision rejects a
// server reply fetched before a rename/delete/like had finished committing.
func (a *app) beginLibraryEdit() func() {
	gen := a.homeGen
	a.listRefresh.edits++
	a.listRefresh.revision++
	return func() {
		if a.homeGen == gen {
			a.listRefresh.edits--
			a.listRefresh.revision++
		}
	}
}

func (a *app) listsNow() time.Time {
	if a.listRefresh.now != nil {
		return a.listRefresh.now()
	}
	return time.Now()
}
func listDue(at, now time.Time) bool { return at.IsZero() || now.Sub(at) >= listsFresh }
func (a *app) listenerID() int64 {
	if a.profile != nil {
		return a.profile.UserID
	}
	return 0
}
func (a *app) resetLibraryFreshness() {
	a.listRefresh = libraryRefreshState{now: a.listRefresh.now}
	a.loadingPl = map[int64]bool{}
	a.plPlay = map[int64]bool{}
}
func (a *app) playlistRequest(id int64) *listRequest {
	if a.listRefresh.playlists == nil {
		a.listRefresh.playlists = map[int64]*listRequest{}
	}
	r := a.listRefresh.playlists[id]
	if r == nil {
		r = &listRequest{}
		a.listRefresh.playlists[id] = r
	}
	return r
}

// forgetPlaylist invalidates the pending request as well as its cached data;
// an older reply cannot restore a deleted list or clear a newer load's flags.
func (a *app) forgetPlaylist(id int64) {
	delete(a.playlists, id)
	delete(a.listRefresh.playlists, id)
	delete(a.loadingPl, id)
	delete(a.plPlay, id)
}
func (a *app) touchPlaylist(id int64) { a.playlistRequest(id).at = a.listsNow() }
func copyPlaylist(pl netease.Playlist) netease.Playlist {
	pl.Tracks = slices.Clone(pl.Tracks)
	pl.TrackIDs = slices.Clone(pl.TrackIDs)
	return pl
}
func copyPlaylists(pl []netease.Playlist) []netease.Playlist {
	if pl == nil {
		return nil
	}
	result := make([]netease.Playlist, len(pl))
	for i := range pl {
		result[i] = copyPlaylist(pl[i])
	}
	return result
}

// loadPlaylist shows a cached list while an expired copy is fetched anew.
// A play request takes the displayed queue immediately; refreshing changes
// the page alone, never the queue currently playing.
func (a *app) loadPlaylist(id int64, thenPlay bool) {
	was := a.playlists[id]
	if was != nil && thenPlay {
		a.pl.PlayQueue(was.Tracks, firstPlayable(was.Tracks))
	}
	r := a.playlistRequest(id)
	if r.busy {
		if was == nil && thenPlay {
			a.plPlay[id] = true
		}
		return
	}
	if was != nil && (a.listRefresh.edits != 0 || !listDue(r.at, a.listsNow())) {
		return
	}
	r.at, r.busy, r.retry = a.listsNow(), true, false
	if was == nil {
		a.loadingPl[id] = true
		delete(a.plErr, id)
	}
	var before netease.Playlist
	if was != nil {
		before = copyPlaylist(*was)
	}
	uid, gen, revision := a.listenerID(), a.homeGen, a.listRefresh.revision
	go func() {
		pl, err := playlistDetail(id)
		a.post(func() {
			if a.listenerID() != uid || a.homeGen != gen || a.listRefresh.playlists[id] != r {
				return
			}
			r.busy = false
			play := thenPlay || a.plPlay[id]
			delete(a.loadingPl, id)
			delete(a.plPlay, id)
			if err != nil || pl == nil {
				if was == nil {
					a.plErr[id] = "歌单加载失败"
				}
				return // a refresh failure leaves displayed content intact
			}
			if was != nil {
				// A local rename, subscription, addition/removal or a replacement wins.
				if a.listRefresh.edits != 0 || a.listRefresh.revision != revision || a.playlists[id] != was || !reflect.DeepEqual(*was, before) {
					r.stale()
					return
				}
			} else if a.playlists[id] != nil {
				return
			}
			a.playlists[id] = pl
			if was == nil && play {
				a.pl.PlayQueue(pl.Tracks, firstPlayable(pl.Tracks))
			}
		})
	}()
}

// loadMine keeps the sidebar visible during refresh and refuses a response
// that belongs to another account, or predates a local sidebar edit.
func (a *app) loadMine(uid int64) {
	if a.profile == nil || a.profile.UserID != uid || a.listRefresh.mine.busy {
		return
	}
	r := &a.listRefresh.mine
	r.at, r.busy, r.retry = a.listsNow(), true, false
	gen, revision := a.homeGen, a.listRefresh.revision
	beforeMine, beforeSaved := copyPlaylists(a.mine), copyPlaylists(a.saved)
	go func() {
		made, saved, err := userPlaylists(uid)
		a.post(func() {
			if a.listenerID() != uid || a.homeGen != gen {
				return
			}
			r.busy = false
			if err != nil {
				return
			}
			if a.listRefresh.edits != 0 || a.listRefresh.revision != revision || !reflect.DeepEqual(a.mine, beforeMine) || !reflect.DeepEqual(a.saved, beforeSaved) {
				r.stale()
				return
			}
			a.mine, a.saved = made, saved
		})
	}()
}

// loadLiked refreshes the actual liked IDs, not just the songs currently
// visible. An optimistic like/unlike made during the request takes priority.
func (a *app) loadLiked(uid int64) {
	if a.profile == nil || a.profile.UserID != uid || a.listRefresh.likes.busy {
		return
	}
	r := &a.listRefresh.likes
	r.at, r.busy, r.retry = a.listsNow(), true, false
	gen, revision := a.homeGen, a.listRefresh.revision
	before := maps.Clone(a.liked)
	go func() {
		ids, err := likedIDs(uid)
		a.post(func() {
			if a.listenerID() != uid || a.homeGen != gen {
				return
			}
			r.busy = false
			if err != nil {
				return
			}
			if a.listRefresh.edits != 0 || a.listRefresh.revision != revision || !maps.Equal(a.liked, before) {
				r.stale()
				return
			}
			a.liked = make(map[int64]bool, len(ids))
			for _, id := range ids {
				a.liked[id] = true
			}
		})
	}()
}
func (a *app) freshenLikes() {
	if a.profile != nil && a.listRefresh.edits == 0 && listDue(a.listRefresh.likes.at, a.listsNow()) {
		a.loadLiked(a.profile.UserID)
	}
}
func (a *app) freshenLibrary() {
	if a.profile != nil && a.listRefresh.edits == 0 {
		if listDue(a.listRefresh.mine.at, a.listsNow()) {
			a.loadMine(a.profile.UserID)
		}
		a.freshenLikes()
	}
	if a.route.page == pagePlaylist {
		a.loadPlaylist(a.route.id, false)
	}
}

// stale keeps a discarded success due for refresh instead of treating data
// we never accepted as fresh. Network failures retain their retry interval.
func (r *listRequest) stale() {
	r.at, r.retry = time.Time{}, true
}

// retryLibraryRefresh runs on the next view after post, including the post
// that finishes an edit. Each load clears its flag before starting, so extra
// build passes coalesce; a failed follow-up does not start a retry loop.
func (a *app) retryLibraryRefresh() {
	if a.listRefresh.edits != 0 {
		return
	}
	if a.profile != nil {
		if a.listRefresh.mine.retry {
			a.loadMine(a.profile.UserID)
		}
		if a.listRefresh.likes.retry {
			a.loadLiked(a.profile.UserID)
		}
	}
	if a.route.page == pagePlaylist {
		if r := a.listRefresh.playlists[a.route.id]; r != nil && r.retry {
			a.loadPlaylist(a.route.id, false)
		}
	}
}
