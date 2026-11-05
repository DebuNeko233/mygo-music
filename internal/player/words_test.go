package player

import (
	"errors"
	"sync/atomic"
	"testing"

	"netease-music-mygo/internal/netease"
)

// A song whose words did not come is not a song without any: the request is
// asked again, and until one answers the words are not said to be there, so
// that nothing calls a song instrumental that only failed to be asked for.
func TestAFailedLyricRequestIsNotAnInstrumental(t *testing.T) {
	b := song(t)
	srv, _ := songServer(t, b)
	serveURL(t, srv, len(b))
	old := lyricsOf
	t.Cleanup(func() { lyricsOf = old })
	var asks atomic.Int32
	lyricsOf = func(id int64) (string, string, string, error) {
		asks.Add(1)
		return "", "", "", errors.New("nothing came")
	}
	c := NewCache(t.TempDir(), 1<<30)
	ch, p := newLoadPlayer(t, c, netease.Track{ID: 1, Duration: 5000})
	p.PlayQueue([]netease.Track{{ID: 1, Duration: 5000}}, 0)

	pump(t, ch, "the words to be given up on", func() bool {
		return !p.Loading() && asks.Load() >= lyricTries
	})
	if lines, loaded := p.Lyrics(); loaded || len(lines) != 0 {
		t.Errorf("says it has %d lines of the words it never got", len(lines))
	}
}

// The words of a song that would not come the first time are asked for
// again, and show once one answers.
func TestTheWordsComeWhenTheyCanBeAskedAgain(t *testing.T) {
	b := song(t)
	srv, _ := songServer(t, b)
	serveURL(t, srv, len(b))
	old := lyricsOf
	t.Cleanup(func() { lyricsOf = old })
	var asks atomic.Int32
	lyricsOf = func(id int64) (string, string, string, error) {
		if asks.Add(1) < lyricTries {
			return "", "", "", errors.New("nothing came")
		}
		return "[00:01.00]你好\n", "", "", nil
	}
	c := NewCache(t.TempDir(), 1<<30)
	ch, p := newLoadPlayer(t, c, netease.Track{ID: 1, Duration: 5000})
	p.PlayQueue([]netease.Track{{ID: 1, Duration: 5000}}, 0)

	pump(t, ch, "the words to come", func() bool {
		lines, loaded := p.Lyrics()
		return loaded && len(lines) == 1
	})
	if lines, _ := p.Lyrics(); lines[0].Text != "你好" {
		t.Errorf("the words are %q", lines[0].Text)
	}
}
