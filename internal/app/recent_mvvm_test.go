/*
验证最近播放分类切换、重试及重新登录后的响应隔离。
*/

package app

import (
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/egoist/mygo/ui"

	diskcache "netease-music-mygo/internal/cache/disk"
	"netease-music-mygo/internal/netease"
	recentvm "netease-music-mygo/internal/viewmodel/recent"
)

// Switching away from a stalled tab keeps ready songs usable, and its retry action refreshes only the failed record.
func TestRecentTabSwitchAndRetry(t *testing.T) {
	calls := fakeRecent(t)
	a := newTestApp()
	a.settings = defaultSettings()
	a.account.Profile = &netease.Profile{UserID: 7}
	a.nav.Current = route{Page: pageRecent}
	fixed := time.Date(2026, 11, 23, 12, 0, 0, 0, time.UTC)
	a.library.Refresh.Now = func() time.Time { return fixed }
	posts := testPosts(a)
	gate := make(chan struct{})
	release := sync.OnceFunc(func() { close(gate) })
	defer release()
	var albumCalls atomic.Int32
	recentAlbums = func(int) ([]netease.Played[netease.AlbumProfile], error) {
		if albumCalls.Add(1) == 1 {
			<-gate
			return nil, netease.ErrOffline
		}
		return []netease.Played[netease.AlbumProfile]{{Item: netease.AlbumProfile{Album: netease.Album{ID: 77, Name: "神的游戏"}}}}, nil
	}
	tt := ui.NewTester(func(c *ui.Context) { a.pal = paletteFor(c.Theme()); a.views.recent.Build(c) }, 1200, 800)
	tt.Frame()
	for len(a.recent.Tracks) == 0 || a.recent.Tabs[recentvm.RecentPlaylists].Loading || a.recent.Tabs[recentvm.RecentPodcasts].Loading || a.recent.Tabs[recentvm.RecentVoices].Loading {
		refreshPost(t, posts)
	}
	tt.Frame()
	if err := tt.Click("专辑"); err != nil {
		t.Fatal(err)
	}
	if a.recent.Selected != recentvm.RecentAlbums || !a.recent.Tabs[recentvm.RecentAlbums].Loading {
		t.Fatalf("tab selection did not bind to the loading record: selected=%d loading=%v", a.recent.Selected, a.recent.Tabs[recentvm.RecentAlbums].Loading)
	}
	if err := tt.Click("歌曲"); err != nil {
		t.Fatal(err)
	}
	if len(a.recent.Tracks) != 2 || calls.Load() != 1 {
		t.Fatal("stalled album blocked songs or restarted their request")
	}
	release()
	refreshPost(t, posts)
	tt.Frame()
	if err := tt.Click("专辑"); err != nil {
		t.Fatal(err)
	}
	if err := tt.Click("重新加载内容"); err != nil {
		t.Fatal(err)
	}
	for a.recent.Tabs[recentvm.RecentAlbums].Loading {
		refreshPost(t, posts)
	}
	tt.Frame()
	if albumCalls.Load() != 2 || calls.Load() != 1 || !a.recent.Tabs[recentvm.RecentAlbums].Shown || len(a.recent.AlbumItems) != 1 || a.recent.AlbumItems[0].ID != 77 || a.pl.Current() != nil {
		t.Fatal("tab retry lost data, refetched songs, or started playback")
	}
}

// Cached and network replies from a retired login cannot overwrite the same user's new record or clear its loading flags.
func TestRecentRepliesRespectSameUserNewLogin(t *testing.T) {
	for _, cached := range []bool{false, true} {
		name := "network"
		if cached {
			name = "cache"
		}
		t.Run(name, func(t *testing.T) {
			fakeRecent(t)
			a := newTestApp()
			a.account.Profile = &netease.Profile{UserID: 7}
			posts := testPosts(a)
			count := recentvm.RecentTabCount
			if cached {
				a.browseCache = diskcache.New(t.TempDir(), 1<<20)
				defer a.browseCache.Close()
				a.browseCache.Put("7", "recent", diskcache.Encode(recentvm.RecentSnapshot{Songs: []netease.Play{{Track: netease.Track{ID: 42}}}}), a.browseCache.Epoch())
				count = 1
			}
			a.loadRecent()
			old := make([]func(), count)
			for i := range old {
				old[i] = wait(t, posts, "retired record result")
			}
			a.account.Accept(&netease.Profile{UserID: 7})
			a.recent.SetSongs([]netease.Play{{Track: netease.Track{ID: 99}}})
			a.recent.Tabs[recentvm.RecentSongs].Loading = true
			for _, apply := range old {
				apply()
			}
			if a.recent.Tracks[0].ID != 99 || !a.recent.Tabs[recentvm.RecentSongs].Loading || cached && !a.recent.ReadingSnapshot {
				t.Fatal("old account generation changed a new pending record")
			}
		})
	}
}
