package app

import (
	"sync/atomic"
	"testing"
	"time"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"
	"netease-music-mygo/internal/netease"
	"netease-music-mygo/internal/player"
)

// A system disconnect is visible on an idle cached page and cancels retries,
// without waiting for a failed request or changing the playing queue.
func TestNetworkDisconnectOnCachedPage(t *testing.T) {
	a, _ := offlineFixture(t)
	a.route = route{page: pagePlaylist, id: 9}
	pl := &netease.Playlist{ID: 9, Name: "Cached"}
	a.playlists[9] = pl
	a.recovery.timer = time.NewTimer(time.Hour)
	a.networkChanged(mygo.NetworkStatus{Online: false})
	a.recoveryFrame(true)
	if !a.network.offline || a.recovery.timer != nil || a.playlists[9] != pl {
		t.Fatal("idle disconnect was missed or cache was cleared")
	}
	read := readNetworkStatus
	t.Cleanup(func() { readNetworkStatus = read })
	readNetworkStatus = func() mygo.NetworkStatus { return mygo.NetworkStatus{} }
	tt := ui.NewTester(a.view, 1100, 800)
	tt.Frame()
	if err := tt.Click("刷新内容"); err != nil {
		t.Fatal(err)
	}
	if !a.network.offline || a.playlists[9] != pl {
		t.Fatal("offline refresh changed cached page")
	}
}

// Reconnection retries a failed current page once, coalesces a duplicate system
// event and preserves the existing page until the new response arrives.
func TestNetworkReconnectionRecoversCurrentPage(t *testing.T) {
	a, posts := offlineFixture(t)
	a.route = route{page: pagePlaylist, id: 9}
	a.playlists[9] = &netease.Playlist{ID: 9, Name: "Cached"}
	a.playlistRequest(9).failed = true
	a.network = networkState{known: true, offline: true}
	a.recovery.active, a.recovery.attempts = true, 4
	var calls atomic.Int32
	playlistDetail = func(id int64) (*netease.Playlist, error) {
		calls.Add(1)
		return &netease.Playlist{ID: id, Name: "Online"}, nil
	}
	a.networkChanged(mygo.NetworkStatus{Online: true})
	a.networkChanged(mygo.NetworkStatus{Online: true, Metered: true})
	if a.network.offline || a.recovery.attempts != 0 || a.playlists[9].Name != "Cached" {
		t.Fatal("reconnect did not reset recovery or replaced cache prematurely")
	}
	refreshPost(t, posts)
	if calls.Load() != 1 || a.playlists[9].Name != "Online" || a.pl.Current() != nil {
		t.Fatal("reconnect duplicated requests or altered playback")
	}
}

// Clicking the icon rechecks system state; it sends no account request while
// still offline, and starts one when that explicit check observes a connection.
func TestNetworkRefreshIcon(t *testing.T) {
	a, posts := offlineFixture(t)
	a.route = route{page: pagePlaylist, id: 9}
	a.playlists[9] = &netease.Playlist{ID: 9, Name: "Cached"}
	a.network = networkState{known: true, offline: true}
	a.recovery.accountPending = true
	read, ask, save, identity := readNetworkStatus, accountAsk, saveLoginSession, loginIdentity
	t.Cleanup(func() { readNetworkStatus, accountAsk, saveLoginSession, loginIdentity = read, ask, save, identity })
	checks := 0
	readNetworkStatus = func() mygo.NetworkStatus { checks++; return mygo.NetworkStatus{Online: checks > 1} }
	var calls atomic.Int32
	accountAsk = func() (*netease.Profile, error) { calls.Add(1); return &netease.Profile{UserID: 7}, nil }
	saveLoginSession = func() error { return nil }
	loginIdentity = func() string { return "" }
	a.listRefresh.mine.at, a.listRefresh.likes.at = a.listsNow(), a.listsNow()
	a.playlistRequest(9).at = a.listsNow()
	tt := ui.NewTester(a.view, 1100, 800)
	tt.Frame()
	if err := tt.Click("刷新内容"); err != nil {
		t.Fatal(err)
	}
	if !a.network.offline || calls.Load() != 0 || checks != 1 {
		t.Fatal("offline refresh issued an account request")
	}
	tt.Frame()
	if err := tt.Click("刷新内容"); err != nil {
		t.Fatal(err)
	}
	refreshPost(t, posts)
	if a.network.offline || a.recovery.accountPending || calls.Load() != 1 {
		t.Fatal("refresh did not confirm the restored connection")
	}
	tt.Frame()
	if err := tt.Click("刷新内容"); err == nil {
		t.Fatal("healthy connection retained the offline controls")
	}
}

