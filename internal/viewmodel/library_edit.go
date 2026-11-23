package viewmodel

import (
	"errors"
	"log"
	"slices"
	"strings"

	"netease-music-mygo/internal/library"
	"netease-music-mygo/internal/netease"
)

func (m *Library) ToggleSaved(pl netease.Playlist, s LibraryServices) {
	s = m.withinLifetime(s)
	id := pl.ID
	if m.SavingPlaylists[id] {
		return
	}
	if m.SavingPlaylists == nil {
		m.SavingPlaylists = map[int64]bool{}
	}
	m.SavingPlaylists[id] = true
	save := !m.IsSaved(&pl)
	at := m.SetSaved(pl, save, 0)

	finishEdit := m.BeginEdit()
	go func() {
		err := s.SavePlaylist(id, save)
		s.Post(func() {
			finishEdit()
			defer m.persist(s, id)
			delete(m.SavingPlaylists, id)
			switch {
			case err != nil:
				m.SetSaved(pl, !save, max(at, 0))
				s.Notify("操作失败，请稍后再试")
			case save:
				s.Notify("已收藏")
			default:
				s.Notify("已取消收藏")
			}
		})
	}()
}

func (m *Library) SubmitName(s LibraryServices) {
	s = m.withinLifetime(s)
	d := &m.Naming
	name := strings.TrimSpace(d.Name)
	if name == "" || d.Busy {
		return
	}
	if d.Rename != 0 {
		id := d.Rename
		d.Open = false
		pl := m.Editable(id)
		if pl == nil || pl.Name == name {
			return
		}
		old := pl.Name

		m.SetPlaylistName(id, name)
		finishEdit := m.BeginEdit()
		go func() {
			err := s.Rename(id, name)
			s.Post(func() {
				finishEdit()
				defer m.persist(s, id)
				if err != nil {
					m.SetPlaylistName(id, old)
					s.Notify("重命名失败，请稍后再试")
				}
			})
		}()
		return
	}
	d.Busy = true
	add := d.Add

	finishEdit := m.BeginEdit()
	go func() {
		pl, err := s.Create(name)
		s.Post(func() {
			finishEdit()
			defer m.persist(s)
			m.Naming.Busy = false
			if err != nil {
				s.Notify("创建歌单失败，请稍后再试")
				return
			}
			m.Naming.Open = false
			// Under the liked songs, as NetEase lists the newest.
			at := 0
			if len(m.Mine) > 0 && m.Mine[0].SpecialType == 5 {
				at = 1
			}
			m.Mine = slices.Insert(m.Mine, at, *pl)
			pl.Tracks = nil
			m.Playlists[pl.ID] = pl
			s.Touch(pl.ID)
			s.Open(pl.ID)
			if add != nil {
				m.AddTo(pl.ID, add, s)
			}
		})
	}()
}

func (m *Library) DeleteChosen(s LibraryServices) {
	s = m.withinLifetime(s)
	id := m.Deleting.ID
	m.Deleting.Open = false
	i := slices.IndexFunc(m.Mine, func(pl netease.Playlist) bool { return pl.ID == id })
	if i < 0 || m.Mine[i].SpecialType == 5 {
		return
	}
	pl := m.Mine[i]
	m.Mine = slices.Delete(slices.Clone(m.Mine), i, i+1)
	s.LeaveDeleted(id)
	finishEdit := m.BeginEdit()
	go func() {
		err := s.Delete(id)
		s.Post(func() {
			finishEdit()
			defer m.persist(s)
			if err != nil {
				m.Mine = slices.Insert(m.Mine, min(i, len(m.Mine)), pl)
				s.Notify("删除失败，请稍后再试")
				return
			}
			s.Forget(id)
			s.Notify("已删除歌单")
		})
	}()
}

func (m *Library) AddTo(pid int64, tr *netease.Track, s LibraryServices) {
	s = m.withinLifetime(s)
	id := tr.ID

	finishEdit := m.BeginEdit()
	go func() {
		err := s.Add(pid, id)
		s.Post(func() {
			finishEdit()
			defer m.persist(s, pid)
			switch {
			case errors.Is(err, netease.ErrInPlaylist):
				s.Notify("歌曲已在歌单中")
			case err != nil:
				s.Notify("添加失败，请稍后再试")
			default:
				s.Notify("已添加到歌单")
				s.PlaylistChanged(pid)
			}
		})
	}()
}

