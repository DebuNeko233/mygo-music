package app

// Bound both the number of cached playlists and their track records. The
// visible playlist may exceed the budget; offscreen details can be read
// back from the existing disk snapshot.
const (
	playlistCachePages  = pageLimit
	playlistCacheTracks = 20000
)

// A playlist can be evicted before its snapshot reaches disk. Keep the
// latest accepted write visible to readers even after eviction.
func (a *app) savePlaylistSnapshot(req cacheRequest, encode func() []byte) {
	write := req.store.Prepare(req.scope, req.key, req.epoch)
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

func (a *app) waitForPlaylistSnapshot(req cacheRequest) {
	for {
		done, pending := a.playlistWrites.Load(req)
		if !pending {
			return
		}
		<-done.(chan struct{})
	}
}

func (a *app) usePlaylist(id int64) {
	a.listRefresh.playlistUse++
	a.playlistRequest(id).used = a.listRefresh.playlistUse
}

func (a *app) trimPlaylistCache() {
	if a.listRefresh.edits != 0 {
		return
	}
	weight := func(id int64) int {
		p := a.playlists[id]
		return max(len(p.Tracks), len(p.TrackIDs))
	}
	tracks := 0
	for id := range a.playlists {
		tracks += weight(id)
	}
	for len(a.playlists) > playlistCachePages || tracks > playlistCacheTracks {
		var oldest int64
		var used uint64
		found := false
		for id := range a.playlists {
			if (a.route.page == pagePlaylist && a.route.id == id) || a.plPlay[id] || a.dirtySnapshots[id] {
				continue
			}
			r := a.listRefresh.playlists[id]
			var last uint64
			if r != nil {
				if r.busy {
					continue
				}
				last = r.used
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
		delete(a.playlists, oldest)
		delete(a.listRefresh.playlists, oldest)
		delete(a.plErr, oldest)
	}
}
