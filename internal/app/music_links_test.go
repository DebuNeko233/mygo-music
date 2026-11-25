package app

import (
	"context"
	"encoding/json"
	"errors"
	"testing"

	"github.com/egoist/mygo/ui"

	"netease-music-mygo/internal/netease"
	"netease-music-mygo/internal/player"
	"netease-music-mygo/internal/userdata"
	albumvm "netease-music-mygo/internal/viewmodel/album"
)

func musicLinkApp(t *testing.T) *app {
	t.Helper()
	isolateUserData(t)
	a := newTestApp()
	// Keep audio suspended while exercising real queue changes, so fixtures
	// never start a network request or open the system audio device.
	a.pl.SuspendCache()
	a.settings = defaultSettings()
	a.albums.Pages.Set(34, &albumvm.AlbumPage{Album: &netease.AlbumProfile{Album: netease.Album{ID: 34}}})
	a.library.Playlists[12] = &netease.Playlist{ID: 12}
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
		{"https://music.163.com/playlist?id=12", route{Page: pagePlaylist, ID: 12}},
		{"https://music.163.com/#/album?id=34", route{Page: pageAlbum, ID: 34}},
		{"分享歌单：https://music.163.com/playlist?id=12（来自网易云音乐）", route{Page: pagePlaylist, ID: 12}},
		{"orpheus://album/34", route{Page: pageAlbum, ID: 34}},
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
			if a.nav.Current != tc.want || a.playbackState.Open {
				t.Fatalf("opened %+v, nowPlaying=%v", a.nav.Current, a.playbackState.Open)
			}
			if len(a.settings.SearchHistory) != 1 || a.settings.SearchHistory[0] != "春日影" {
				t.Fatal("URL entered search history")
			}
			a.searches.DismissSuggestions()
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
	before := a.nav.Current
	if !a.openMusicInput("orpheus://song/-1") || a.toast == "" || a.nav.Current != before {
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
	if tr := a.pl.Current(); tr == nil || tr.ID != 2 || !a.playbackState.Open {
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
	a.open(route{Page: pageAlbum, ID: 34})
	if ctx.Err() == nil {
		t.Fatal("navigation did not cancel lookup")
	}
	close(release)
	wait(t, posts, "stale result")()
	if a.pl.Current().ID != 7 || a.playbackState.Open {
		t.Fatal("navigation was overwritten by stale link")
	}
}

// Cold-start links wait for session restoration and keep only the newest
// destination; restoring the previous queue must happen before opening it.
func TestMusicLinkColdStart(t *testing.T) {
	a := musicLinkApp(t)
	a.startup.waiting = true
	a.openMusicInput("orpheus://playlist/12")
	a.openMusicInput("orpheus://album/34")
	if a.nav.Current.Page != pageHome || a.startup.pending == nil {
		t.Fatal("link ran before restoration")
	}
	a.restoreSession()
	a.finishStartup()
	if a.startup.waiting || a.startup.pending != nil || a.nav.Current != (route{Page: pageAlbum, ID: 34}) {
		t.Fatalf("pending link lost: %+v", a.nav.Current)
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
	if a.pl.Current().ID != 2 || len(a.pl.Queue()) != 2 || !a.playbackState.Open {
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
	tt := ui.NewTester(a.views.settings.Build, 780, 640)
	tt.Frame()
	if attempts != 0 {
		t.Fatal("rendering claimed the protocol")
	}
	if err := tt.Click("设为网易云链接默认应用"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	if attempts != 1 || a.links.RegistrationError == "" {
		t.Fatal("registration error not shown")
	}
	if err := tt.Click("设为网易云链接默认应用"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	if !a.links.Registered || a.links.RegistrationError != "" {
		t.Fatal("successful registration not reflected")
	}
	registered = false
	a.refreshMusicLinkHandler()
	tt.Frame()
	if a.links.Registered {
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
	if a.nav.Current != (route{Page: pageAlbum, ID: 34}) {
		t.Fatalf("short link opened %+v", a.nav.Current)
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
	if tr := a.pl.Current(); tr == nil || tr.ID != 42 || !a.playbackState.Open {
		t.Fatalf("song URL did not play: %+v", tr)
	}
	if len(a.settings.SearchHistory) != 0 {
		t.Fatal("song URL entered keyword history")
	}
}

// A link started by the old account must not replace playback after account switching.
func TestMusicLinkRejectsAccountChange(t *testing.T) {
	a := musicLinkApp(t)
	posts := testPosts(a)
	old := musicLinkTrack
	t.Cleanup(func() { musicLinkTrack = old })
	musicLinkTrack = func(context.Context, netease.MusicLink) (netease.Track, error) {
		return netease.Track{ID: 77, Name: "old account"}, nil
	}
	a.openMusicInput("orpheus://song/77")
	update := wait(t, posts, "old link")
	a.account.Generation++
	update()
	if a.pl.Current() != nil || a.playbackState.Open {
		t.Fatal("old link crossed account boundary")
	}
}

// Going back while a short link resolves cancels its pending navigation too.
func TestMusicLinkBackCancelsPending(t *testing.T) {
	a := musicLinkApp(t)
	posts := testPosts(a)
	old := resolveMusicLink
	t.Cleanup(func() { resolveMusicLink = old })
	a.open(route{Page: pageAlbum, ID: 34})
	resolveMusicLink = func(context.Context, netease.MusicLink) (netease.MusicLink, error) {
		return netease.MusicLink{Kind: "playlist", ID: 12}, nil
	}
	a.openMusicInput("https://163cn.tv/abc")
	update := wait(t, posts, "short link")
	a.back()
	update()
	if a.nav.Current.Page != pageHome {
		t.Fatalf("pending link replaced back navigation: %+v", a.nav.Current)
	}
}

// A short link also preserves playback selected manually while its redirect is resolving.
func TestMusicLinkShortKeepsManualPlayback(t *testing.T) {
	a := musicLinkApp(t)
	posts := testPosts(a)
	old := resolveMusicLink
	t.Cleanup(func() { resolveMusicLink = old })
	resolveMusicLink = func(context.Context, netease.MusicLink) (netease.MusicLink, error) {
		return netease.MusicLink{Kind: "playlist", ID: 12}, nil
	}
	a.openMusicInput("https://163cn.tv/abc")
	update := wait(t, posts, "short link")
	a.pl.Cue([]netease.Track{{ID: 99}})
	update()
	if a.nav.Current.Page != pageHome || a.pl.Current() == nil || a.pl.Current().ID != 99 {
		t.Fatal("short link overrode manual playback")
	}
}

// Accepting the restored account must keep a cold-start link until its saved queue is available.
func TestMusicLinkColdStartSurvivesAccountRestore(t *testing.T) {
	a := musicLinkApp(t)
	posts := testPosts(a)
	fakeOfflineHome(t)
	saved := player.Session{Queue: []netease.Track{{ID: 1}, {ID: 2}}, Order: []int{0, 1}, At: 0}
	data, err := json.Marshal(saved)
	if err != nil {
		t.Fatal(err)
	}
	path, err := sessionPath()
	if err != nil {
		t.Fatal(err)
	}
	if err := userdata.Write(path, data); err != nil {
		t.Fatal(err)
	}
	a.startup.waiting = true
	a.openMusicInput("orpheus://song/2")
	a.signedIn(nil)
	for a.home.Loading {
		refreshPost(t, posts)
	}
	if a.startup.waiting || a.startup.pending != nil || !a.restored || a.pl.Current() == nil || a.pl.Current().ID != 2 || len(a.pl.Queue()) != 2 || !a.playbackState.Open {
		t.Fatalf("cold-start link lost across restoration: links=%+v, current=%+v", a.links, a.pl.Current())
	}
}

// A later account change cancels an active lookup instead of replaying it as a startup link.
func TestMusicLinkAccountRestoreCancelsLookup(t *testing.T) {
	a := musicLinkApp(t)
	posts := testPosts(a)
	fakeOfflineHome(t)
	a.restored = true
	a.pl.Cue([]netease.Track{{ID: 99}})
	old := musicLinkTrack
	t.Cleanup(func() { musicLinkTrack = old })
	contexts := make(chan context.Context, 1)
	musicLinkTrack = func(ctx context.Context, _ netease.MusicLink) (netease.Track, error) {
		contexts <- ctx
		return netease.Track{ID: 7}, nil
	}
	a.openMusicInput("orpheus://song/7")
	ctx := wait(t, contexts, "link context")
	update := wait(t, posts, "link result")
	a.signedIn(nil)
	update()
	for a.home.Loading {
		refreshPost(t, posts)
	}
	if ctx.Err() == nil || a.pl.Current() == nil || a.pl.Current().ID != 99 || a.playbackState.Open {
		t.Fatal("account change kept old lookup")
	}
}