func (m *Library) RemoveFrom(pid int64, tr *netease.Track, s LibraryServices) {
	s = m.withinLifetime(s)
	id := tr.ID
	before := m.Playlists[pid]
	req := m.playlistRequest(pid)
	edit := req.Removal
	if edit == nil {
		edit = &library.PlaylistRemoval{Original: before, Hidden: map[int64]bool{}}
		req.Removal = edit
	}
	if edit.Hidden[id] {
		return
	}
	edit.Hidden[id] = true
	edit.Pending++
	if before != nil {
		m.Playlists[pid] = library.WithoutSong(before, id)
	}

	finishEdit := m.BeginEdit()
	remove := s.Remove
	go func() {
		err := remove(pid, id)
		s.Post(func() {
			finishEdit()
			defer m.persist(s, pid)
			if m.Refresh.Playlists[pid] != req {
				return // The playlist itself was deleted.
			}
			edit.Pending--
			if edit.Pending == 0 {
				req.Removal = nil
			}
			if err != nil {
				delete(edit.Hidden, id)
				if current, original := m.Playlists[pid], edit.Original; current != nil && original != nil {
					restored := *current
					restored.Tracks, restored.TrackIDs, restored.TrackCount = original.Tracks, original.TrackIDs, original.TrackCount
					m.Playlists[pid] = library.WithoutSongs(&restored, func(song int64) bool { return edit.Hidden[song] })
				}
				s.Notify("移除失败，请稍后再试")
				return
			}
			s.PlaylistChanged(pid)
		})
	}()
}

func (m *Library) ToggleLike(tr *netease.Track, s LibraryServices) {
	s = m.withinLifetime(s)
	if tr.IsLocal() || tr.Cloud {
		return
	}
	if tr.ProgramID != 0 {
		m.ToggleProgramLike(tr.ProgramID, s)
		return
	}
	if m.Liked == nil {
		m.Liked = map[int64]bool{}
	}
	id, like := tr.ID, !m.Liked[tr.ID]

	m.SetLiked(id, like)
	finishEdit := m.BeginEdit()
	ask := s.LikeSong
	go func() {
		err := ask(id, like)
		s.Post(func() {
			finishEdit()
			if err != nil {
				log.Printf("like song %d: %v", id, err)
				if m.Liked[id] == like {
					m.SetLiked(id, !like)
				}
				s.Notify("操作失败，请稍后再试")
			} else if changed := m.updateLikedPlaylist(id, like, s); changed != 0 {
				s.Persist(SnapshotLikes, changed)
				return
			}
			s.Persist(SnapshotLikes)
		})
	}()
}

func (m *Library) ToggleProgramLike(id int64, s LibraryServices) {
	s = m.withinLifetime(s)
	like := !m.LikedPrograms[id]

	m.SetProgramLiked(id, like)
	ask := s.LikeProgram
	go func() {
		err := ask(id, like)
		s.Post(func() {
			if err != nil {
				m.SetProgramLiked(id, !like)
				s.Notify("操作失败，请稍后再试")
			}
		})
	}()
}

func (m *Library) AskProgramLikes(ids []int64, s LibraryServices) {
	s = m.withinLifetime(s)
	var ask []int64
	for _, id := range ids {
		if _, known := m.LikedPrograms[id]; !known && !m.AskedPrograms[id] {
			ask = append(ask, id)
		}
	}
	if len(ask) == 0 {
		return
	}
	if m.AskedPrograms == nil {
		m.AskedPrograms = map[int64]bool{}
	}
	for _, id := range ask {
		m.AskedPrograms[id] = true
	}

	fetch := s.ProgramsLiked
	go func() {
		liked, _ := fetch(ask)
		s.Post(func() { m.ProgramLikesAnswered(ask, liked) })
	}()
}

func (m *Library) ProgramLikesAnswered(asked []int64, liked map[int64]bool) {
	for _, id := range asked {
		// A heart clicked meanwhile knows better.
		if m.AskedPrograms[id] {
			m.SetProgramLiked(id, liked[id])
		}
	}
}

func (m *Library) updateLikedPlaylist(song int64, like bool, s LibraryServices) int64 {
	pid := m.LikedPlaylist()
	if pid == 0 {
		pid = s.VisibleLikedPlaylist()
	}
	if pid == 0 {
		m.Refresh.FindLiked = true
		return 0
	}
	pl := m.Playlists[pid]
	var changed int64
	if !like && pl != nil {
		if next := library.WithoutSong(pl, song); next != pl {
			m.Playlists[pid] = next
			changed = pid
		}
	}
	s.Revalidate(pid)
	return changed
}

func (m *Library) ToggleAlbumSaved(al netease.AlbumProfile, s LibraryServices) {
	s = m.withinLifetime(s)
	id := al.ID
	if m.SavingAlbums[id] {
		return
	}
	if m.SavingAlbums == nil {
		m.SavingAlbums = map[int64]bool{}
	}
	m.SavingAlbums[id] = true
	save := !m.AlbumSaved(id)
	at := m.SetAlbumSaved(al, save, 0)

	finishEdit := m.BeginEdit()
	go func() {
		err := s.SaveAlbum(id, save)
		s.Post(func() {
			finishEdit()
			defer m.persist(s)
			delete(m.SavingAlbums, id)
			if err == nil {
				// The optimistic state was accepted.
				return
			}
			m.SetAlbumSaved(al, !save, max(at, 0))
			s.Notify("操作失败，请稍后再试")
		})
	}()
}

func (m *Library) persist(s LibraryServices, ids ...int64) {
	s.Persist(SnapshotLists|SnapshotAlbums|SnapshotLikes, ids...)
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
func (m *Library) NeedsProgramLikes(ids []int64) bool {
	for _, id := range ids {
		if _, known := m.LikedPrograms[id]; !known && !m.AskedPrograms[id] {
			return true
		}
	}
	return false
}
