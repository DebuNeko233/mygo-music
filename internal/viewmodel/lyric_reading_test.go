package viewmodel

import (
	"testing"
	"time"

	"netease-music-mygo/internal/lyrics"
)

// Paused browsing stays put; resuming after the deadline restores following the same line.
func TestLyricReadingPauseAndResume(t *testing.T) {
	now := time.Unix(100, 0)
	m := LyricReading{At: 8}
	m.Browse(now)
	if browsing, after, _ := m.Browsing(now.Add(time.Hour), false); !browsing || after != 0 || m.At != 8 {
		t.Fatal("paused browsing was reset")
	}
	if browsing, after, resumed := m.Browsing(now.Add(time.Hour), true); browsing || after != 0 || !resumed || m.At != LyricResumeFollow || !m.BrowseUntil.IsZero() {
		t.Fatal("following was not restored")
	}
}

// A clicked seek follows smoothly, while a refreshed lyric source requires the initial placement again.
func TestLyricReadingSourceAndSeekIdentity(t *testing.T) {
	first := &lyrics.Line{Text: "a"}
	m := LyricReading{At: 9, Track: 7, First: first, ClickSeekRevision: 2}
	m.Browse(time.Unix(100, 0))
	if m.Select(7, first) {
		t.Fatal("unchanged source reset reading")
	}
	if action := m.FollowSeek(2, true); action != LyricResumeFollow {
		t.Fatal("clicked line did not resume smoothly")
	}
	if m.FollowSeek(2, true) != 0 {
		t.Fatal("same seek applied twice")
	}
	if !m.Select(7, &lyrics.Line{Text: "b"}) || m.At != LyricInitialFollow {
		t.Fatal("new lyrics kept stale reading position")
	}
}
