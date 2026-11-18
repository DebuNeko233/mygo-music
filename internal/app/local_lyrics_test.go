package app

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/egoist/mygo/ui"

	"netease-music-mygo/internal/lyrics"
	"netease-music-mygo/internal/netease"
	"netease-music-mygo/internal/player"
)

// Matching requires a version confirmation, keeps playback intact, and
// restores the selected lyrics offline until a same-name LRC takes priority.
func TestLocalLyricsMatchAndRestore(t *testing.T) {
	isolateUserData(t)
	a := newTestApp()
	a.settings = defaultSettings()
	a.settings.LyricEdgeFade, a.settings.HotComments = false, false
	a.pl = player.New(a.post)
	a.initLocalMusic()
	posts := testPosts(a)
	dir := t.TempDir()
	track := netease.Track{LocalID: 4, Name: "试唱", Artists: []netease.Artist{{Name: "测试乐队"}}, Duration: 180000}
	catalog := localCatalog{Songs: []localSong{{Track: track, Path: filepath.Join(dir, "recording.mp3")}}}
	a.local.replace(catalog)
	a.pl.Cue([]netease.Track{track})
	a.pl.Hold(time.Second)
	a.pl.RetryMetadata()
	wait(t, posts, "local lyrics")()
	if view, loaded := a.pl.LyricsDisplay(); !loaded || view.EmptyText() != lyrics.UnavailableText {
		t.Fatal("a song without lyrics was not marked unavailable")
	}

	oldSearch, oldLyrics := searchLocalLyrics, lyricsRequest
	queries, chosen := make(chan string, 2), make(chan int64, 2)
	searchLocalLyrics = func(q string, _ int) ([]netease.Track, error) {
		queries <- q
		return []netease.Track{{ID: 21, Name: "现场版", Duration: 205000}, {ID: 22, Name: "录音室版", Duration: 180000}}, nil
	}
	lyricsRequest = func(id int64) (string, string, string, error) {
		chosen <- id
		return "[00:00.10]匹配歌词", "[00:00.10]译文", "", nil
	}
	t.Cleanup(func() { searchLocalLyrics, lyricsRequest = oldSearch, oldLyrics })
	tt := ui.NewTesterWith(func(c *ui.Context) {
		a.pal = paletteFor(c.Theme())
		_, h := c.Size()
		ui.Column(c).Fill().Children(func() { a.lyricsPane(c, a.pl.Current(), h, ui.Rect{}, 0) })
		a.localLyricMatchDialog(c)
	}, 780, 600, ui.TesterOptions{Preferences: ui.Preferences{ReduceMotion: true}})
	settleTallLyrics(tt)
	if err := tt.Click("从网易云匹配歌词"); err != nil {
		t.Fatal(err)
	}
	if q := wait(t, queries, "search query"); q != "试唱 测试乐队" {
		t.Fatalf("searched %q", q)
	}
	wait(t, posts, "search results")()
	if d := a.local.lyricDialog; len(d.results) != 2 || d.results[0].ID != 22 || d.selected != 0 {
		t.Fatalf("results weren't ranked by duration or selected in advance: %+v", d)
	}
	tt.Frame()
	if err := tt.Click("录音室版 3:00"); err != nil {
		t.Fatal(err)
	}
	select {
	case <-chosen:
		t.Fatal("fetch started before confirmation")
	default:
	}
	if err := tt.Click("使用此版本"); err != nil {
		t.Fatal(err)
	}
	if id := wait(t, chosen, "confirmed song"); id != 22 {
		t.Fatalf("fetched version %d", id)
	}
	wait(t, posts, "match saved")()
	wait(t, posts, "matched lyrics applied")()
	wantLyrics := func(a *app, text, translation string) {
		t.Helper()
		if lines, ok := a.pl.Lyrics(); !ok || len(lines) != 1 || lines[0].Text != text || lines[0].Translation != translation {
			t.Fatalf("got lyrics %+v, want %q / %q", lines, text, translation)
		}
	}
	wantLyrics(a, "匹配歌词", "译文")
	if a.pl.Current().LocalID != track.LocalID || a.pl.Position() != time.Second || !a.pl.Playing() {
		t.Fatal("matching lyrics interrupted local playback")
	}
	var saved localLyricMatch
	if b, err := os.ReadFile(a.localLyricPath(track.LocalID)); err != nil || json.Unmarshal(b, &saved) != nil || saved.SongID != 22 {
		t.Fatalf("wrong match on disk: %+v, %v", saved, err)
	}

	reopened := newTestApp()
	reopened.pl = player.New(reopened.post)
	reopened.initLocalMusic()
	reopened.local.replace(catalog)
	reopened.network.offline = true
	reopenedPosts := testPosts(reopened)
	reopened.pl.Cue([]netease.Track{track})
	reopened.pl.RetryMetadata()
	wait(t, reopenedPosts, "offline cached lyrics")()
	wantLyrics(reopened, "匹配歌词", "译文")
	if err := os.WriteFile(filepath.Join(dir, "recording.lrc"), []byte("[00:00.10]同名歌词"), 0600); err != nil {
		t.Fatal(err)
	}
	reopened.pl.ReloadLocalLyrics()
	wait(t, reopenedPosts, "local LRC")()
	wantLyrics(reopened, "同名歌词", "")
}

