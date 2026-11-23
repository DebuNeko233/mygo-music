package app

import (
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/egoist/mygo/ui"

	"netease-music-mygo/internal/diskcache"
	"netease-music-mygo/internal/netease"
	"netease-music-mygo/internal/viewmodel"
)

// fakeRecent answers for what the listener played lately, as NetEase does:
// two songs, a playlist, an album and an episode, counted as they are asked
// for.
func fakeRecent(t *testing.T) *atomic.Int32 {
	t.Helper()
	oldPlays, oldPlaylists, oldAlbums := recentPlays, recentPlaylists, recentAlbums
	oldPodcasts, oldVoices := recentPodcasts, recentVoices
	t.Cleanup(func() {
		recentPlays, recentPlaylists, recentAlbums = oldPlays, oldPlaylists, oldAlbums
		recentPodcasts, recentVoices = oldPodcasts, oldVoices
	})
	heard := time.Now().Add(-3 * time.Minute)
	var calls atomic.Int32
	recentPlays = func(limit int) ([]netease.Play, error) {
		calls.Add(1)
		return []netease.Play{
			{Track: netease.Track{ID: 900, Name: "玫瑰色的你", Artists: []netease.Artist{{ID: 3, Name: "张悬"}}, Duration: 240000}, At: heard, Device: "iphone"},
			{Track: netease.Track{ID: 901, Name: "蓝天白云", Artists: []netease.Artist{{ID: 3, Name: "张悬"}}, Duration: 220000}, At: heard.Add(-time.Hour), Device: "osx"},
		}, nil
	}
	recentPlaylists = func(limit int) ([]netease.Played[netease.Playlist], error) {
		return []netease.Played[netease.Playlist]{{Item: netease.Playlist{ID: 11, Name: "夜里的歌"}, At: heard}}, nil
	}
	recentAlbums = func(limit int) ([]netease.Played[netease.AlbumProfile], error) {
		return []netease.Played[netease.AlbumProfile]{{Item: netease.AlbumProfile{Album: netease.Album{ID: 77, Name: "神的游戏"}}, At: heard}}, nil
	}
	recentPodcasts = func(limit int) ([]netease.Played[netease.Podcast], error) { return nil, nil }
	recentVoices = func(limit int) ([]netease.Played[netease.Voice], error) {
		return []netease.Played[netease.Voice]{{Item: netease.Voice{
			Program: netease.Program{ID: 55, Name: "第一夜", Duration: 600000},
			Podcast: netease.Podcast{ID: 66, Name: "夜谈"},
		}, At: heard}}, nil
	}
	return &calls
}

// recentTestApp is signed in, showing the recent plays the fake answers with.
func recentTestApp(t *testing.T) (*app, chan func(), *ui.Tester) {
	t.Helper()
	isolateUserData(t)
	a := newTestApp()
	a.settings = defaultSettings()
	a.account.Profile = &netease.Profile{UserID: 1, Nickname: "小明"}
	posts := testPosts(a)
	a.route = route{Page: pageRecent}
	tt := ui.NewTester(a.view, 1200, 800)
	// The page asks for the records as it builds.
	tt.Frame()
	for a.recent.Loading() {
		refreshPost(t, posts)
	}
	if !a.recent.Tabs[viewmodel.RecentSongs].Shown {
		t.Fatal("the recent plays never loaded")
	}
	tt.Frame()
	return a, posts, tt
}

// The five recent-play requests start together instead of waiting for each other.
func TestRecentRequestsRunConcurrently(t *testing.T) {
	fakeRecent(t)
	a := newTestApp()
	a.account.Profile = &netease.Profile{UserID: 1}
	posts := testPosts(a)
	started := make(chan string, 5)
	release := make(chan struct{})
	unblock := sync.OnceFunc(func() { close(release) })
	defer unblock()
	block := func(kind string) { started <- kind; <-release }
	recentPlays = func(int) ([]netease.Play, error) {
		block("songs")
		return nil, nil
	}
	recentPlaylists = func(int) ([]netease.Played[netease.Playlist], error) {
		block("playlists")
		return nil, nil
	}
	recentAlbums = func(int) ([]netease.Played[netease.AlbumProfile], error) {
		block("albums")
		return nil, nil
	}
	recentPodcasts = func(int) ([]netease.Played[netease.Podcast], error) {
		block("podcasts")
		return nil, nil
	}
	recentVoices = func(int) ([]netease.Played[netease.Voice], error) {
		block("voices")
		return nil, nil
	}
	a.loadRecent()
	for i := 0; i < 5; i++ {
		wait(t, started, "recent requests to start")
	}
	unblock()
	for range viewmodel.RecentTabCount {
		refreshPost(t, posts)
	}
	for i, tab := range a.recent.Tabs {
		if tab.Loading || !tab.Shown {
			t.Fatalf("recent tab %d did not finish: %+v", i, tab)
		}
	}
}

// Songs can appear while an unopened recent-play tab is still waiting on NetEase.
func TestRecentShowsSongsBeforeOtherTabsFinish(t *testing.T) {
	fakeRecent(t)
	a := newTestApp()
	a.account.Profile = &netease.Profile{UserID: 1}
	a.route = route{Page: pageRecent}
	posts := testPosts(a)
	started := make(chan struct{})
	release := make(chan struct{})
	defer close(release)
	recentPlaylists = func(int) ([]netease.Played[netease.Playlist], error) {
		close(started)
		<-release
		return nil, nil
	}
	tt := ui.NewTester(a.view, 1200, 800)
	tt.Frame()
	wait(t, started, "the playlist request")
	for len(a.recent.Tracks) == 0 {
		refreshPost(t, posts)
	}
	tt.Frame()
	if _, ok := tt.Find("玫瑰色的你"); !ok {
		t.Fatal("the songs tab still hides its results while the playlists request is pending")
	}
}

