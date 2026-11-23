package playback

import (
	"time"

	"netease-music-mygo/internal/lyrics"
)

const LyricBrowseDelay = 4 * time.Second
const LyricInitialFollow = -2
const LyricResumeFollow = -3

// LyricReading tracks intent and source identity, not measured geometry or animation state.
type LyricReading struct {
	At                              int
	Track                           int64
	First                           *lyrics.Line
	SeekRevision, ClickSeekRevision uint64
	BrowseUntil                     time.Time
}

func (m *LyricReading) Select(track int64, first *lyrics.Line) bool {
	if m.Track == track && m.First == first {
		return false
	}
	m.Track, m.First = track, first
	m.Seek()
	return true
}
func (m *LyricReading) FollowSeek(revision uint64, immersive bool) int {
	if revision == m.SeekRevision {
		return 0
	}
	m.SeekRevision = revision
	if revision == m.ClickSeekRevision && m.At != LyricInitialFollow {
		m.Resume()
		return LyricResumeFollow
	}
	if immersive {
		m.Seek()
		return LyricInitialFollow
	}
	return 0
}
func (m *LyricReading) Browse(now time.Time) { m.BrowseUntil = now.Add(LyricBrowseDelay) }
func (m *LyricReading) Browsing(now time.Time, playing bool) (browsing bool, after time.Duration, resumed bool) {
	if m.BrowseUntil.IsZero() {
		return
	}
	if !playing {
		return true, 0, false
	}
	if left := m.BrowseUntil.Sub(now); left > 0 {
		return true, left, false
	}
	m.Resume()
	return false, 0, true
}
func (m *LyricReading) Resume() { m.BrowseUntil = time.Time{}; m.At = LyricResumeFollow }
func (m *LyricReading) Seek()   { m.BrowseUntil = time.Time{}; m.At = LyricInitialFollow }
