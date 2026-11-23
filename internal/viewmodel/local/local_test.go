package local

import (
	"errors"
	"os"
	"path/filepath"
	"sync"
	"testing"

	"netease-music-mygo/internal/localcatalog"
	"netease-music-mygo/internal/lyrics"
	"netease-music-mygo/internal/netease"
	"netease-music-mygo/internal/player"
	"netease-music-mygo/internal/viewmodel/internal/testutil"
)

// A catalog edit must not publish or remove lyrics until its write succeeds; concurrent edits are ignored.
func TestLocalCatalogCommitAndFailure(t *testing.T) {
	for _, success := range []bool{false, true} {
		name := "write-failed"
		if success {
			name = "saved"
		}
		t.Run(name, func(t *testing.T) {
			m := LocalLibrary{Path: filepath.Join(t.TempDir(), "local-music.json")}
			track := netease.Track{LocalID: 1, Name: "kept until committed"}
			m.Replace(localcatalog.Catalog{LastID: 9, Songs: []localcatalog.Song{{Track: track, Path: "/music/one.mp3"}, {Track: netease.Track{LocalID: 2}, Path: "/music/two.mp3"}}})
			lyricPath := m.LyricPath(1)
			if err := m.LyricCache.Save(lyricPath, 1, localcatalog.LyricMatch{SongID: 7}); err != nil {
				t.Fatal(err)
			}
			original := m.Catalog
			posts := make(chan func(), 4)
			saved := make(chan localcatalog.Catalog, 2)
			finish := make(chan struct{})
			release := sync.OnceFunc(func() { close(finish) })
			t.Cleanup(release)
			updates := 0
			s := LocalServices{
				Post: func(fn func()) { posts <- fn }, Changed: func() {}, UpdateTracks: func([]netease.Track) { updates++ },
				Check: func([]localcatalog.Song, map[string]struct{}) map[int64]bool { return nil },
				Save:  func(_ string, value localcatalog.Catalog) bool { saved <- value; <-finish; return success },
			}
			m.RemoveSong(1, s)
			staged := testutil.Wait(t, saved, "staged catalog")
			if !m.Busy || len(m.Catalog.Songs) != 2 || len(original.Songs) != 2 || len(staged.Songs) != 1 || staged.Songs[0].Track.LocalID != 2 {
				t.Fatal("worker mutated the published catalog")
			}
			m.RemoveSong(2, s)
			if _, err := os.Stat(lyricPath); err != nil {
				t.Fatal("removed lyrics before catalog commit", err)
			}
			release()
			apply := testutil.Wait(t, posts, "saved catalog")
			if len(m.Tracks) != 2 || updates != 0 {
				t.Fatal("worker published outside Post")
			}
			apply()
			if m.Busy || updates != 1 || m.Catalog.LastID != 9 {
				t.Fatal("edit did not finish once or reused an allocated ID")
			}
			select {
			case <-saved:
				t.Fatal("overlapping edit started another write")
			default:
			}
			_, err := os.Stat(lyricPath)
			if success {
				if len(m.Tracks) != 1 || m.Song(1).Path != "" || !os.IsNotExist(err) {
					t.Fatal("committed removal did not update indexes and lyrics", err)
				}
				if err := m.LyricCache.Save(lyricPath, 1, localcatalog.LyricMatch{SongID: 8}); !errors.Is(err, os.ErrNotExist) {
					t.Fatal("late match recreated deleted lyrics", err)
				}
			} else if len(m.Tracks) != 2 || m.Song(1).Track.Name != track.Name || err != nil || m.Status == "" {
				t.Fatal("failed write changed the live catalog, removed lyrics or hid the error")
			}
		})
	}
}

// A file check queued before an edit cannot mark songs missing in the committed catalog.
func TestLocalCheckRejectsPreEditResult(t *testing.T) {
	m := LocalLibrary{Path: filepath.Join(t.TempDir(), "local-music.json")}
	m.Replace(localcatalog.Catalog{Songs: []localcatalog.Song{{Track: netease.Track{LocalID: 1}}, {Track: netease.Track{LocalID: 2}}}})
	posts := make(chan func(), 4)
	s := LocalServices{Post: func(fn func()) { posts <- fn }, Changed: func() {}, UpdateTracks: func([]netease.Track) {}, Save: func(string, localcatalog.Catalog) bool { return true },
		Check: func(_ []localcatalog.Song, present map[string]struct{}) map[int64]bool {
			if present == nil {
				return map[int64]bool{1: true, 2: true}
			}
			return map[int64]bool{}
		},
	}
	m.CheckFiles(s)
	old := testutil.Wait(t, posts, "old file check")
	m.RemoveSong(1, s)
	current := testutil.Wait(t, posts, "catalog removal")
	old()
	if len(m.Missing) != 0 {
		t.Fatal("old file check published during an edit")
	}
	current()
	old()
	if len(m.Missing) != 0 || len(m.Tracks) != 1 || m.Tracks[0].LocalID != 2 {
		t.Fatal("old check overwrote the committed availability")
	}
}

