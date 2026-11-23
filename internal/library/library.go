// Package library owns the listener's shared collections, independently of windows.
package library

import (
	"slices"

	"netease-music-mygo/internal/netease"
)

// State is the single source read by the sidebar, detail pages and playback controls.
// Access is confined to the owning thread; views borrow its collections read-only.
type State struct {
	Mine, Saved                  []netease.Playlist
	Liked                        map[int64]bool
	LikedPrograms, AskedPrograms map[int64]bool
	SubAlbums                    []netease.AlbumProfile
	SubsShown                    bool
	Playlists                    map[int64]*netease.Playlist
}

type Lists struct{ Mine, Saved []netease.Playlist }

// PlaylistRemoval retains the original ordering across overlapping removals.
type PlaylistRemoval struct {
	Original *netease.Playlist
	Hidden   map[int64]bool
	Pending  int
}

func (m *State) Editable(id int64) *netease.Playlist {
	for i := range m.Mine {
		if m.Mine[i].ID == id && m.Mine[i].SpecialType != 5 {
			return &m.Mine[i]
		}
	}
	return nil
}

func (m *State) IsSaved(pl *netease.Playlist) bool {
	return pl.Subscribed || slices.ContainsFunc(m.Saved, func(s netease.Playlist) bool { return s.ID == pl.ID })
}

func (m *State) SetSaved(pl netease.Playlist, save bool, at int) int {
	if loaded := m.Playlists[pl.ID]; loaded != nil {
		loaded.Subscribed = save
	}
	// A new slice, not shifting the one the sidebar may be listing.
	saved := slices.Clone(m.Saved)
	was := slices.IndexFunc(saved, func(s netease.Playlist) bool { return s.ID == pl.ID })
	if was >= 0 {
		saved = slices.Delete(saved, was, was+1)
	}
	if save {
		pl.Subscribed = true
		pl.Tracks, pl.TrackIDs = nil, nil
		saved = slices.Insert(saved, min(at, len(saved)), pl)
	}
	m.Saved = saved
	return was
}

func (m *State) SetPlaylistName(id int64, name string) {
	if pl := m.Editable(id); pl != nil {
		pl.Name = name
	}
	if pl := m.Playlists[id]; pl != nil {
		pl.Name = name
	}
}

func WithoutSong(pl *netease.Playlist, id int64) *netease.Playlist {
	return WithoutSongs(pl, func(song int64) bool { return song == id })
}

func WithoutSongs(pl *netease.Playlist, hidden func(int64) bool) *netease.Playlist {
	next := *pl
	next.Tracks = slices.DeleteFunc(slices.Clone(pl.Tracks), func(tr netease.Track) bool { return hidden(tr.ID) })
	next.TrackIDs = slices.DeleteFunc(slices.Clone(pl.TrackIDs), func(id netease.TrackID) bool { return hidden(id.ID) })
	removed := max(len(pl.Tracks)-len(next.Tracks), len(pl.TrackIDs)-len(next.TrackIDs))
	if removed == 0 {
		return pl
	}
	next.TrackCount = max(0, pl.TrackCount-removed)
	return &next
}

func (m *State) IsLiked(tr *netease.Track) bool {
	if tr.IsLocal() {
		return false
	}
	if tr.ProgramID != 0 {
		return m.LikedPrograms[tr.ProgramID]
	}
	return m.Liked[tr.ID]
}

func (m *State) SetLiked(id int64, like bool) {
	if like {
		if m.Liked == nil {
			m.Liked = map[int64]bool{}
		}
		m.Liked[id] = true
	} else {
		delete(m.Liked, id)
	}
}

func (m *State) SetProgramLiked(id int64, like bool) {
	if m.LikedPrograms == nil {
		m.LikedPrograms = map[int64]bool{}
	}
	m.LikedPrograms[id] = like
	delete(m.AskedPrograms, id)
}

func (m *State) AlbumSaved(id int64) bool {
	return slices.ContainsFunc(m.SubAlbums, func(al netease.AlbumProfile) bool { return al.ID == id })
}

func (m *State) SetAlbumSaved(al netease.AlbumProfile, save bool, at int) int {
	// A new slice, not shifting the one the listener's page may be listing.
	saved := slices.Clone(m.SubAlbums)
	was := slices.IndexFunc(saved, func(s netease.AlbumProfile) bool { return s.ID == al.ID })
	if was >= 0 {
		saved = slices.Delete(saved, was, was+1)
	}
	if save {
		al.Songs = nil
		saved = slices.Insert(saved, min(at, len(saved)), al)
	}
	m.SubAlbums = saved
	return was
}

func (m *State) LikedPlaylist() int64 {
	for _, pl := range m.Mine {
		if pl.SpecialType == 5 {
			return pl.ID
		}
	}
	return 0
}

func CopyPlaylist(pl netease.Playlist) netease.Playlist {
	pl.Tracks = slices.Clone(pl.Tracks)
	pl.TrackIDs = slices.Clone(pl.TrackIDs)
	return pl
}

func CopyPlaylists(pl []netease.Playlist) []netease.Playlist {
	if pl == nil {
		return nil
	}
	result := make([]netease.Playlist, len(pl))
	for i := range pl {
		result[i] = CopyPlaylist(pl[i])
	}
	return result
}
