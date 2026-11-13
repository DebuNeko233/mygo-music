package app

import (
	"errors"
	"fmt"
	"testing"
	"testing/synctest"

	"netease-music-mygo/internal/diskcache"
	"netease-music-mygo/internal/netease"
)

// Reopening an evicted playlist before its accepted write finishes must
// wait for that snapshot instead of treating an offline response as empty.
func TestPlaylistCacheWaitsForPendingOfflineSnapshot(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		a, posts, _ := refreshFixture(t)
		a.browseCache = diskcache.New(t.TempDir(), 64<<20)
		t.Cleanup(a.browseCache.Close)
		snapshot := netease.Playlist{ID: 1, Name: "Newest"}
		req := a.cacheRequest("playlist/1")
		req.store.Put(req.scope, req.key, cacheBytes(netease.Playlist{ID: 1, Name: "Older"}), req.epoch)
		unblock := make(chan struct{})
		defer close(unblock)
		a.savePlaylistSnapshot(req, func() []byte {
			<-unblock
			return cacheBytes(snapshot)
		})
		for id := int64(1); id <= 3; id++ {
			a.playlists[id] = &netease.Playlist{ID: id, Tracks: make([]netease.Track, 8000)}
			a.usePlaylist(id)
		}
		a.trimPlaylistCache()
		if a.playlists[1] != nil {
			t.Fatal("test playlist was not evicted")
		}
		playlistDetail = func(int64) (*netease.Playlist, error) { return nil, netease.ErrOffline }
		a.show(route{page: pagePlaylist, id: 1})
		synctest.Wait()
		if len(posts) != 0 {
			t.Fatal("load finished before the accepted snapshot was written")
		}
		unblock <- struct{}{}
		refreshPost(t, posts)
		refreshPost(t, posts)
		if pl := a.playlists[1]; pl == nil || pl.Name != snapshot.Name {
			t.Fatal("offline return did not restore the latest accepted snapshot")
		}
	})
}

// Visiting several long playlists evicts old details from memory but
// returning offline still reads the same tracks from the disk snapshot.
func TestPlaylistCacheKeepsOfflineSnapshots(t *testing.T) {
	a, posts, _ := refreshFixture(t)
	dir := t.TempDir()
	a.browseCache = diskcache.New(dir, 64<<20)
	t.Cleanup(func() { a.browseCache.Close() })
	playlistDetail = func(id int64) (*netease.Playlist, error) {
		pl := &netease.Playlist{ID: id, Tracks: make([]netease.Track, 8000)}
		pl.Tracks[0] = netease.Track{ID: id * 10, Name: fmt.Sprintf("Song %d", id)}
		return pl, nil
	}
	for id := int64(1); id <= 3; id++ {
		a.show(route{page: pagePlaylist, id: id})
		refreshPost(t, posts)
	}
	if a.playlists[1] != nil || a.playlists[2] == nil || a.playlists[3] == nil {
		t.Fatal("track budget did not evict the least recently visited playlist")
	}
	a.browseCache.Close() // drain accepted snapshots before simulating an offline return
	a.browseCache = diskcache.New(dir, 64<<20)
	playlistDetail = func(int64) (*netease.Playlist, error) { return nil, errors.New("offline") }
	a.show(route{page: pagePlaylist, id: 1})
	refreshPost(t, posts)
	refreshPost(t, posts)
	if pl := a.playlists[1]; pl == nil || len(pl.Tracks) != 8000 || pl.Tracks[0].ID != 10 {
		t.Fatal("evicted playlist was not restored from its offline snapshot")
	}
	if a.playlists[2] != nil || a.playlists[3] == nil {
		t.Fatal("offline return did not apply the same retention budget")
	}
}

// The page limit must make room without evicting a visible list, a list
// with a pending reply, or a modified list whose snapshot is not yet saved.
func TestPlaylistCacheProtectsActiveEntries(t *testing.T) {
	a := newTestApp()
	a.route = route{page: pagePlaylist, id: 1}
	for id := int64(1); id <= playlistCachePages+1; id++ {
		a.playlists[id] = &netease.Playlist{ID: id}
		a.usePlaylist(id)
	}
	a.playlistRequest(2).busy = true
	a.dirtySnapshots = map[int64]bool{3: true}
	a.usePlaylist(4)
	a.trimPlaylistCache()
	if len(a.playlists) != playlistCachePages || a.playlists[5] != nil {
		t.Fatal("eviction did not pick the oldest eligible entry")
	}
	for _, id := range []int64{1, 2, 3, 4} {
		if a.playlists[id] == nil {
			t.Fatalf("lost active or recently accessed entry %d", id)
		}
	}
}

// Finishing an edit must resume eviction even if disk caching is disabled.
func TestPlaylistCacheTrimsAfterEditWithoutDisk(t *testing.T) {
	a := newTestApp()
	finish := a.beginLibraryEdit()
	for id := int64(1); id <= playlistCachePages+1; id++ {
		a.playlists[id] = &netease.Playlist{ID: id}
		a.usePlaylist(id)
	}
	a.trimPlaylistCache()
	if a.playlists[1] == nil {
		t.Fatal("evicted a playlist during the edit")
	}
	finish()
	a.persistLibrary(1)
	if len(a.playlists) != playlistCachePages || a.playlists[1] != nil {
		t.Fatal("finished edit left the cache above its page limit")
	}
}
