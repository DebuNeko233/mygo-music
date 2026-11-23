package library

import (
	"fmt"
	"reflect"
	"slices"

	"netease-music-mygo/internal/diskcache"
	"netease-music-mygo/internal/library"
	"netease-music-mygo/internal/netease"
	"netease-music-mygo/internal/viewmodel"
)

// PlaylistServices keeps platform dispatch, disk coordination and playback in the host.
// Post runs on the owner thread and rejects retired account generations.
type PlaylistServices struct {
	Post            func(func() bool)
	Cache           func(string) diskcache.Snapshot
	Detail          func(int64) (*netease.Playlist, error)
	WaitSnapshot    func(diskcache.Snapshot)
	SaveSnapshot    func(diskcache.Snapshot, func() []byte)
	Play            func(*netease.Playlist)
	VisiblePlaylist func() int64
	Recovering      func() bool
}

// NeedsPlaylistLoad lets the host avoid creating service closures for a fresh page.
func (m *Library) NeedsPlaylistLoad(id int64) bool {
	r := m.Refresh.Playlists[id]
	if r != nil && r.Busy {
		return false
	}
	return m.Playlists[id] == nil || m.Refresh.Edits == 0 && (r == nil || viewmodel.ListDue(r.At, m.Now()))
}

func (m *Library) TouchPlaylist(id int64) {
	m.UsePlaylist(id)
	m.playlistRequest(id).At = m.Now()
}

// ForgetPlaylist retires both the snapshot and its request identity in memory.
// The host separately invalidates the offline snapshot when deleting a playlist.
func (m *Library) ForgetPlaylist(id int64) {
	delete(m.Playlists, id)
	delete(m.Refresh.Playlists, id)
	delete(m.Refresh.EditedLists, id)
	delete(m.Loading, id)
	delete(m.PlayWhenLoaded, id)
	delete(m.Errors, id)
	delete(m.DirtySnapshots, id)
}

// RevalidatePlaylist retains optimistic content until a post-edit refresh is accepted.
func (m *Library) RevalidatePlaylist(id int64, s PlaylistServices) {
	r := m.playlistRequest(id)
	r.Stale()
	if m.Refresh.EditedLists == nil {
		m.Refresh.EditedLists = map[int64]struct{}{}
	}
	m.Refresh.EditedLists[id] = struct{}{}
	if !r.Busy && m.Refresh.Edits == 0 {
		m.LoadPlaylist(id, false, s)
	}
}

// LoadPlaylist shows a cached list while an expired copy is fetched anew.
// A play request takes the displayed queue immediately; refreshing changes
// the page alone, never the queue currently playing.
func (m *Library) LoadPlaylist(id int64, thenPlay bool, s PlaylistServices) {
	m.UsePlaylist(id)
	was := m.Playlists[id]
	if was != nil && thenPlay {
		s.Play(was)
	}
	r := m.playlistRequest(id)
	if r.Busy {
		if was == nil && thenPlay {
			m.PlayWhenLoaded[id] = true
		}
		return
	}
	if was != nil && (m.Refresh.Edits != 0 || !viewmodel.ListDue(r.At, m.Now())) {
		return
	}
	r.At, r.Busy, r.Retry = m.Now(), true, false
	r.Failed, r.Failure = false, nil
	if was == nil {
		m.Loading[id] = true
	}
	var before *netease.Playlist
	if was != nil {
		snapshot := library.CopyPlaylist(*was)
		before = &snapshot
	}
	generation, revision := m.generation, m.Refresh.Revision
	post := func(update func() bool) {
		s.Post(func() bool {
			if m.generation != generation {
				return false
			}
			return update()
		})
	}
	req := s.Cache(fmt.Sprintf("playlist/%d", id))
	readDisk := was == nil
	fetch := s.Detail
	go func(baseline *netease.Playlist) {
		if readDisk {
			s.WaitSnapshot(req)
			var kept *netease.Playlist
			if at := req.Read(&kept); !at.IsZero() && kept != nil && kept.ID == id {
				snapshot := library.CopyPlaylist(*kept)
				baseline = &snapshot
				post(func() bool {
					if m.Refresh.Playlists[id] == r && m.Playlists[id] == nil && m.Refresh.Edits == 0 && m.Refresh.Revision == revision {
						m.Playlists[id] = kept
						was = kept
						before = &snapshot
						delete(m.Loading, id)
						if thenPlay || m.PlayWhenLoaded[id] {
							s.Play(kept)
							thenPlay = false
							delete(m.PlayWhenLoaded, id)
						}
						return true
					}
					return false
				})
			}
		}
		pl, err := fetch(id)
		var sameTracks, unchanged bool
		if err == nil && pl != nil && baseline != nil {
			sameTracks = len(baseline.Tracks) == 0 && len(pl.Tracks) == 0 || reflect.DeepEqual(baseline.Tracks, pl.Tracks)
			if sameTracks && slices.Equal(baseline.TrackIDs, pl.TrackIDs) {
				old, fresh := *baseline, *pl
				old.Tracks, fresh.Tracks = nil, nil
				old.TrackIDs, fresh.TrackIDs = nil, nil
				unchanged = reflect.DeepEqual(old, fresh)
			}
		}
		var bytes []byte
		if err == nil && pl != nil && req.Store != nil && !(readDisk && unchanged) {
			bytes = diskcache.Encode(pl)
		}
		post(func() bool {
			if m.Refresh.Playlists[id] != r {
				return false
			}
			r.Busy = false
			defer func() { m.TrimPlaylists(s.VisiblePlaylist()) }()
			play := thenPlay || m.PlayWhenLoaded[id]
			delete(m.Loading, id)
			delete(m.PlayWhenLoaded, id)
			if err != nil || pl == nil {
				if !r.Retry {
					delete(m.Refresh.EditedLists, id)
				}
				r.Failed, r.Failure = true, err
				if was == nil {
					m.Errors[id] = "歌单加载失败"
				}
				return true // the error or network recovery can change the UI
			}
			if m.Refresh.Edits != 0 || m.Refresh.Revision != revision {
				if was == nil && play {
					m.PlayWhenLoaded[id] = true
				}
				r.Stale()
				return true
			}
			if was != nil {
				// A local rename, subscription, addition/removal or a replacement wins.
				if m.Playlists[id] != was || !reflect.DeepEqual(*was, *before) {
					r.Stale()
					return true
				}
			} else if m.Playlists[id] != nil {
				delete(m.Refresh.EditedLists, id)
				return false
			}
			delete(m.Refresh.EditedLists, id)
			if r.Retry {
				r.At, r.Retry = m.Now(), false
			}
			delete(m.Errors, id)
			if bytes != nil {
				s.SaveSnapshot(req, func() []byte { return bytes })
			}
			if was != nil && unchanged {
				return s.Recovering() && s.VisiblePlaylist() == id
			}
			if was != nil && sameTracks {
				pl.Tracks = was.Tracks
			}
			m.Playlists[id] = pl
			if was == nil && play {
				s.Play(pl)
			}
			return true
		})
	}(before)
}

func (m *Library) playlistRequest(id int64) *ListRequest {
	if m.Refresh.Playlists == nil {
		m.Refresh.Playlists = map[int64]*ListRequest{}
	}
	if m.Refresh.Playlists[id] == nil {
		m.Refresh.Playlists[id] = &ListRequest{}
	}
	return m.Refresh.Playlists[id]
}