// A failed songs request must not discard the other four kinds of records.
func TestRecentKeepsOtherKindsWhenSongsFail(t *testing.T) {
	fakeRecent(t)
	recentPlays = func(int) ([]netease.Play, error) { return nil, netease.ErrOffline }
	a := newTestApp()
	a.account.Profile = &netease.Profile{UserID: 1}
	posts := testPosts(a)
	a.loadRecent()
	for a.recent.Loading() {
		refreshPost(t, posts)
	}
	if tab := a.recent.Tabs[viewmodel.RecentSongs]; tab.Shown || tab.Failure != netease.ErrOffline {
		t.Fatalf("songs failure did not reach its tab: %+v", tab)
	}
	if len(a.recent.Playlists) != 1 || len(a.recent.Albums) != 1 || len(a.recent.Voices) != 1 {
		t.Fatalf("songs failure discarded other records: %+v", a.recent)
	}
}

// The recent page plays the songs NetEase says were played, wherever they
// were played.
func TestRecentPagePlaysWhatWasPlayed(t *testing.T) {
	fakeRecent(t)
	a, _, tt := recentTestApp(t)
	if err := tt.Click("蓝天白云"); err != nil {
		t.Fatal(err)
	}
	if cur := a.pl.Current(); cur == nil || cur.ID != 901 {
		t.Errorf("playing %+v, want the song clicked", cur)
	}
}

// The 声音 tab of the recent page plays the episode clicked, as the podcast
// it is on plays it.
func TestRecentVoicesTabPlaysEpisode(t *testing.T) {
	fakeRecent(t)
	a, _, tt := recentTestApp(t)
	if err := tt.Click("声音"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	if err := tt.Click("第一夜"); err != nil {
		t.Fatal(err)
	}
	cur := a.pl.Current()
	if cur == nil || cur.ProgramID != 55 || cur.PodcastID != 66 {
		t.Errorf("playing %+v, want episode 55 of podcast 66", cur)
	}
}

// The recent plays are asked for again once they are a while old: the
// listener may have played something on another device since.
func TestRecentPageAsksAgainWhenOld(t *testing.T) {
	calls := fakeRecent(t)
	a, posts, tt := recentTestApp(t)
	if calls.Load() != 1 {
		t.Fatalf("asked for the songs %d times, want once", calls.Load())
	}
	a.recent.Tabs[viewmodel.RecentSongs].At = time.Now().Add(-time.Hour)
	tt.Frame()
	for i := 0; i < 20 && calls.Load() == 1; i++ {
		refreshPost(t, posts)
	}
	if calls.Load() != 2 {
		t.Errorf("asked for the songs %d times, want twice", calls.Load())
	}
}

// The recent plays say when and on what the listener played something.
func TestRecentPlaysSayWhen(t *testing.T) {
	now := time.Date(2026, 11, 10, 12, 0, 0, 0, time.Local)
	cases := []struct {
		at     time.Time
		device string
		want   string
	}{
		{now.Add(-30 * time.Second), "iphone", "刚刚 · 手机"},
		{now.Add(-12 * time.Minute), "osx", "12分钟前 · 电脑"},
		{now.Add(-3 * time.Hour), "web", "3小时前 · 网页版"},
		{now.Add(-50 * time.Hour), "", "2天前"},
	}
	for _, c := range cases {
		if got := playedAt(c.at, c.device, now); got != c.want {
			t.Errorf("playedAt(%v, %q) = %q, want %q", c.at, c.device, got, c.want)
		}
	}
}

// A saved recent list can be published while its network refresh is still blocked.
func TestRecentLoadsSnapshotOffTheUIThread(t *testing.T) {
	fakeRecent(t)
	a := newTestApp()
	a.account.Profile = &netease.Profile{UserID: 7}
	store := diskcache.New(t.TempDir(), 1<<20)
	defer store.Close()
	a.browseCache = store
	store.Put("7", "recent", cacheBytes(viewmodel.RecentSnapshot{
		Songs: []netease.Play{{Track: netease.Track{ID: 42}}},
	}), store.Epoch())
	posts := testPosts(a)
	release := make(chan struct{})
	unblock := sync.OnceFunc(func() { close(release) })
	defer unblock()
	recentPlays = func(int) ([]netease.Play, error) {
		<-release
		return nil, netease.ErrOffline
	}
	a.loadRecent()
	if a.recent.Tabs[viewmodel.RecentSongs].Shown {
		t.Fatal("loadRecent read the disk snapshot before returning")
	}
	refreshPost(t, posts)
	if !a.recent.Tabs[viewmodel.RecentSongs].Shown || !a.recent.Tabs[viewmodel.RecentSongs].Loading || len(a.recent.Tracks) != 1 || a.recent.Tracks[0].ID != 42 {
		t.Fatalf("cached list did not appear before the refresh: %+v", a.recent)
	}
	unblock()
	for a.recent.Loading() {
		refreshPost(t, posts)
	}
	if len(a.recent.Tracks) != 1 || a.recent.Tracks[0].ID != 42 {
		t.Fatal("offline refresh discarded the cached list")
	}
}
