package local

import (
	"errors"
	"os"
	"path/filepath"
	"testing"

	"netease-music-mygo/internal/localcatalog"
	"netease-music-mygo/internal/lyrics"
	"netease-music-mygo/internal/netease"
	"netease-music-mygo/internal/viewmodel/internal/testutil"
)

// Out-of-order searches, dismissed dialogs and changed tracks cannot overwrite the active lyric choices.
func TestLocalLyricSearchOwnsItsResult(t *testing.T) {
	var m LocalLibrary
	track := &netease.Track{LocalID: 1, Duration: 180000}
	current := track
	posts := make(chan func(), 4)
	s := LocalLyricsServices{
		Post: func(fn func()) { posts <- fn }, Current: func() *netease.Track { return current },
		Search: func(q string, limit int) ([]netease.Track, error) {
			if limit != 20 {
				t.Errorf("unexpected result limit: %d", limit)
			}
			if q == "old" {
				return []netease.Track{{ID: 1, Name: "old"}}, nil
			}
			return []netease.Track{{ID: 8, Duration: 200000}, {ID: 0}, {ID: 9, Duration: 180000}, {ID: 9, Duration: 180000}, {ID: 10}}, nil
		},
	}
	m.OpenLyricMatch(track, "old", s)
	d := m.LyricMatch
	old := testutil.Wait(t, posts, "old search")
	d.Query = "new"
	if !m.SearchLyrics(d, s) {
		t.Fatal("did not replace a pending search")
	}
	next := testutil.Wait(t, posts, "new search")
	old()
	if !d.Searching || len(d.Results) != 0 {
		t.Fatal("old query overwrote new search")
	}
	next()
	if d.Searching || len(d.Results) != 3 || d.Results[0].ID != 9 || d.Results[1].ID != 8 || d.Results[2].ID != 10 || d.Selected != 0 {
		t.Fatal("results were not deduplicated/ranked or selected without confirmation")
	}
	d.Query = "old"
	m.SearchLyrics(d, s)
	dismissed := testutil.Wait(t, posts, "dismissed search")
	d.Open = false
	m.OpenLyricMatch(track, "new", s)
	reopened := m.LyricMatch
	fresh := testutil.Wait(t, posts, "reopened search")
	dismissed()
	if !reopened.Searching || len(reopened.Results) != 0 {
		t.Fatal("dismissed dialog changed the new dialog")
	}
	current = &netease.Track{LocalID: 2}
	fresh()
	if len(reopened.Results) != 0 {
		t.Fatal("another track accepted old results")
	}
}

// Failed or lyric-less versions keep the match dialog usable, and only a persisted match reloads the player.
func TestLocalLyricConfirmationFailuresAndRetry(t *testing.T) {
	for _, failure := range []string{"offline", "fetch", "empty", "write", "removed", "saved"} {
		t.Run(failure, func(t *testing.T) {
			m := LocalLibrary{Path: filepath.Join(t.TempDir(), "local-music.json")}
			track := &netease.Track{LocalID: 4}
			m.Replace(localcatalog.Catalog{Songs: []localcatalog.Song{{Track: *track, Path: "recording.flac"}}})
			d := &LocalLyricMatch{Open: true, Track: track, Selected: 7}
			m.LyricMatch = d
			posts := make(chan func(), 2)
			saved, reloaded := 0, 0
			s := LocalLyricsServices{
				Post: func(fn func()) { posts <- fn }, Current: func() *netease.Track { return track },
				Fetch: func(int64) (string, string, string, error) {
					switch failure {
					case "offline":
						return "", "", "", netease.ErrOffline
					case "fetch":
						return "", "", "", errors.New("request failed")
					case "empty":
						return "", "", "", nil
					}
					return "[00:00.00]original", "[00:00.00]translated", "", nil
				},
				Save: func(path string, id int64, match localcatalog.LyricMatch) error {
					saved++
					if path != m.LyricPath(4) || id != 4 || match.SongID != 7 || match.Lyrics.Translation == "" {
						t.Error("saved the wrong identity or dropped translation")
					}
					switch failure {
					case "write":
						return os.ErrPermission
					case "removed":
						return os.ErrNotExist
					}
					return nil
				}, Reload: func() { reloaded++ },
			}
			// A second click while loading must not start another fetch/save.
			m.ConfirmLyrics(d, s)
			m.ConfirmLyrics(d, s)
			if !d.Loading || reloaded != 0 {
				t.Fatal("confirmation bypassed asynchronous load")
			}
			testutil.Wait(t, posts, "confirmed lyrics")()
			if failure == "saved" {
				if d.Open || d.Loading || reloaded != 1 || saved != 1 {
					t.Fatal("persisted lyrics did not reload exactly once")
				}
			} else {
				if !d.Open || d.Loading || d.Status == "" || reloaded != 0 {
					t.Fatal("failed confirmation closed the dialog or changed playback")
				}
				if failure == "offline" || failure == "fetch" || failure == "empty" {
					if saved != 0 {
						t.Fatal("failed fetch was persisted")
					}
				}
				s.Fetch = func(int64) (string, string, string, error) { return "[00:00.00]retry", "", "", nil }
				s.Save = func(string, int64, localcatalog.LyricMatch) error { return nil }
				m.ConfirmLyrics(d, s)
				testutil.Wait(t, posts, "retry lyrics")()
				if d.Open || reloaded != 1 {
					t.Fatal("failed request could not be retried")
				}
			}
		})
	}
}

