package app

import (
	"netease-music-mygo/internal/diskcache"
)

// Bound cached pages and their track records; the visible playlist may exceed this budget.
const (
	playlistCachePages  = pageLimit
	playlistCacheTracks = 20000
)

// A playlist can be evicted before its snapshot reaches disk. Keep the
// latest accepted write visible to readers even after eviction.
func (a *app) savePlaylistSnapshot(req diskcache.Snapshot, encode func() []byte) {
	write := req.Store.PrepareIfChanged(req.Scope, req.Key, req.Epoch)
	if write == nil {
		return
	}
	done := make(chan struct{})
	a.playlistWrites.Store(req, done)
	go func() {
		write(encode())
		a.playlistWrites.CompareAndDelete(req, done)
		close(done)
	}()
}

func (a *app) waitForPlaylistSnapshot(req diskcache.Snapshot) {
	for {
		done, pending := a.playlistWrites.Load(req)
		if !pending {
			return
		}
		<-done.(chan struct{})
	}
}

func (a *app) usePlaylist(id int64) {
	a.library.Refresh.PlaylistUse++
	a.playlistRequest(id).Used = a.library.Refresh.PlaylistUse
}

func (a *app) trimPlaylistCache() {
	if a.library.Refresh.Edits != 0 {
		return
	}
	weight := func(id int64) int {
		p := a.library.Playlists[id]
		return max(len(p.Tracks), len(p.TrackIDs))
	}
	tracks := 0
	for id := range a.library.Playlists {
		tracks += weight(id)
	}
	for len(a.library.Playlists) > playlistCachePages || tracks > playlistCacheTracks {
		var oldest int64
		var used uint64
		found := false
		for id := range a.library.Playlists {
			_, editing := a.library.Refresh.EditedLists[id]
			if (a.route.page == pagePlaylist && a.route.id == id) || a.library.PlayWhenLoaded[id] || a.library.DirtySnapshots[id] || editing {
				continue
			}
			r := a.library.Refresh.Playlists[id]
			var last uint64
			if r != nil {
				if r.Busy {
					continue
				}
				last = r.Used
			}
			if !found || last < used {
				oldest, used, found = id, last, true
			}
		}
		if !found {
			return
		}
		tracks -= weight(oldest)
		// Unlike forgetPlaylist, eviction must keep the offline snapshot.
		delete(a.library.Playlists, oldest)
		delete(a.library.Refresh.Playlists, oldest)
		delete(a.library.Errors, oldest)
	}
}
