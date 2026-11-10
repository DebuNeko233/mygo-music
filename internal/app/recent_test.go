package app

import (
	"testing"
	"time"

	"github.com/egoist/mygo/ui"

	"netease-music-mygo/internal/netease"
)

// fakeRecent answers for what the listener played lately, as NetEase does:
// two songs, a playlist, an album and an episode, counted as they are asked
// for.
func fakeRecent(t *testing.T) *int {
	t.Helper()
	oldPlays, oldPlaylists, oldAlbums := recentPlays, recentPlaylists, recentAlbums
	oldPodcasts, oldVoices := recentPodcasts, recentVoices
	t.Cleanup(func() {
		recentPlays, recentPlaylists, recentAlbums = oldPlays, oldPlaylists, oldAlbums
		recentPodcasts, recentVoices = oldPodcasts, oldVoices
	})
	heard := time.Now().Add(-3 * time.Minute)
	calls := 0
	recentPlays = func(limit int) ([]netease.Play, error) {
		calls++
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
	a.profile = &netease.Profile{UserID: 1, Nickname: "小明"}
	posts := testPosts(a)
	a.route = route{page: pageRecent}
	tt := ui.NewTester(a.view, 1200, 800)
	// The page asks for the records as it builds.
	tt.Frame()
	for i := 0; i < 20 && !a.recent.shown; i++ {
		refreshPost(t, posts)
	}
	if !a.recent.shown {
		t.Fatal("the recent plays never loaded")
	}
	tt.Frame()
	return a, posts, tt
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
	if *calls != 1 {
		t.Fatalf("asked for the songs %d times, want once", *calls)
	}
	a.recent.at = time.Now().Add(-time.Hour)
	tt.Frame()
	for i := 0; i < 20 && *calls == 1; i++ {
		refreshPost(t, posts)
	}
	if *calls != 2 {
		t.Errorf("asked for the songs %d times, want twice", *calls)
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