// Closing or changing tracks while lyrics load cannot attach a late temporary match to a new track.
func TestLocalLyricConfirmationRejectsRetiredDialog(t *testing.T) {
	for _, changeTrack := range []bool{false, true} {
		name := "closed"
		if changeTrack {
			name = "changed-track"
		}
		t.Run(name, func(t *testing.T) {
			m := LocalLibrary{TemporaryLyrics: lyrics.Text{Original: "retained"}}
			track := &netease.Track{LocalID: 1, Temporary: true}
			current := track
			d := &LocalLyricMatch{Open: true, Track: track, Selected: 7}
			m.LyricMatch = d
			posts := make(chan func(), 1)
			s := LocalLyricsServices{Post: func(fn func()) { posts <- fn }, Current: func() *netease.Track { return current },
				Fetch: func(int64) (string, string, string, error) { return "[00:00.00]late", "", "", nil },
				Save: func(string, int64, localcatalog.LyricMatch) error {
					t.Error("temporary lyrics wrote to disk")
					return nil
				},
				Reload: func() { t.Fatal("late result reloaded a different track") },
			}
			m.ConfirmLyrics(d, s)
			apply := testutil.Wait(t, posts, "late match")
			if changeTrack {
				current = &netease.Track{LocalID: 2, Temporary: true}
			} else {
				d.Open = false
			}
			apply()
			if m.TemporaryLyrics.Original != "retained" {
				t.Fatal("late result replaced a new track's lyrics")
			}
		})
	}
}

// Search errors clear busy state and permit the same dialog to retry with a useful result.
func TestLocalLyricSearchRetry(t *testing.T) {
	for _, err := range []error{netease.ErrOffline, errors.New("bad response"), nil} {
		var m LocalLibrary
		track := &netease.Track{LocalID: 1}
		posts := make(chan func(), 1)
		s := LocalLyricsServices{Post: func(fn func()) { posts <- fn }, Current: func() *netease.Track { return track }, Search: func(string, int) ([]netease.Track, error) { return nil, err }}
		m.OpenLyricMatch(track, "query", s)
		testutil.Wait(t, posts, "search failure")()
		d := m.LyricMatch
		if d.Searching || d.Status == "" || !d.Open {
			t.Fatal("empty/error result left search busy without feedback")
		}
		s.Search = func(string, int) ([]netease.Track, error) { return []netease.Track{{ID: 7}}, nil }
		m.SearchLyrics(d, s)
		testutil.Wait(t, posts, "search retry")()
		if d.Searching || d.Status != "" || len(d.Results) != 1 {
			t.Fatal("search failed to recover")
		}
	}
}
