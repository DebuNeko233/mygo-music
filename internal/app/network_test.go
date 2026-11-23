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
	a.library.Playlists[9] = pl
	a.recovery.timer = time.NewTimer(time.Hour)
	a.networkChanged(mygo.NetworkStatus{Online: false})
	a.recoveryFrame(true)
	if !a.network.offline || a.recovery.timer != nil || a.library.Playlists[9] != pl {
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
	if !a.network.offline || a.library.Playlists[9] != pl {
		t.Fatal("offline refresh changed cached page")
	}
}

// Reconnection retries a failed current page once, coalesces a duplicate system
// event and preserves the existing page until the new response arrives.
func TestNetworkReconnectionRecoversCurrentPage(t *testing.T) {
	a, posts := offlineFixture(t)
	a.route = route{page: pagePlaylist, id: 9}
	a.library.Playlists[9] = &netease.Playlist{ID: 9, Name: "Cached"}
	a.playlistRequest(9).Failed = true
	a.playlistRequest(9).Failure = netease.ErrOffline
	a.network = networkState{known: true, offline: true}
	a.recovery.active, a.recovery.attempts = true, 4
	var calls atomic.Int32
	playlistDetail = func(id int64) (*netease.Playlist, error) {
		calls.Add(1)
		return &netease.Playlist{ID: id, Name: "Online"}, nil
	}
	a.networkChanged(mygo.NetworkStatus{Online: true})
	a.networkChanged(mygo.NetworkStatus{Online: true, Metered: true})
	if a.network.offline || a.recovery.attempts != 0 || a.library.Playlists[9].Name != "Cached" {
		t.Fatal("reconnect did not reset recovery or replaced cache prematurely")
	}
	refreshPost(t, posts)
	if calls.Load() != 1 || a.library.Playlists[9].Name != "Online" || a.pl.Current() != nil {
		t.Fatal("reconnect duplicated requests or altered playback")
	}
}

// A failed podcast list used to stay failed through explicit refresh and
// reconnection because neither recovery path considered the visible tab.
func TestPodcastTabRecoversWithConnection(t *testing.T) {
	for _, manual := range []bool{false, true} {
		t.Run(map[bool]string{false: "reconnect", true: "manual"}[manual], func(t *testing.T) {
			a, posts := offlineFixture(t)
			a.route = route{page: pagePodcasts}
			a.podcastTab.hotPage.Error, a.podcastTab.hotPage.Failure = "热门播客加载失败", netease.ErrOffline
			calls := 0
			hotPodcasts = func(_, _ int) ([]netease.Podcast, bool, error) {
				calls++
				return []netease.Podcast{{ID: 5}}, false, nil
			}
			if manual {
				a.retryLoads(true)
			} else {
				a.recoverLoads()
			}
			refreshPost(t, posts)
			if calls != 1 || a.podcastTab.hotPage.Error != "" || len(a.podcastTab.hot) != 1 {
				t.Fatalf("podcast list did not recover: calls %d, tab %+v", calls, a.podcastTab.hotPage)
			}
		})
	}
}

