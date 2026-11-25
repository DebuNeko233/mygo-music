/*
验证快照等待编辑完成且不受后续修改影响，并在重置后丢弃旧编辑。
*/

package library

import (
	"slices"
	"testing"

	"netease-music-mygo/internal/library"
	"netease-music-mygo/internal/netease"
)

// Overlapping edits publish only their final state, and later mutations cannot
// change the playlist slices, liked IDs or collection rows given to disk workers.
func TestSnapshotsWaitForEditsAndOwnTheirData(t *testing.T) {
	var m Library
	m.Reset()
	pl := netease.Playlist{ID: 7, Name: "before", Tracks: []netease.Track{{ID: 1}}, TrackIDs: []netease.TrackID{{ID: 1}}}
	m.Mine, m.Saved = []netease.Playlist{pl}, []netease.Playlist{pl}
	m.Playlists[7] = &pl
	m.SubAlbums, m.SubsShown = []netease.AlbumProfile{{Album: netease.Album{ID: 9}}}, true
	m.Liked = map[int64]bool{3: true, 1: true, 2: false}
	finishFirst, finishLast := m.BeginEdit(), m.BeginEdit()
	if got := m.TakeSnapshots(SnapshotLists|SnapshotLikes, 7); len(got) != 0 {
		t.Fatal("published an unfinished edit")
	}
	finishFirst()
	if got := m.TakeSnapshots(SnapshotAlbums, 7); len(got) != 0 {
		t.Fatal("published before the last edit settled")
	}
	pl.Name = "committed"
	finishLast()
	batch := m.TakeSnapshots(0)
	if len(batch) != 4 {
		t.Fatalf("lost accumulated collections: %+v", batch)
	}
	m.Mine[0].Name, m.Saved[0].Name = "later", "later"
	pl.Tracks[0].ID, pl.TrackIDs[0].ID, pl.Name = 8, 8, "later"
	m.SubAlbums[0].ID = 8
	m.Liked[1] = false
	for _, snapshot := range batch {
		switch snapshot.Key {
		case "mine":
			lists := snapshot.Value.(library.Lists)
			if lists.Mine[0].Name != "before" || lists.Saved[0].Name != "before" || lists.Mine[0].Tracks[0].ID != 1 || lists.Saved[0].TrackIDs[0].ID != 1 {
				t.Fatal("collection snapshot shares mutable playlist data")
			}
		case "albums":
			if snapshot.Value.([]netease.AlbumProfile)[0].ID != 9 {
				t.Fatal("album snapshot shares collection rows")
			}
		case "likes":
			if !slices.Equal(snapshot.Value.([]int64), []int64{1, 3}) {
				t.Fatal("liked snapshot lost sorted, true-only IDs")
			}
		case "playlist/7":
			kept := snapshot.Value.(netease.Playlist)
			if !snapshot.Playlist || kept.Name != "committed" || kept.Tracks[0].ID != 1 || kept.TrackIDs[0].ID != 1 {
				t.Fatal("playlist snapshot lost final state or shares mutable tracks")
			}
		default:
			t.Fatalf("unexpected snapshot: %s", snapshot.Key)
		}
	}
	if got := m.TakeSnapshots(0); len(got) != 0 {
		t.Fatal("already drained changes were published twice")
	}
}

// Reset discards accumulated changes from the previous model lifetime.
func TestSnapshotResetDiscardsPendingEdits(t *testing.T) {
	var m Library
	m.Reset()
	finish := m.BeginEdit()
	m.TakeSnapshots(SnapshotLikes, 7)
	m.Reset()
	finish()
	if got := m.TakeSnapshots(0); len(got) != 0 || m.Refresh.Edits != 0 || m.Dirty != 0 || len(m.DirtySnapshots) != 0 {
		t.Fatal("reset retained changes or an old edit affected the new lifetime")
	}
}