// A confirmed temporary match is available in memory and doesn't create a library cache.
func TestLocalLyricsTemporaryMatchStaysInMemory(t *testing.T) {
	isolateUserData(t)
	a := newTestApp()
	a.pl = player.New(a.post)
	a.initLocalMusic()
	posts := testPosts(a)
	track := netease.Track{LocalID: 1, Temporary: true}
	a.local.temporary = localSong{Track: track, Path: filepath.Join(t.TempDir(), "temp.flac")}
	a.pl.Cue([]netease.Track{track})
	a.pl.RetryMetadata()
	wait(t, posts, "temporary metadata")()
	d := &localLyricDialog{open: true, track: a.pl.Current(), selected: 12}
	a.local.lyricDialog = d
	old := lyricsRequest
	lyricsRequest = func(id int64) (string, string, string, error) { return "[00:00.00]试听歌词", "", "", nil }
	t.Cleanup(func() { lyricsRequest = old })
	a.confirmLocalLyricMatch(d)
	wait(t, posts, "temporary match")()
	wait(t, posts, "temporary lyrics")()
	if lines, ok := a.pl.Lyrics(); !ok || len(lines) != 1 || lines[0].Text != "试听歌词" {
		t.Fatalf("temporary lyrics weren't displayed: %+v", lines)
	}
	if _, err := os.Stat(a.localLyricPath(track.LocalID)); !os.IsNotExist(err) {
		t.Fatalf("temporary match wrote to disk: %v", err)
	}
}

// Removing a folder clears its lyrics cache and a pending match can't restore it.
func TestLocalLyricsRemovedFolderDoesNotKeepMatches(t *testing.T) {
	isolateUserData(t)
	a := newTestApp()
	a.initLocalMusic()
	posts := testPosts(a)
	dir := t.TempDir()
	folder := filepath.Join(dir, "music")
	removed, kept := netease.Track{LocalID: 1}, netease.Track{LocalID: 2}
	a.local.replace(localCatalog{Folders: []string{folder}, Songs: []localSong{
		{Track: removed, Scanned: true, Path: filepath.Join(folder, "song.mp3")},
		{Track: kept, Path: filepath.Join(dir, "relinked.mp3")},
	}})
	a.pl.Cue([]netease.Track{removed})
	for _, id := range []int64{removed.LocalID, kept.LocalID} {
		if err := a.local.lyricCache.save(a.localLyricPath(id), id, localLyricMatch{SongID: id}); err != nil {
			t.Fatal(err)
		}
	}
	old := lyricsRequest
	started, finish := make(chan struct{}), make(chan struct{}, 1)
	lyricsRequest = func(id int64) (string, string, string, error) {
		close(started)
		<-finish
		return "[00:00.00]迟到的匹配", "", "", nil
	}
	t.Cleanup(func() { close(finish); lyricsRequest = old })
	dialog := &localLyricDialog{open: true, selected: 10, track: a.pl.Current()}
	a.local.lyricDialog = dialog
	a.confirmLocalLyricMatch(dialog)
	wait(t, started, "lyric request")
	a.removeLocalFolder(folder)
	for a.local.busy {
		wait(t, posts, "folder removal")()
	}
	finish <- struct{}{}
	wait(t, posts, "late lyric response")()
	if dialog.status != "该歌曲已从曲库移除" {
		t.Fatalf("late lyric response status was %q", dialog.status)
	}
	if _, err := os.Stat(a.localLyricPath(removed.LocalID)); !os.IsNotExist(err) {
		t.Fatalf("removed track kept its cache: %v", err)
	}
	if _, err := os.Stat(a.localLyricPath(kept.LocalID)); err != nil {
		t.Fatalf("the remaining track lost its cache: %v", err)
	}
}
