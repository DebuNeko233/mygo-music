package app

import (
	"netease-music-mygo/internal/diskcache"
	libraryvm "netease-music-mygo/internal/viewmodel/library"
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

func (a *app) persistLibrary(ids ...int64) {
	a.persistLibraryChanges(libraryvm.SnapshotLists|libraryvm.SnapshotAlbums|libraryvm.SnapshotLikes, ids...)
}

// Commit independent model snapshots using this account's cache epoch.
func (a *app) persistLibraryChanges(changed libraryvm.LibrarySnapshots, ids ...int64) {
	// An edit can temporarily pin cached playlists, even without a disk store.
	defer a.trimPlaylistCache()
	if a.browseCache == nil || a.account.Profile == nil {
		return
	}
	for _, snapshot := range a.library.TakeSnapshots(changed, ids...) {
		req := a.cacheRequest(snapshot.Key)
		if snapshot.Playlist {
			a.savePlaylistSnapshot(req, func() []byte { return diskcache.Encode(snapshot.Value) })
		} else if write := req.Store.Prepare(req.Scope, req.Key, req.Epoch); write != nil {
			go func() { write(diskcache.Encode(snapshot.Value)) }()
		}
	}
}