// Relinking publishes the new path and metadata under the original queue identity after persistence.
func TestLocalRelinkUpdatesPlaybackMetadata(t *testing.T) {
	path, err := filepath.Abs("../../player/testdata/seek.flac")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(path); err != nil {
		t.Fatal("relink fixture is unavailable:", err)
	}
	m := LocalLibrary{Path: filepath.Join(t.TempDir(), "local-music.json")}
	m.Replace(localcatalog.Catalog{Songs: []localcatalog.Song{{Track: netease.Track{LocalID: 7, Name: "old title"}, Path: "/missing/old.flac"}}})
	posts := make(chan func(), 1)
	var updated []netease.Track
	s := LocalServices{Post: func(fn func()) { posts <- fn }, Changed: func() {}, Check: localcatalog.MissingFiles, Save: func(string, localcatalog.Catalog) bool { return true }, UpdateTracks: func(tracks []netease.Track) { updated = tracks }}
	m.Relink(7, path, s)
	apply := testutil.Wait(t, posts, "relinked metadata")
	if m.Song(7).Path != "/missing/old.flac" || len(updated) != 0 {
		t.Fatal("relink published before its dispatcher")
	}
	apply()
	if m.Song(7).Path != path || len(updated) != 1 || updated[0].LocalID != 7 || updated[0].Duration <= 0 || m.Missing[7] {
		t.Fatal("relink lost the stable identity or playback metadata")
	}
}

// A superseded or quitting open releases its prepared decoder and never changes the active temporary track.
func TestLocalTemporaryOpenRejectsRetiredResult(t *testing.T) {
	for _, quitting := range []bool{false, true} {
		name := "superseded"
		if quitting {
			name = "quitting"
		}
		t.Run(name, func(t *testing.T) {
			var m LocalLibrary
			posts := make(chan func(), 2)
			oldAudio, newAudio := &player.LocalAudio{}, &player.LocalAudio{}
			stopped := false
			released := map[*player.LocalAudio]int{}
			played := 0
			s := LocalOpenServices{
				Post: func(fn func()) { posts <- fn }, Stopped: func() bool { return stopped },
				Prepare: func(path string) (localcatalog.Song, *player.LocalAudio) {
					if path == "old" {
						return localcatalog.Song{Path: path}, oldAudio
					}
					return localcatalog.Song{Path: path}, newAudio
				},
				Release: func(audio *player.LocalAudio) { released[audio]++ },
				Play: func(_, song localcatalog.Song, audio *player.LocalAudio) {
					played++
					if song.Path != "new" || audio != newAudio {
						t.Fatal("played a retired decoder")
					}
				},
				Notify: func(string) { t.Fatal("retired request changed the toast") },
			}
			m.OpenTemporary("old", s)
			old := testutil.Wait(t, posts, "old decoder")
			if quitting {
				stopped = true
			} else {
				m.OpenTemporary("new", s)
				testutil.Wait(t, posts, "current decoder")()
			}
			old()
			if released[oldAudio] != 1 || released[newAudio] != 0 {
				t.Fatal("decoder was leaked or released after ownership transfer")
			}
			if quitting && played != 0 || !quitting && (played != 1 || m.Temporary.Path != "new") {
				t.Fatal("obsolete request changed playback")
			}
		})
	}
}

// Known temporary files reuse local IDs, unknown ones get distinct descending IDs, and corrupt files leave playback intact.
func TestLocalTemporaryIdentityAndInvalidOpen(t *testing.T) {
	var m LocalLibrary
	m.Replace(localcatalog.Catalog{LastID: 3, Songs: []localcatalog.Song{{Track: netease.Track{LocalID: 3}, Path: "known"}}})
	posts := make(chan func(), 1)
	previous := ""
	var ids []int64
	notified := false
	s := LocalOpenServices{
		Post: func(fn func()) { posts <- fn }, Stopped: func() bool { return false }, Release: func(*player.LocalAudio) { t.Fatal("released transferred audio") },
		Prepare: func(path string) (localcatalog.Song, *player.LocalAudio) {
			if path == "bad" {
				return localcatalog.Song{}, nil
			}
			return localcatalog.Song{Path: path}, &player.LocalAudio{}
		},
		Play: func(old, song localcatalog.Song, _ *player.LocalAudio) {
			if old.Path != previous || m.Temporary.Path != song.Path {
				t.Fatal("temporary replacement published in the wrong order")
			}
			previous = song.Path
			ids = append(ids, song.Track.LocalID)
		},
		Notify: func(string) { notified = true },
	}
	for _, path := range []string{"known", "one", "two", "bad"} {
		m.OpenTemporary(path, s)
		testutil.Wait(t, posts, "opened file")()
	}
	if len(ids) != 3 || ids[0] != 3 || ids[1] <= 3 || ids[2] != ids[1]-1 || !notified || m.Temporary.Path != "two" || len(m.Catalog.Songs) != 1 {
		t.Fatal("temporary playback changed catalog identity or accepted an invalid file")
	}
	m.TemporaryLyrics = lyrics.Text{Original: "first"}
	loader := m.LyricsLoader(netease.Track{Temporary: true})
	m.TemporaryLyrics = lyrics.Text{Original: "next"}
	if original, _, _ := loader(); original != "first" {
		t.Fatal("worker loader read the next temporary track's mutable lyrics")
	}
}

// A corrupt catalog disables edits and preserves its bytes instead of replacing the user's library with an empty file.
func TestLocalLoadRejectsCorruptCatalog(t *testing.T) {
	path := filepath.Join(t.TempDir(), "local-music.json")
	if err := os.WriteFile(path, []byte("corrupt catalog"), 0600); err != nil {
		t.Fatal(err)
	}
	var m LocalLibrary
	m.Load(path)
	if m.Path != "" || m.Status == "" || len(m.Tracks) != 0 {
		t.Fatal("corrupt catalog enabled editing")
	}
	m.RemoveSong(1, LocalServices{Save: func(string, localcatalog.Catalog) bool { t.Fatal("corrupt catalog was overwritten"); return true }})
	if b, err := os.ReadFile(path); err != nil || string(b) != "corrupt catalog" {
		t.Fatal("failed startup modified the existing catalog")
	}
}
