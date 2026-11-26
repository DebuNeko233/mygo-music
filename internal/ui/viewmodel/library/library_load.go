/*
加载用户歌单、喜欢歌曲和收藏专辑，并发布共享集合状态。
*/

package library

import (
	"maps"
	"reflect"
	"slices"

	diskcache "netease-music-mygo/internal/features/cache/disk"
	"netease-music-mygo/internal/features/library"
	"netease-music-mygo/internal/features/netease"
)

func (m *Library) LoadMine(uid int64, s LibraryServices) {
	s = m.withinLifetime(s)
	if m.Refresh.Mine.Busy {
		return
	}
	r := &m.Refresh.Mine
	r.At, r.Busy, r.Retry = m.Now(), true, false
	r.Failed, r.Failure = false, nil
	revision := m.Refresh.Revision
	beforeMine, beforeSaved := library.CopyPlaylists(m.Mine), library.CopyPlaylists(m.Saved)
	req := s.Cache("mine")
	fetch := s.Mine
	go func() {
		var kept library.Lists
		if at := req.Read(&kept); !at.IsZero() {
			s.Post(func() {
				if m.Refresh.Edits == 0 && m.Refresh.Revision == revision && m.Mine == nil && m.Saved == nil {
					m.Mine, m.Saved = kept.Mine, kept.Saved
					beforeMine, beforeSaved = library.CopyPlaylists(m.Mine), library.CopyPlaylists(m.Saved)
				}
			})
		}
		made, saved, err := fetch(uid)
		bytes := diskcache.Encode(library.Lists{Mine: made, Saved: saved})
		s.Post(func() {
			r.Busy = false
			if err != nil {
				r.Failed, r.Failure = true, err
				return
			}
			if m.Refresh.Edits != 0 || m.Refresh.Revision != revision || !reflect.DeepEqual(m.Mine, beforeMine) || !reflect.DeepEqual(m.Saved, beforeSaved) {
				r.Stale()
				return
			}
			m.Mine, m.Saved = made, saved
			req.Keep(bytes)
			if m.Refresh.FindLiked {
				if id := m.LikedPlaylist(); id != 0 {
					m.Refresh.FindLiked = false
					m.RevalidatePlaylist(id, s.Playlist)
				}
			}
		})
	}()
}

func (m *Library) LoadLiked(uid int64, s LibraryServices) {
	s = m.withinLifetime(s)
	if m.Refresh.Likes.Busy {
		return
	}
	r := &m.Refresh.Likes
	r.At, r.Busy, r.Retry = m.Now(), true, false
	r.Failed, r.Failure = false, nil
	revision := m.Refresh.Revision
	before := maps.Clone(m.Liked)
	req := s.Cache("likes")
	fetch := s.Likes
	go func() {
		var kept []int64
		if at := req.Read(&kept); !at.IsZero() {
			s.Post(func() {
				if m.Refresh.Edits == 0 && m.Refresh.Revision == revision && m.Liked == nil {
					m.Liked = make(map[int64]bool, len(kept))
					for _, id := range kept {
						m.Liked[id] = true
					}
					before = maps.Clone(m.Liked)
				}
			})
		}
		ids, err := fetch(uid)
		bytes := diskcache.Encode(ids)
		s.Post(func() {
			r.Busy = false
			if err != nil {
				r.Failed, r.Failure = true, err
				return
			}
			if m.Refresh.Edits != 0 || m.Refresh.Revision != revision || !maps.Equal(m.Liked, before) {
				r.Stale()
				return
			}
			req.Keep(bytes)
			m.Liked = make(map[int64]bool, len(ids))
			for _, id := range ids {
				m.Liked[id] = true
			}
		})
	}()
}

func (m *Library) LoadAlbums(uid int64, s LibraryServices) {
	s = m.withinLifetime(s)
	if m.Refresh.Albums.Busy {
		return
	}
	r := &m.Refresh.Albums
	r.At, r.Busy, r.Retry = m.Now(), true, false
	r.Failed, r.Failure = false, nil
	revision := m.Refresh.Revision
	before := slices.Clone(m.SubAlbums)
	req := s.Cache("albums")
	fetch := s.Albums
	go func() {
		var kept []netease.AlbumProfile
		if at := req.Read(&kept); !at.IsZero() {
			s.Post(func() {
				if m.Refresh.Edits == 0 && m.Refresh.Revision == revision && !m.SubsShown {
					m.SubAlbums, m.SubsShown = kept, true
					before = slices.Clone(m.SubAlbums)
				}
			})
		}
		als, err := fetch()
		bytes := diskcache.Encode(als)
		s.Post(func() {
			r.Busy = false
			if err != nil {
				r.Failed, r.Failure = true, err
				return
			}
			if m.Refresh.Edits != 0 || m.Refresh.Revision != revision || !reflect.DeepEqual(m.SubAlbums, before) {
				r.Stale()
				return
			}
			m.SubAlbums, m.SubsShown = als, true
			req.Keep(bytes)
		})
	}()
}
