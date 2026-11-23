package app

import (
	"netease-music-mygo/internal/diskcache"
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

func (a *app) trimPlaylistCache() { a.library.TrimPlaylists(a.visiblePlaylist()) }
