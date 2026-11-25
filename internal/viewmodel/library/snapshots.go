/*
汇总重叠编辑的脏集合，在编辑完成后生成供后台持久化的独立快照。
*/

package library

import (
	"fmt"
	"slices"

	"netease-music-mygo/internal/library"
)

// Snapshot owns copied data that can be encoded on a worker after model edits.
type Snapshot struct {
	Key      string
	Value    any
	Playlist bool
}

// TakeSnapshots accumulates dirty collections across overlapping edits. Only
// their final success or rollback releases a batch, independent of future edits.
// The caller must reserve writes on the owner thread before starting workers.
func (m *Library) TakeSnapshots(changed LibrarySnapshots, ids ...int64) []Snapshot {
	if m.DirtySnapshots == nil {
		m.DirtySnapshots = map[int64]bool{}
	}
	for _, id := range ids {
		m.DirtySnapshots[id] = true
	}
	m.Dirty |= changed
	if m.Refresh.Edits != 0 {
		return nil
	}

	var snapshots []Snapshot
	keep := func(key string, value any) { snapshots = append(snapshots, Snapshot{Key: key, Value: value}) }
	if m.Dirty&SnapshotLists != 0 && (m.Mine != nil || m.Saved != nil) {
		keep("mine", library.Lists{Mine: library.CopyPlaylists(m.Mine), Saved: library.CopyPlaylists(m.Saved)})
	}
	if m.Dirty&SnapshotAlbums != 0 && m.SubsShown {
		keep("albums", slices.Clone(m.SubAlbums))
	}
	if m.Dirty&SnapshotLikes != 0 && m.Liked != nil {
		likes := make([]int64, 0, len(m.Liked))
		for id, liked := range m.Liked {
			if liked {
				likes = append(likes, id)
			}
		}
		slices.Sort(likes)
		keep("likes", likes)
	}

	for id := range m.DirtySnapshots {
		if pl := m.Playlists[id]; pl != nil {
			snapshots = append(snapshots, Snapshot{Key: fmt.Sprintf("playlist/%d", id), Value: library.CopyPlaylist(*pl), Playlist: true})
		}
	}
	clear(m.DirtySnapshots)
	m.Dirty = 0
	return snapshots
}
