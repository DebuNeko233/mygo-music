package library

import (
	"time"

	"netease-music-mygo/internal/netease"
	"netease-music-mygo/internal/viewmodel"
)

func (m *Library) LikesDue(uid int64) bool {
	return uid != 0 && m.Refresh.Edits == 0 && viewmodel.ListDue(m.Refresh.Likes.At, m.Now())
}
func (m *Library) AlbumsDue(uid int64) bool {
	return uid != 0 && m.Refresh.Edits == 0 && viewmodel.ListDue(m.Refresh.Albums.At, m.Now())
}
func (m *Library) FreshenLikes(uid int64, s LibraryServices) {
	if m.LikesDue(uid) {
		m.LoadLiked(uid, s)
	}
}
func (m *Library) FreshenAlbums(uid int64, s LibraryServices) {
	if m.AlbumsDue(uid) {
		m.LoadAlbums(uid, s)
	}
}

// Freshen revalidates shared personal data and the visible detail without replacing old content.
func (m *Library) Freshen(uid, visible int64, s LibraryServices) {
	if uid != 0 && m.Refresh.Edits == 0 {
		if viewmodel.ListDue(m.Refresh.Mine.At, m.Now()) {
			m.LoadMine(uid, s)
		}
		m.FreshenLikes(uid, s)
		m.FreshenAlbums(uid, s)
	}
	if visible != 0 {
		m.LoadPlaylist(visible, false, s.Playlist)
	}
}

// NeedsRefreshRetry avoids service allocation on ordinary idle view builds.
func (m *Library) NeedsRefreshRetry(uid, visible int64) bool {
	if m.Refresh.Edits != 0 {
		return false
	}
	if uid != 0 && (m.Refresh.Mine.Retry || m.Refresh.Likes.Retry || m.Refresh.Albums.Retry) {
		return true
	}
	for id := range m.Refresh.EditedLists {
		if r := m.Refresh.Playlists[id]; r != nil && !r.Busy {
			return true
		}
	}
	r := m.Refresh.Playlists[visible]
	return visible != 0 && r != nil && r.Retry && !r.Busy
}

// RetryRefresh runs after edits finish; each load consumes its retry flag before starting.
// Failed follow-ups keep their normal freshness interval rather than polling the network.
func (m *Library) RetryRefresh(uid, visible int64, s LibraryServices) {
	if m.Refresh.Edits != 0 {
		return
	}
	if uid != 0 {
		if m.Refresh.Mine.Retry {
			m.LoadMine(uid, s)
		}
		if m.Refresh.Likes.Retry {
			m.LoadLiked(uid, s)
		}
		if m.Refresh.Albums.Retry {
			m.LoadAlbums(uid, s)
		}
	}
	for id := range m.Refresh.EditedLists {
		if r := m.Refresh.Playlists[id]; r != nil && !r.Busy {
			m.LoadPlaylist(id, false, s.Playlist)
		}
	}
	if r := m.Refresh.Playlists[visible]; visible != 0 && r != nil && r.Retry && !r.Busy {
		m.LoadPlaylist(visible, false, s.Playlist)
	}
}

// NeedsRecovery distinguishes temporary network failures from explicit user retries.
func (m *Library) NeedsRecovery(visible int64, manual bool) bool {
	if listFailed(&m.Refresh.Mine, manual) || listFailed(&m.Refresh.Likes, manual) {
		return true
	}
	r := m.Refresh.Playlists[visible]
	return visible != 0 && r != nil && !r.Busy && listFailed(r, manual)
}

// RetryFailed reuses normal loading and its edit gates rather than resetting requests in the host.
func (m *Library) RetryFailed(uid, visible int64, manual bool, s LibraryServices) {
	if uid != 0 && m.Refresh.Edits == 0 {
		if listFailed(&m.Refresh.Mine, manual) {
			m.LoadMine(uid, s)
		}
		if listFailed(&m.Refresh.Likes, manual) {
			m.LoadLiked(uid, s)
		}
	}
	if r := m.Refresh.Playlists[visible]; visible != 0 && r != nil && !r.Busy && listFailed(r, manual) {
		r.At = time.Time{}
		m.LoadPlaylist(visible, false, s.Playlist)
	}
}

func listFailed(r *ListRequest, manual bool) bool {
	return r != nil && r.Failed && (manual || netease.RetryableError(r.Failure))
}
