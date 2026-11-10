package app

import (
	"context"
	"errors"
	"testing"

	"github.com/egoist/mygo/ui"

	"netease-music-mygo/internal/netease"
)

func musicLinkApp(t *testing.T) *app {
	t.Helper()
	isolateUserData(t)
	a := newTestApp()
	// Keep audio suspended while exercising real queue changes, so fixtures
	// never start a network request or open the system audio device.
	a.pl.SuspendCache()
	a.settings = defaultSettings()
	a.albums.set(34, &albumData{album: &netease.AlbumProfile{Album: netease.Album{ID: 34}}})
	a.playlists[12] = &netease.Playlist{ID: 12}
	return a
}

// Pasting a URL and pressing Enter opens the resource, bypasses suggestions
// and keyword search, and keeps URLs out of search history.
func TestMusicLinkSearchEnter(t *testing.T) {
	oldResults, oldSuggest := searchResults, searchSuggest
	t.Cleanup(func() { searchResults, searchSuggest = oldResults, oldSuggest })
	searched := make(chan string, 8)
	searchResults = func(q string, _ netease.SearchType, _, _ int) (netease.SearchResult, error) {
		searched <- q
		return netease.SearchResult{}, nil
	}
	searchSuggest = func(q string) (netease.Suggestions, error) { searched <- q; return netease.Suggestions{}, nil }
	for _, tc := range []struct {
		input string
		want  route
	}{
		{"https://music.163.com/playlist?id=12", route{page: pagePlaylist, id: 12}},
		{"https://music.163.com/#/album?id=34", route{page: pageAlbum, id: 34}},
		{"分享歌单：https://music.163.com/playlist?id=12（来自网易云音乐）", route{page: pagePlaylist, id: 12}},
		{"orpheus://album/34", route{page: pageAlbum, id: 34}},
	} {
		t.Run(tc.input, func(t *testing.T) {
			a := musicLinkApp(t)
			a.settings.SearchHistory = []string{"春日影"}
			tt := ui.NewTester(a.view, 1200, 800)
			tt.Frame()
			if err := tt.Click("搜索"); err != nil {
				t.Fatal(err)
			}
			tt.Type(tc.input)
			tt.Key(0, ui.KeyEnter)
			tt.Frame()
			if a.route != tc.want || a.nowPlaying {
				t.Fatalf("opened %+v, nowPlaying=%v", a.route, a.nowPlaying)
			}
			if len(a.settings.SearchHistory) != 1 || a.settings.SearchHistory[0] != "春日影" {
				t.Fatal("URL entered search history")
			}
			a.dismissSuggestions()
		})
	}
	select {
	case q := <-searched:
		t.Fatalf("link sent to keyword search: %q", q)
	default:
	}
}

// Invalid resource links leave navigation and playback alone and report
// the error rather than silently searching for the URL.
func TestMusicLinkInvalidInput(t *testing.T) {
	a := musicLinkApp(t)
	before := a.route
	if !a.openMusicInput("orpheus://song/-1") || a.toast == "" || a.route != before {
		t.Fatal("invalid link changed route or failed silently")
	}
	if a.openMusicInput("春日影") {
		t.Fatal("ordinary query consumed")
	}
}

// Newer links cancel older lookups; even a response already in the UI queue
// must not replace the song selected by the newer link.
func TestMusicLinkLatestWins(t *testing.T) {
	old := musicLinkTrack
	t.Cleanup(func() { musicLinkTrack = old })
	a := musicLinkApp(t)
	posts := testPosts(a)
	type asked struct {
		ctx   context.Context
		link  netease.MusicLink
		reply chan netease.Track
	}
	requests := make(chan asked, 2)
	musicLinkTrack = func(ctx context.Context, link netease.MusicLink) (netease.Track, error) {
		reply := make(chan netease.Track, 1)
		requests <- asked{ctx, link, reply}
		return <-reply, nil
	}
	a.openMusicInput("orpheus://song/1")
	first := wait(t, requests, "first link")
	a.openMusicInput("orpheus://song/2")
	second := wait(t, requests, "second link")
	if first.ctx.Err() == nil {
		t.Fatal("old request not cancelled")
	}
	second.reply <- netease.Track{ID: 2, Name: "second"}
	wait(t, posts, "second result")()
	first.reply <- netease.Track{ID: 1, Name: "first"}
	wait(t, posts, "first result")()
	if tr := a.pl.Current(); tr == nil || tr.ID != 2 || !a.nowPlaying {
		t.Fatalf("wrong linked song: %+v", tr)
	}
}

// A failed lookup and a later manual selection must preserve the current
// queue; navigating away also cancels the outstanding link.
func TestMusicLinkFailureAndNavigation(t *testing.T) {
	old := musicLinkTrack
	t.Cleanup(func() { musicLinkTrack = old })
	a := musicLinkApp(t)
	a.pl.Cue([]netease.Track{{ID: 7, Name: "original"}})
	posts := testPosts(a)
	musicLinkTrack = func(context.Context, netease.MusicLink) (netease.Track, error) {
		return netease.Track{}, errors.New("offline")
	}
	a.openMusicInput("orpheus://song/1")
	wait(t, posts, "failed result")()
	if a.pl.Current().ID != 7 || a.toast == "" {
		t.Fatal("failed lookup replaced queue or failed silently")
	}
	entered := make(chan context.Context, 1)
	release := make(chan struct{})
	musicLinkTrack = func(ctx context.Context, _ netease.MusicLink) (netease.Track, error) {
		entered <- ctx
		<-release
		return netease.Track{ID: 1}, nil
	}
	a.openMusicInput("orpheus://song/1")
	ctx := wait(t, entered, "link request")
	a.open(route{page: pageAlbum, id: 34})
	if ctx.Err() == nil {
		t.Fatal("navigation did not cancel lookup")
	}
	close(release)
	wait(t, posts, "stale result")()
	if a.pl.Current().ID != 7 || a.nowPlaying {
		t.Fatal("navigation was overwritten by stale link")
	}
}

