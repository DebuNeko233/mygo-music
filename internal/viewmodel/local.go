package viewmodel

import (
	"slices"

	"netease-music-mygo/internal/localcatalog"
	"netease-music-mygo/internal/lyrics"
	"netease-music-mygo/internal/netease"
)

// LocalLibrary owns local catalog commands and jobs on the main thread.
// Windows, file pickers and scrolling belong to the host. Workers publish through Post.
type LocalLibrary struct {
	localcatalog.State
	Path, Status    string
	Busy            bool
	check           uint64
	Temporary       localcatalog.Song
	temporaryID     int64
	TemporaryLyrics lyrics.Text
	LyricCache      localcatalog.LyricCache
	LyricMatch      *LocalLyricMatch
	openRequest     uint64
}

// Load restores the existing catalog schema before the player/session are connected.
func (m *LocalLibrary) Load(path string) {
	m.Path = path
	catalog, err := localcatalog.Load(path)
	if err != nil {
		m.Path, m.Status = "", "无法读取本地曲库"
		catalog = localcatalog.Catalog{}
	}
	m.Replace(catalog)
	if m.Path != "" && m.Catalog.LastID > 0 {
		go localcatalog.PruneLyrics(m.Path, m.Catalog.LastID, m.ByID)
	}
}

// LocalServices connects catalog I/O and existing playback metadata updates.
// Post always runs the callback on the owning thread; Changed redraws auxiliary views.
type LocalServices struct {
	Post         func(func())
	Save         func(string, localcatalog.Catalog) bool
	Check        func([]localcatalog.Song, map[string]struct{}) map[int64]bool
	UpdateTracks func([]netease.Track)
	Changed      func()
}

func (m *LocalLibrary) CheckFiles(s LocalServices) {
	if m.Busy || len(m.Catalog.Songs) == 0 {
		return
	}
	m.check++
	check, songs := m.check, m.Catalog.Songs
	go func() {
		missing := s.Check(songs, nil)
		s.Post(func() {
			if m.check == check {
				m.SetMissing(missing)
			}
		})
	}()
}

// change serializes edits and replaces published data only after the write succeeds.
func (m *LocalLibrary) change(work func(*localcatalog.Catalog, map[string]struct{}) (bool, string), s LocalServices) {
	if m.Busy || m.Path == "" {
		return
	}
	m.Busy, m.Status = true, "正在更新本地曲库…"
	s.Changed()
	m.check++
	path, original := m.Path, m.Catalog
	catalog := original
	catalog.Songs = slices.Clone(catalog.Songs)
	catalog.Folders = slices.Clone(catalog.Folders)
	go func() {
		present := make(map[string]struct{})
		changed, status := work(&catalog, present)
		if changed {
			if s.Save(path, catalog) {
				m.LyricCache.RemoveDeleted(path, original.Songs, catalog.Songs)
			} else {
				changed, status = false, "无法保存本地曲库"
			}
		}
		if !changed {
			catalog = original
		}
		missing := s.Check(catalog.Songs, present)
		s.Post(func() {
			m.Busy, m.Status = false, status
			var updated []netease.Track
			if changed {
				for _, song := range catalog.Songs {
					old := m.Song(song.Track.LocalID)
					if old.Path != "" && (old.Path != song.Path || old.Stamp != song.Stamp || old.Size != song.Size) {
						updated = append(updated, song.Track)
					}
				}
				m.Replace(catalog)
			}
			m.SetMissing(missing)
			s.UpdateTracks(updated)
			s.Changed()
		})
	}()
}
func (m *LocalLibrary) AddFolders(paths []string, s LocalServices) {
	if len(paths) == 0 {
		return
	}
	paths = slices.Clone(paths)
	m.change(func(c *localcatalog.Catalog, p map[string]struct{}) (bool, string) {
		return localcatalog.AddFolders(c, p, paths)
	}, s)
}
func (m *LocalLibrary) Refresh(s LocalServices) {
	if m.Busy {
		return
	}
	if len(m.Catalog.Folders) == 0 {
		m.CheckFiles(s)
		return
	}
	m.change(localcatalog.Scan, s)
}
func (m *LocalLibrary) RemoveFolder(path string, s LocalServices) {
	m.change(func(c *localcatalog.Catalog, _ map[string]struct{}) (bool, string) {
		return localcatalog.RemoveFolder(c, path)
	}, s)
}
func (m *LocalLibrary) Relink(id int64, path string, s LocalServices) {
	m.change(func(c *localcatalog.Catalog, p map[string]struct{}) (bool, string) {
		return localcatalog.Relink(c, p, id, path)
	}, s)
}
func (m *LocalLibrary) RemoveSong(id int64, s LocalServices) {
	m.change(func(c *localcatalog.Catalog, _ map[string]struct{}) (bool, string) {
		return localcatalog.RemoveSong(c, id)
	}, s)
}

func (m *LocalLibrary) LyricPath(id int64) string {
	if m.Path == "" {
		return ""
	}
	return localcatalog.LyricFile(m.Path, id)
}

// LyricsLoader captures a stable payload or path before the player starts worker I/O.
func (m *LocalLibrary) LyricsLoader(track netease.Track) func() (string, string, string) {
	if track.Temporary {
		kept := m.TemporaryLyrics
		return func() (string, string, string) { return kept.Original, kept.Translation, kept.Words }
	}
	path := m.LyricPath(track.LocalID)
	if path == "" {
		return nil
	}
	return func() (string, string, string) { return localcatalog.LoadLyrics(path) }
}
