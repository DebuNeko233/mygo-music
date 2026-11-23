package viewmodel

import (
	"cmp"
	"errors"
	"math"
	"os"
	"slices"
	"strings"

	"netease-music-mygo/internal/localcatalog"
	"netease-music-mygo/internal/lyrics"
	"netease-music-mygo/internal/netease"
)

// LocalLyricMatch owns form data and request identity; scrolling stays in the view.
type LocalLyricMatch struct {
	Open, Searching, Loading bool
	Track                    *netease.Track
	Query, Status            string
	Results                  []netease.Track
	Selected                 int64
	request                  uint64
}

// LocalLyricsServices reuses the existing API and reloads the current player's metadata.
type LocalLyricsServices struct {
	Post    func(func())
	Current func() *netease.Track
	Search  func(string, int) ([]netease.Track, error)
	Fetch   func(int64) (string, string, string, error)
	Save    func(string, int64, localcatalog.LyricMatch) error
	Reload  func()
}

func (m *LocalLibrary) OpenLyricMatch(track *netease.Track, query string, s LocalLyricsServices) {
	m.LyricMatch = &LocalLyricMatch{Open: true, Track: track, Query: query}
	m.SearchLyrics(m.LyricMatch, s)
}

func (m *LocalLibrary) matchingLyrics(d *LocalLyricMatch, s LocalLyricsServices) bool {
	return m.LyricMatch == d && d.Open && s.Current() == d.Track
}

func (m *LocalLibrary) SearchLyrics(d *LocalLyricMatch, s LocalLyricsServices) bool {
	query := strings.TrimSpace(d.Query)
	if !m.matchingLyrics(d, s) || query == "" || d.Loading {
		return false
	}
	d.request++
	d.Results, d.Selected, d.Status = nil, 0, ""
	d.Searching = true
	request, duration := d.request, d.Track.Duration
	search := s.Search
	go func() {
		results, err := search(query, 20)
		results = lyricCandidates(results, duration)
		s.Post(func() {
			if !m.matchingLyrics(d, s) || d.request != request {
				return
			}
			d.Searching, d.Results = false, results
			switch {
			case errors.Is(err, netease.ErrOffline):
				d.Status = "联网后可搜索歌词"
			case err != nil:
				d.Status = "搜索失败，请重试"
			case len(results) == 0:
				d.Status = "没有找到匹配的歌曲，可以换个关键词"
			}
		})
	}()
	return true
}

func lyricCandidates(results []netease.Track, duration int64) []netease.Track {
	seen := make(map[int64]bool, len(results))
	unique := make([]netease.Track, 0, len(results))
	for _, track := range results {
		if track.ID > 0 && !seen[track.ID] {
			seen[track.ID] = true
			unique = append(unique, track)
		}
	}
	distance := func(t netease.Track) int64 {
		if t.Duration <= 0 || duration <= 0 {
			return math.MaxInt64
		}
		return max(t.Duration-duration, duration-t.Duration)
	}
	slices.SortStableFunc(unique, func(x, y netease.Track) int { return cmp.Compare(distance(x), distance(y)) })
	return unique
}

func (m *LocalLibrary) ConfirmLyrics(d *LocalLyricMatch, s LocalLyricsServices) {
	if !m.matchingLyrics(d, s) || d.Selected <= 0 || d.Searching || d.Loading {
		return
	}
	if !d.Track.Temporary && m.Song(d.Track.LocalID).Path == "" {
		d.Status = "该歌曲已从曲库移除"
		return
	}
	d.Loading, d.Status = true, ""
	id, localID, temporary := d.Selected, d.Track.LocalID, d.Track.Temporary
	path, fetch := m.LyricPath(localID), s.Fetch
	go func() {
		original, translation, words, err := fetch(id)
		kept := lyrics.Text{Original: original, Translation: translation, Words: words}
		_, display := lyrics.BuildDisplay(original, translation, words)
		status := ""
		switch {
		case errors.Is(err, netease.ErrOffline):
			status = "联网后可加载歌词"
		case err != nil:
			status = "歌词加载失败，请重试"
		case !display.HasSinging():
			status = "这个版本没有可用的歌词，请选择其他版本"
		case !temporary:
			err := s.Save(path, localID, localcatalog.LyricMatch{SongID: id, Lyrics: kept})
			if errors.Is(err, os.ErrNotExist) {
				status = "该歌曲已从曲库移除"
			} else if err != nil {
				status = "无法保存歌词，请重试"
			}
		}
		s.Post(func() {
			if !m.matchingLyrics(d, s) {
				return
			}
			d.Loading, d.Status = false, status
			if status != "" {
				return
			}
			if temporary {
				m.TemporaryLyrics = kept
			}
			d.Open = false
			s.Reload()
		})
	}()
}
