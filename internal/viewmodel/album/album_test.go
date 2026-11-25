/*
验证离线专辑快照保留歌曲，并在模型重置后拒绝旧磁盘与网络结果。
*/

package album

import (
	"testing"

	"netease-music-mygo/internal/diskcache"
	"netease-music-mygo/internal/netease"
	"netease-music-mygo/internal/viewmodel/internal/testutil"
)

// Album snapshots keep their songs while the network fails, despite AlbumProfile omitting songs from JSON.
func TestAlbumDetailsRestoreSongsBeforeOfflineFailure(t *testing.T) {
	var m Albums
	m.Reset()
	posts := make(chan func(), 4)
	store := diskcache.New(t.TempDir(), 1<<20)
	defer store.Close()
	req := diskcache.Snapshot{Store: store, Scope: "7", Key: "album/8", Epoch: store.Epoch()}
	original := &netease.AlbumProfile{Album: netease.Album{ID: 8, Name: "saved"}, Songs: []netease.Track{{ID: 1}, {ID: 2}}}
	store.Put(req.Scope, req.Key, diskcache.Encode(KeptAlbum{original, original.Songs}), req.Epoch)
	s := AlbumServices{Post: func(update func()) { posts <- update }, Cache: func(string) diskcache.Snapshot { return req }, Detail: func(int64) (*netease.AlbumProfile, error) { return nil, netease.ErrOffline }}
	m.Load(8, s)
	cached, failed := testutil.Wait(t, posts, "cached album"), testutil.Wait(t, posts, "album failure")
	if m.Pages.Peek(8).Album != nil {
		t.Fatal("worker published the snapshot itself")
	}
	cached()
	failed()
	d := m.Pages.Peek(8)
	if d.Album == nil || len(d.Album.Songs) != 2 || d.Album.Songs[1].ID != 2 || d.Loading || d.Failure != netease.ErrOffline {
		t.Fatal("offline response lost restored songs or pending state")
	}
}

// Resetting an album cache retires disk and network results before they can finish the new request.
func TestAlbumDetailsResetRejectsBothReplies(t *testing.T) {
	var m Albums
	m.Reset()
	posts := make(chan func(), 4)
	store := diskcache.New(t.TempDir(), 1<<20)
	defer store.Close()
	req := diskcache.Snapshot{Store: store, Key: "album/8", Epoch: store.Epoch()}
	store.Put("", req.Key, diskcache.Encode(KeptAlbum{Album: &netease.AlbumProfile{Album: netease.Album{ID: 8}}}), req.Epoch)
	s := AlbumServices{Post: func(update func()) { posts <- update }, Cache: func(string) diskcache.Snapshot { return req }, Detail: func(int64) (*netease.AlbumProfile, error) {
		return &netease.AlbumProfile{Album: netease.Album{ID: 8}}, nil
	}}
	m.Load(8, s)
	cached, fresh := testutil.Wait(t, posts, "old cache"), testutil.Wait(t, posts, "old network")
	m.Reset()
	current := &AlbumPage{Loading: true}
	m.Pages.Set(8, current)
	cached()
	fresh()
	if !current.Loading || current.Album != nil || m.Pages.Peek(8) != current {
		t.Fatal("retired album result changed its replacement")
	}
}
