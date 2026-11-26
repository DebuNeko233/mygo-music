/*
验证集合编辑不修改借出的快照，且收藏失败回滚恢复原有顺序。
*/

package library

import (
	"testing"

	"netease-music-mygo/internal/features/netease"
)

// Sidebar changes and playlist removals preserve the old slices retained by playback and transitions.
func TestCollectionEditsPreserveBorrowedSnapshots(t *testing.T) {
	original := &netease.Playlist{ID: 7, TrackCount: 3, Tracks: []netease.Track{{ID: 1, Name: "one"}, {ID: 2}, {ID: 3}}, TrackIDs: []netease.TrackID{{ID: 1}, {ID: 2}, {ID: 3}}}
	next := WithoutSongs(original, func(id int64) bool { return id == 1 || id == 3 })
	if len(original.Tracks) != 3 || len(original.TrackIDs) != 3 || original.TrackCount != 3 || len(next.Tracks) != 1 || next.Tracks[0].ID != 2 || next.TrackCount != 1 {
		t.Fatal("removal changed the borrowed queue or lost its original order")
	}
	state := State{Saved: []netease.Playlist{{ID: 10}}, Playlists: map[int64]*netease.Playlist{7: original}}
	old := state.Saved
	state.SetSaved(*original, true, 0)
	if len(old) != 1 || old[0].ID != 10 || len(state.Saved) != 2 || len(state.Saved[0].Tracks) != 0 || !original.Subscribed {
		t.Fatal("sidebar and detail subscription diverged or mutated an old slice")
	}
	if at := state.SetSaved(*original, false, 0); at != 0 || original.Subscribed || len(state.Saved) != 1 {
		t.Fatal("unsubscribing did not update the shared detail")
	}
}

// Restoring a failed album removal keeps its previous order and omits heavy song data from the sidebar.
func TestAlbumRollbackRestoresPosition(t *testing.T) {
	state := State{SubAlbums: []netease.AlbumProfile{{Album: netease.Album{ID: 1}}, {Album: netease.Album{ID: 2}}, {Album: netease.Album{ID: 3}}}}
	album := state.SubAlbums[1]
	album.Songs = []netease.Track{{ID: 9}}
	before := state.SubAlbums
	at := state.SetAlbumSaved(album, false, 0)
	state.SetAlbumSaved(album, true, at)
	if at != 1 || len(before) != 3 || before[1].ID != 2 || len(state.SubAlbums) != 3 || state.SubAlbums[1].ID != 2 || state.SubAlbums[1].Songs != nil {
		t.Fatal("rollback changed the old list, its order or retained detail songs")
	}
}
