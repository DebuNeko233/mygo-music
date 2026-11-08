package app

import (
	"sync/atomic"
	"testing"
	"time"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"
	"netease-music-mygo/internal/netease"
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
	if err := tt.Click("刷新网络与内容"); err != nil {
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
	if err := tt.Click("刷新网络与内容"); err != nil {
		t.Fatal(err)
	}
	if !a.network.offline || calls.Load() != 0 || checks != 1 {
		t.Fatal("offline refresh issued an account request")
	}
	tt.Frame()
	if err := tt.Click("刷新网络与内容"); err != nil {
		t.Fatal(err)
	}
	refreshPost(t, posts)
	if a.network.offline || a.recovery.accountPending || calls.Load() != 1 {
		t.Fatal("refresh did not confirm the restored connection")
	}
	tt.Frame()
	if err := tt.Click("刷新网络与内容"); err == nil {
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