// Cold-start links wait for session restoration and keep only the newest
// destination; restoring the previous queue must happen before opening it.
func TestMusicLinkColdStart(t *testing.T) {
	a := musicLinkApp(t)
	a.links.starting = true
	a.openMusicInput("orpheus://playlist/12")
	a.openMusicInput("orpheus://album/34")
	if a.route.page != pageHome || a.links.pending == nil {
		t.Fatal("link ran before restoration")
	}
	a.openPendingMusicLink()
	if a.links.starting || a.links.pending != nil || a.route != (route{page: pageAlbum, id: 34}) {
		t.Fatalf("pending link lost: %+v", a.route)
	}
}

// Reopening the current song should focus it without seeking to the start;
// a song already in the queue can be played offline without a metadata call.
func TestMusicLinkReusesQueue(t *testing.T) {
	a := musicLinkApp(t)
	a.pl.Cue([]netease.Track{{ID: 1}, {ID: 2}})
	a.pl.Hold(0)
	a.openMusicInput("orpheus://song/1")
	if len(a.pl.Queue()) != 2 || a.pl.Current().ID != 1 {
		t.Fatal("current song's queue was replaced")
	}
	a.openMusicInput("orpheus://song/2")
	if a.pl.Current().ID != 2 || len(a.pl.Queue()) != 2 || !a.nowPlaying {
		t.Fatal("queued song could not play offline")
	}
}

// Taking over the protocol is an explicit settings action, reports errors,
// and refreshes its real state when another app becomes the handler.
func TestMusicLinkRegistrationSetting(t *testing.T) {
	oldRegister, oldStatus := registerMusicLinks, musicLinksRegistered
	t.Cleanup(func() { registerMusicLinks, musicLinksRegistered = oldRegister, oldStatus })
	registered := false
	attempts := 0
	registerMusicLinks = func() error {
		attempts++
		if attempts == 1 {
			return errors.New("failed")
		}
		registered = true
		return nil
	}
	musicLinksRegistered = func() bool { return registered }
	a := musicLinkApp(t)
	tt := ui.NewTester(a.settingsView, 780, 640)
	tt.Frame()
	if attempts != 0 {
		t.Fatal("rendering claimed the protocol")
	}
	if err := tt.Click("设为网易云链接默认应用"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	if attempts != 1 || a.links.registrationError == "" {
		t.Fatal("registration error not shown")
	}
	if err := tt.Click("设为网易云链接默认应用"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	if !a.links.registered || a.links.registrationError != "" {
		t.Fatal("successful registration not reflected")
	}
	registered = false
	a.refreshMusicLinkHandler()
	tt.Frame()
	if a.links.registered {
		t.Fatal("stale handler state")
	}
}

// A link waiting for metadata cannot override a song the listener manually
// selected while waiting, even though the link itself was not replaced.
func TestMusicLinkRespectsManualPlayback(t *testing.T) {
	old := musicLinkTrack
	t.Cleanup(func() { musicLinkTrack = old })
	a := musicLinkApp(t)
	a.pl.Cue([]netease.Track{{ID: 7}})
	posts := testPosts(a)
	entered := make(chan struct{})
	release := make(chan struct{})
	musicLinkTrack = func(context.Context, netease.MusicLink) (netease.Track, error) {
		close(entered)
		<-release
		return netease.Track{ID: 1}, nil
	}
	a.openMusicInput("orpheus://song/1")
	wait(t, entered, "metadata request")
	a.pl.Cue([]netease.Track{{ID: 8}})
	close(release)
	wait(t, posts, "metadata result")()
	if a.pl.Current().ID != 8 {
		t.Fatal("late link replaced manual playback")
	}
}

// A short share link resolves off the UI thread, then uses exactly the
// same page navigation as a full share link.
func TestMusicLinkShortShareOpensPage(t *testing.T) {
	old := resolveMusicLink
	t.Cleanup(func() { resolveMusicLink = old })
	resolveMusicLink = func(context.Context, netease.MusicLink) (netease.MusicLink, error) {
		return netease.MusicLink{Kind: "album", ID: 34}, nil
	}
	a := musicLinkApp(t)
	posts := testPosts(a)
	a.search("https://163cn.tv/abc")
	wait(t, posts, "resolved share")()
	if a.route != (route{page: pageAlbum, id: 34}) {
		t.Fatalf("short link opened %+v", a.route)
	}
}

// Enter on a pasted song URL plays that exact song through the same
// dispatcher as an external protocol event, rather than searching its title.
func TestMusicLinkSearchPlaysSong(t *testing.T) {
	old := musicLinkTrack
	t.Cleanup(func() { musicLinkTrack = old })
	musicLinkTrack = func(context.Context, netease.MusicLink) (netease.Track, error) {
		return netease.Track{ID: 42, Name: "链接中的歌曲"}, nil
	}
	a := musicLinkApp(t)
	posts := testPosts(a)
	tt := ui.NewTester(a.view, 1200, 800)
	tt.Frame()
	if err := tt.Click("搜索"); err != nil {
		t.Fatal(err)
	}
	tt.Type("https://music.163.com/song?id=42")
	tt.Key(0, ui.KeyEnter)
	wait(t, posts, "song metadata")()
	tt.Frame()
	if tr := a.pl.Current(); tr == nil || tr.ID != 42 || !a.nowPlaying {
		t.Fatalf("song URL did not play: %+v", tr)
	}
	if len(a.settings.SearchHistory) != 0 {
		t.Fatal("song URL entered keyword history")
	}
}
