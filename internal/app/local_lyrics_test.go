package app

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/egoist/mygo/ui"

	"netease-music-mygo/internal/localcatalog"
	"netease-music-mygo/internal/lyrics"
	"netease-music-mygo/internal/netease"
	"netease-music-mygo/internal/player"
	"netease-music-mygo/internal/viewmodel"
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
	catalog := localcatalog.Catalog{Songs: []localcatalog.Song{{Track: track, Path: filepath.Join(dir, "recording.mp3")}}}
	a.local.Replace(catalog)
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
	if d := a.local.LyricMatch; len(d.Results) != 2 || d.Results[0].ID != 22 || d.Selected != 0 {
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
	var saved localcatalog.LyricMatch
	if b, err := os.ReadFile(a.local.LyricPath(track.LocalID)); err != nil || json.Unmarshal(b, &saved) != nil || saved.SongID != 22 {
		t.Fatalf("wrong match on disk: %+v, %v", saved, err)
	}

	reopened := newTestApp()
	reopened.pl = player.New(reopened.post)
	reopened.initLocalMusic()
	reopened.local.Replace(catalog)
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
	a.local.Temporary = localcatalog.Song{Track: track, Path: filepath.Join(t.TempDir(), "temp.flac")}
	a.pl.Cue([]netease.Track{track})
	a.pl.RetryMetadata()
	wait(t, posts, "temporary metadata")()
	d := &viewmodel.LocalLyricMatch{Open: true, Track: a.pl.Current(), Selected: 12}
	a.local.LyricMatch = d
	old := lyricsRequest
	lyricsRequest = func(id int64) (string, string, string, error) { return "[00:00.00]试听歌词", "", "", nil }
	t.Cleanup(func() { lyricsRequest = old })
	a.confirmLocalLyricMatch(d)
	wait(t, posts, "temporary match")()
	wait(t, posts, "temporary lyrics")()
	if lines, ok := a.pl.Lyrics(); !ok || len(lines) != 1 || lines[0].Text != "试听歌词" {
		t.Fatalf("temporary lyrics weren't displayed: %+v", lines)
	}
	if _, err := os.Stat(a.local.LyricPath(track.LocalID)); !os.IsNotExist(err) {
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
	a.local.Replace(localcatalog.Catalog{Folders: []string{folder}, Songs: []localcatalog.Song{
		{Track: removed, Scanned: true, Path: filepath.Join(folder, "song.mp3")},
		{Track: kept, Path: filepath.Join(dir, "relinked.mp3")},
	}})
	a.pl.Cue([]netease.Track{removed})
	for _, id := range []int64{removed.LocalID, kept.LocalID} {
		if err := a.local.LyricCache.Save(a.local.LyricPath(id), id, localcatalog.LyricMatch{SongID: id}); err != nil {
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
	dialog := &viewmodel.LocalLyricMatch{Open: true, Selected: 10, Track: a.pl.Current()}
	a.local.LyricMatch = dialog
	a.confirmLocalLyricMatch(dialog)
	wait(t, started, "lyric request")
	a.removeLocalFolder(folder)
	for a.local.Busy {
		wait(t, posts, "folder removal")()
	}
	finish <- struct{}{}
	wait(t, posts, "late lyric response")()
	if dialog.Status != "该歌曲已从曲库移除" {
		t.Fatalf("late lyric response status was %q", dialog.Status)
	}
	if _, err := os.Stat(a.local.LyricPath(removed.LocalID)); !os.IsNotExist(err) {
		t.Fatalf("removed track kept its cache: %v", err)
	}
	if _, err := os.Stat(a.local.LyricPath(kept.LocalID)); err != nil {
		t.Fatalf("the remaining track lost its cache: %v", err)
	}
}

// The separated lyric form retains typed input across rebuilds and Escape retires a queued search result.
func TestLocalLyricsDialogInputAndDismissal(t *testing.T) {
	a := newTestApp()
	track := netease.Track{LocalID: 1, Name: "recording"}
	a.pl.Cue([]netease.Track{track})
	posts := testPosts(a)
	old := searchLocalLyrics
	queries := make(chan string, 2)
	searchLocalLyrics = func(query string, _ int) ([]netease.Track, error) {
		queries <- query
		return []netease.Track{{ID: 7, Name: "version"}}, nil
	}
	t.Cleanup(func() { searchLocalLyrics = old })
	d := &viewmodel.LocalLyricMatch{Open: true, Track: a.pl.Current(), Query: "initial"}
	a.local.LyricMatch = d
	tt := ui.NewTester(func(c *ui.Context) { a.pal = paletteFor(c.Theme()); a.localLyricMatchDialog(c) }, 780, 600)
	tt.Frame()
	if err := tt.Click("歌名和歌手"); err != nil {
		t.Fatal(err)
	}
	tt.Key(ui.Cmd, ui.KeyA)
	tt.Type("new query")
	tt.Frame()
	tt.Frame()
	if d.Query != "new query" {
		t.Fatal("form input was recreated during rebuild")
	}
	tt.Key(0, ui.KeyEnter)
	tt.Frame()
	if query := wait(t, queries, "submitted query"); query != "new query" {
		t.Fatalf("searched %q", query)
	}
	pending := wait(t, posts, "pending lyric search")
	tt.Key(0, ui.KeyEscape)
	tt.Frame()
	if d.Open {
		t.Fatal("Escape did not update the ViewModel's dialog state")
	}
	a.local.LyricMatch = &viewmodel.LocalLyricMatch{Open: true, Track: a.pl.Current(), Query: "reopened"}
	tt.Frame()
	pending()
	tt.Frame()
	if !a.local.LyricMatch.Open || len(a.local.LyricMatch.Results) != 0 || a.local.LyricMatch.Query != "reopened" {
		t.Fatal("closed dialog's response overwrote the reopened form")
	}
}