// A hidden window records reconnection without starting background page loads.
func TestNetworkReconnectionWhileHidden(t *testing.T) {
	a, _ := offlineFixture(t)
	a.network = networkState{known: true, offline: true}
	a.recovery.accountPending = true
	a.networkChanged(mygo.NetworkStatus{Online: true})
	if a.network.offline || a.recovery.accountBusy {
		t.Fatal("hidden window started a request or missed reconnection")
	}
}

// Opening either player layout hides the page's network actions immediately;
// they return only after closing, and network events still recover resources.
func TestNetworkControlsStayOutsideNowPlaying(t *testing.T) {
	a, posts := offlineFixture(t)
	a.route = route{page: pagePlaylist, id: 9}
	a.playlists[9] = &netease.Playlist{ID: 9, Name: "Cached"}
	a.pl = player.New(a.post)
	a.pl.Cue([]netease.Track{{ID: 1}})
	a.pl.FetchLyrics = func(int64) (string, string, string, error) { return "", "", "", netease.ErrOffline }
	read, comments := readNetworkStatus, threadComments
	t.Cleanup(func() { readNetworkStatus, threadComments = read, comments })
	status, checks := mygo.NetworkStatus{}, 0
	readNetworkStatus = func() mygo.NetworkStatus { checks++; return status }
	threadComments = func(string, int, int) (*netease.Comments, error) { return nil, netease.ErrOffline }
	a.networkChanged(status)
	tt := ui.NewTester(a.view, 1100, 800)
	tt.Frame()
	if err := tt.Click("刷新内容"); err != nil {
		t.Fatal(err)
	}
	a.nowPlaying = true
	for _, pane := range []int{paneLyrics, paneStage, paneQueue} {
		a.npPane = pane
		tt.Frame()
		if _, found := tt.Find("离线"); found {
			t.Fatal("player retained the offline symbol")
		}
		if err := tt.Click("刷新内容"); err == nil {
			t.Fatal("player retained the page refresh action")
		}
	}
	status.Online = true
	a.networkChanged(status)
	for a.pl.MetadataLoading() || a.comments.loading {
		refreshPost(t, posts)
	}
	tt.Frame()
	if err := tt.Click("刷新内容"); err == nil || a.network.offline || a.pl.Playing() {
		t.Fatal("reconnect exposed page controls or altered playback")
	}
	a.nowPlaying = false
	tt.Frame()
	if err := tt.Click("刷新内容"); err == nil {
		t.Fatal("page controls appeared over the closing player")
	}
	tt.Advance(npSettle + time.Millisecond)
	tt.Frame()
	if err := tt.Click("刷新内容"); err != nil {
		t.Fatal(err)
	}
	for a.pl.MetadataLoading() {
		refreshPost(t, posts)
	}
	if checks != 2 {
		t.Fatal("hidden network actions rechecked connectivity")
	}
}

