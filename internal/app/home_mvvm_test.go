package app

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/egoist/mygo/ui"

	"netease-music-mygo/internal/diskcache"
	"netease-music-mygo/internal/netease"
)

// Late cached and network replies from a prior login cannot change a new session of the same account or its snapshot.
func TestHomeRepliesRespectSameUserNewLogin(t *testing.T) {
	a, posts := offlineFixture(t)
	a.browseCache.Close()
	dir := t.TempDir()
	a.browseCache = diskcache.New(dir, 1<<20)
	for _, key := range []string{"featured", "charts", "recommended"} {
		a.browseCache.Put("7", key, diskcache.Encode([]netease.Playlist{{ID: 1}}), a.browseCache.Epoch())
	}
	a.browseCache.Put("7", "daily", diskcache.Encode([]netease.Track{{ID: 1}}), a.browseCache.Epoch())
	oldFeatured, oldCharts, oldDaily, oldRecommended := featuredPlaylists, homeCharts, dailySongs, recommendedPlaylists
	t.Cleanup(func() {
		featuredPlaylists, homeCharts, dailySongs, recommendedPlaylists = oldFeatured, oldCharts, oldDaily, oldRecommended
	})
	featuredPlaylists = func() ([]netease.Playlist, error) { return []netease.Playlist{{ID: 11}}, nil }
	homeCharts = featuredPlaylists
	recommendedPlaylists = featuredPlaylists
	dailySongs = func() ([]netease.Track, error) { return []netease.Track{{ID: 11}}, nil }
	a.loadHome()
	old := make([]func(), 8)
	for i := range old {
		old[i] = wait(t, posts, "retired home cache/network reply")
	}
	// The host guard is independent of Home.Reset; the account generation itself must retire its dispatcher.
	a.account.Accept(&netease.Profile{UserID: 7})
	a.home.Featured = []netease.Playlist{{ID: 100}}
	a.home.Daily = []netease.Track{{ID: 200}}
	a.home.Error = "new request"
	for _, apply := range old {
		apply()
	}
	if a.home.Featured[0].ID != 100 || a.home.Daily[0].ID != 200 || a.home.Error != "new request" || !a.home.Loading {
		t.Fatal("old login changed current home state")
	}
	a.browseCache.Close()
	a.browseCache = diskcache.New(dir, 1<<20)
	b, _ := a.browseCache.Read("7", "featured")
	var kept []netease.Playlist
	if json.Unmarshal(b, &kept) != nil || len(kept) != 1 || kept[0].ID != 1 {
		t.Fatal("retired login overwrote the accepted cache")
	}
}

// The daily card starts the existing player at the first playable song, then pauses/resumes its queue without seeking or navigating.
func TestDailyCardPlayPauseKeepsSharedQueue(t *testing.T) {
	a := homePerfApp(3)
	a.home.Daily[0].Fee = 1
	a.pl.SuspendCache()
	tt := ui.NewTester(func(c *ui.Context) { a.pal = paletteFor(c.Theme()); a.views.daily.Card(c, c.Now()) }, 1200, 300)
	if err := tt.Click("播放"); err != nil {
		t.Fatal(err)
	}
	if a.pl.Current() == nil || a.pl.Current().ID != 2 || len(a.pl.Queue()) != 3 || !a.pl.Playing() || a.nav.Current.Page != pageHome {
		t.Fatal("daily card bypassed the shared player or navigated instead of playing")
	}
	a.pl.Hold(17 * time.Second)
	tt.Frame()
	if err := tt.Click("暂停播放"); err != nil {
		t.Fatal(err)
	}
	if a.pl.Playing() || a.pl.Position() != 17*time.Second || a.pl.Current().ID != 2 || len(a.pl.Queue()) != 3 {
		t.Fatal("pause changed the daily queue or progress")
	}
	if err := tt.Click("继续播放"); err != nil {
		t.Fatal(err)
	}
	if !a.pl.Playing() || a.pl.Position() != 17*time.Second || a.pl.Current().ID != 2 || a.playbackState.Open {
		t.Fatal("resume restarted the queue or opened another page")
	}
}

// Daily page controls read the shared VM list and preserve the existing sequential-play and selected-row behavior after a refresh.
func TestDailyPageUsesRefreshedHomeList(t *testing.T) {
	a := homePerfApp(3)
	a.nav.Current = route{Page: pageDaily}
	a.pl.SuspendCache()
	a.home.Daily[0].Fee = 1
	a.pl.ToggleShuffle()
	tt := ui.NewTester(func(c *ui.Context) { a.pal = paletteFor(c.Theme()); a.views.daily.Build(c) }, 1200, 800)
	if err := tt.Click("播放全部"); err != nil {
		t.Fatal(err)
	}
	if a.pl.Current() == nil || a.pl.Current().ID != 2 || a.pl.Shuffle() || len(a.pl.Queue()) != 3 {
		t.Fatal("play-all lost its existing sequential/first-playable behavior")
	}
	a.home.Daily = []netease.Track{{ID: 101, Name: "刷新后的第一首"}, {ID: 102, Name: "刷新后的第二首"}}
	tt.Frame()
	if a.pl.Current().ID != 2 || len(a.pl.Queue()) != 3 {
		t.Fatal("publishing recommendations changed the active queue")
	}
	if err := tt.Click("刷新后的第二首"); err != nil {
		t.Fatal(err)
	}
	if a.pl.Current().ID != 102 || len(a.pl.Queue()) != 2 || a.pl.Queue()[0].ID != 101 || a.nav.Current.Page != pageDaily {
		t.Fatal("row selection used an old daily snapshot or a separate queue")
	}
}
