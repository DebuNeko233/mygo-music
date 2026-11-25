/*
记录歌单使用情况并按预算裁剪详情缓存，保护可见或正在编辑的歌单。
*/

package library

// Bound cached details and track records; a visible or edited playlist may exceed the budget.
const (
	PlaylistCachePages  = 8
	PlaylistCacheTracks = 20000
)

func (m *Library) UsePlaylist(id int64) {
	m.Refresh.PlaylistUse++
	m.playlistRequest(id).Used = m.Refresh.PlaylistUse
}

func (m *Library) TrimPlaylists(visible int64) {
	if m.Refresh.Edits != 0 {
		return
	}
	weight := func(id int64) int {
		p := m.Playlists[id]
		return max(len(p.Tracks), len(p.TrackIDs))
	}
	tracks := 0
	for id := range m.Playlists {
		tracks += weight(id)
	}
	for len(m.Playlists) > PlaylistCachePages || tracks > PlaylistCacheTracks {
		var oldest int64
		var used uint64
		found := false
		for id := range m.Playlists {
			_, editing := m.Refresh.EditedLists[id]
			if id == visible || m.PlayWhenLoaded[id] || m.DirtySnapshots[id] || editing {
				continue
			}
			r := m.Refresh.Playlists[id]
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
		// Unlike ForgetPlaylist, eviction must keep the offline snapshot.
		delete(m.Playlists, oldest)
		delete(m.Refresh.Playlists, oldest)
		delete(m.Errors, oldest)
	}
}