// Clicking the icon rechecks system state; it sends no account request while
// still offline, and starts one when that explicit check observes a connection.
func TestNetworkRefreshIcon(t *testing.T) {
	a, posts := offlineFixture(t)
	a.route = route{page: pagePlaylist, id: 9}
	a.library.Playlists[9] = &netease.Playlist{ID: 9, Name: "Cached"}
	a.network = networkState{known: true, offline: true}
	a.account.Pending = true
	read, ask, save, identity := readNetworkStatus, accountAsk, saveLoginSession, loginIdentity
	t.Cleanup(func() { readNetworkStatus, accountAsk, saveLoginSession, loginIdentity = read, ask, save, identity })
	checks := 0
	readNetworkStatus = func() mygo.NetworkStatus { checks++; return mygo.NetworkStatus{Online: checks > 1} }
	var calls atomic.Int32
	accountAsk = func() (*netease.Profile, error) { calls.Add(1); return &netease.Profile{UserID: 7}, nil }
	saveLoginSession = func() error { return nil }
	loginIdentity = func() string { return "" }
	a.library.Refresh.Mine.At, a.library.Refresh.Likes.At = a.listsNow(), a.listsNow()
	a.playlistRequest(9).At = a.listsNow()
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
	if a.network.offline || a.account.Pending || calls.Load() != 1 {
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
	a.account.Pending = true
	a.networkChanged(mygo.NetworkStatus{Online: true})
	if a.network.offline || a.account.Busy {
		t.Fatal("hidden window started a request or missed reconnection")
	}
}

// Opening either player layout hides the page's network actions immediately;
// they return only after closing, and network events still recover resources.
func TestNetworkControlsStayOutsideNowPlaying(t *testing.T) {
	a, posts := offlineFixture(t)
	a.route = route{page: pagePlaylist, id: 9}
	a.library.Playlists[9] = &netease.Playlist{ID: 9, Name: "Cached"}
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

// Refreshing the home page used to retry only failed covers, leaving healthy recommendation modules untouched.
func TestHomeRefreshIconReloadsContent(t *testing.T) {
	for _, scenario := range []struct {
		name                         string
		signedIn, reconnect, offline bool
		page                         pageKind
	}{
		{"signed-in", true, false, false, pageHome},
		{"signed-out", false, false, false, pageHome},
		{"reconnection", true, true, false, pageHome},
		{"offline", true, false, true, pageHome},
		{"charts", true, false, false, pageCharts},
	} {
		t.Run(scenario.name, func(t *testing.T) {
			a, posts := offlineFixture(t)
			a.route = route{page: scenario.page}
			if !scenario.signedIn {
				a.account.Profile = nil
			}
			a.network = networkState{known: true, offline: scenario.reconnect || scenario.offline}
			a.recovery.active = scenario.reconnect
			a.home.Featured = []netease.Playlist{{ID: 1, Name: "old featured"}}
			a.home.Charts = []netease.Playlist{{ID: 2, Name: "old charts"}}
			a.home.Daily = []netease.Track{{ID: 3, Name: "old daily"}}
			a.home.Recommended = []netease.Playlist{{ID: 4, Name: "old personal"}}
			if scenario.reconnect {
				a.home.Failures = map[string]error{"featured": netease.ErrOffline}
			}
			a.art.art["failed-cover"] = &artwork{failed: true}
			a.art.seen["failed-cover"] = struct{}{}
			featured, charts, daily, recommended, status := featuredPlaylists, homeCharts, dailySongs, recommendedPlaylists, readNetworkStatus
			t.Cleanup(func() {
				featuredPlaylists, homeCharts, dailySongs, recommendedPlaylists, readNetworkStatus = featured, charts, daily, recommended, status
			})
			var calls [4]atomic.Int32
			featuredPlaylists = func() ([]netease.Playlist, error) {
				calls[0].Add(1)
				return []netease.Playlist{{ID: 11, Name: "fresh featured"}}, nil
			}
			homeCharts = func() ([]netease.Playlist, error) {
				calls[1].Add(1)
				return []netease.Playlist{{ID: 12, Name: "fresh charts"}}, nil
			}
			dailySongs = func() ([]netease.Track, error) {
				calls[2].Add(1)
				return []netease.Track{{ID: 13, Name: "fresh daily"}}, nil
			}
			recommendedPlaylists = func() ([]netease.Playlist, error) {
				calls[3].Add(1)
				return []netease.Playlist{{ID: 14, Name: "fresh personal"}}, nil
			}
			readNetworkStatus = func() mygo.NetworkStatus { return mygo.NetworkStatus{Online: !scenario.offline} }
			tt := ui.NewTester(func(c *ui.Context) { a.pal = paletteFor(c.Theme()); a.offlineNotice(c) }, 1100, 800)
			tt.Frame()
			if err := tt.Click("刷新内容"); err != nil {
				t.Fatal(err)
			}
			if scenario.offline {
				if a.home.Loading || !a.network.offline {
					t.Fatal("offline refresh started loading content")
				}
				for i := range calls {
					if calls[i].Load() != 0 {
						t.Fatal("offline refresh issued a request")
					}
				}
				return
			}
			if !a.home.Loading {
				t.Fatal("refresh retried artwork without requesting home content")
			}
			if a.home.Featured[0].ID != 1 || a.home.Charts[0].ID != 2 {
				t.Fatal("refresh discarded the visible content before a reply")
			}
			// A second click while the same request is running must coalesce.
			if err := tt.Click("刷新内容"); err != nil {
				t.Fatal(err)
			}
			for a.home.Loading {
				refreshPost(t, posts)
			}
			if calls[0].Load() != 1 || calls[1].Load() != 1 || a.home.Featured[0].ID != 11 || a.home.Charts[0].ID != 12 {
				t.Fatal("home modules were not replaced exactly once")
			}
			if scenario.signedIn {
				if calls[2].Load() != 1 || calls[3].Load() != 1 || a.home.Daily[0].ID != 13 || a.home.Recommended[0].ID != 14 {
					t.Fatal("personal recommendations were not refreshed")
				}
			} else if calls[2].Load() != 0 || calls[3].Load() != 0 {
				t.Fatal("signed-out refresh requested private recommendations")
			}
			if a.pl.Current() != nil || len(a.pl.Queue()) != 0 {
				t.Fatal("refresh started playback")
			}
			tt.Frame()
			if err := tt.Click("刷新内容"); err == nil {
				t.Fatal("successful content and cover recovery left a stale refresh control")
			}
		})
	}
}