// Reconnection replaces stale offline status with recovery controls without
// playing audio; another disconnect removes those actions immediately.
func TestNetworkChangesUpdateVisibleComponentStates(t *testing.T) {
	a, posts := offlineFixture(t)
	a.pl = player.New(a.post)
	a.pl.FetchLyrics = func(int64) (string, string, string, error) { return "", "", "", netease.ErrOffline }
	a.networkChanged(mygo.NetworkStatus{})
	a.pl.PlayQueue([]netease.Track{{ID: 1, Name: "Connection test", Duration: 180000}}, 0)
	for a.pl.Loading() || a.pl.MetadataLoading() {
		refreshPost(t, posts)
	}
	tt := ui.NewTester(a.view, 1100, 800)
	tt.Frame()
	if a.pl.PlaybackRetryable() || a.pl.LyricsRetryable() {
		t.Fatal("offered network retries while offline")
	}
	a.recovery.active = false
	a.networkChanged(mygo.NetworkStatus{Online: true})
	tt.Frame()
	if !a.pl.PlaybackRetryable() {
		t.Fatal("reconnect did not offer recovery")
	}
	if _, offline := tt.Find("离线"); offline {
		t.Fatal("connected network retained offline icon")
	}
	if _, play := tt.Find("重试播放"); !play {
		t.Fatal("reconnected playback did not offer retry")
	}
	for a.pl.MetadataLoading() {
		refreshPost(t, posts)
	}
	a.nowPlaying, a.npPane = true, paneLyrics
	tt.Frame()
	tt.Advance(time.Second)
	tt.Frame()
	if _, retry := tt.Find("重新加载歌词"); !retry {
		t.Fatal("missing lyrics recovery action")
	}
	a.networkChanged(mygo.NetworkStatus{})
	tt.Frame()
	if a.pl.PlaybackRetryable() || a.pl.LyricsRetryable() {
		t.Fatal("second disconnect retained network retries")
	}
	if a.pl.Playing() || a.pl.Loading() || a.pl.Current().ID != 1 {
		t.Fatal("status update restarted or changed playback")
	}
}

// Clicking lyric recovery in either layout starts one request, removes the
// action until it finishes, and preserves the selected song and pause state.
func TestNetworkLyricsRetryAction(t *testing.T) {
	for _, pane := range []int{paneLyrics, paneStage} {
		a, posts := offlineFixture(t)
		a.pl = player.New(a.post)
		a.pl.Cue([]netease.Track{{ID: 7, Name: "Recovery"}})
		a.pl.FetchLyrics = func(int64) (string, string, string, error) { return "", "", "", netease.ErrOffline }
		a.networkChanged(mygo.NetworkStatus{})
		a.networkChanged(mygo.NetworkStatus{Online: true})
		a.nowPlaying, a.npPane = true, pane
		tt := ui.NewTester(a.view, 1100, 800)
		tt.Frame()
		for a.pl.MetadataLoading() {
			refreshPost(t, posts)
		}
		tt.Advance(time.Second)
		tt.Frame()
		started, release := make(chan struct{}, 2), make(chan struct{})
		var calls atomic.Int32
		a.pl.FetchLyrics = func(int64) (string, string, string, error) {
			calls.Add(1)
			started <- struct{}{}
			<-release
			return "[00:01]Recovered", "", "", nil
		}
		if err := tt.Click("重新加载歌词"); err != nil {
			t.Fatal(err)
		}
		<-started
		tt.Frame()
		if err := tt.Click("重新加载歌词"); err == nil {
			t.Fatal("in-flight lyrics offered duplicate retry")
		}
		close(release)
		for a.pl.MetadataLoading() {
			refreshPost(t, posts)
		}
		lines, loaded := a.pl.Lyrics()
		if !loaded || len(lines) != 1 || calls.Load() != 1 || a.pl.Playing() || a.pl.Current().ID != 7 {
			t.Fatalf("lyrics recovery: loaded=%v lines=%d calls=%d playing=%v track=%d", loaded, len(lines), calls.Load(), a.pl.Playing(), a.pl.Current().ID)
		}
	}
}
