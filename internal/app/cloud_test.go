/*
验证网盘分页、搜索、队列、刷新和当前歌曲定位，并覆盖取消与账号隔离。
*/

package app

import (
	"context"
	"fmt"
	"image/png"
	"os"
	"path/filepath"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/egoist/mygo/ui"

	playbackview "netease-music-mygo/internal/desktop/view/playback"
	"netease-music-mygo/internal/netease"
	"netease-music-mygo/internal/player"
	"netease-music-mygo/internal/viewmodel"
	cloudvm "netease-music-mygo/internal/viewmodel/cloud"
)

func cloudFixture(t *testing.T) (*app, chan func()) {
	t.Helper()
	isolateUserData(t)
	old, oldSuggest := cloudSongs, searchSuggest
	searchSuggest = func(string) (netease.Suggestions, error) { return netease.Suggestions{}, netease.ErrOffline }
	t.Cleanup(func() { cloudSongs, searchSuggest = old, oldSuggest })
	a := newTestApp()
	t.Cleanup(a.searches.DismissSuggestions)
	a.settings = defaultSettings()
	a.account.Profile = &netease.Profile{UserID: 1}
	a.nav.Current = route{Page: pageCloud}
	return a, testPosts(a)
}

func cloudTrack(id int64, name string) netease.Track {
	return netease.Track{ID: id, Cloud: true, Name: name, Artists: []netease.Artist{{Name: "MyGO!!!!!"}}, Duration: 180000}
}

// Opening the cloud page loads once; more, retry and filtering preserve the right queue entries.
func TestCloudPaginationFilterAndQueue(t *testing.T) {
	a, posts := cloudFixture(t)
	calls := make(chan int, 8)
	result := make(chan *netease.CloudPage, 8)
	failure := make(chan error, 8)
	cloudSongs = func(_ context.Context, limit, offset int) (*netease.CloudPage, error) {
		if limit != 100 {
			t.Errorf("limit %d", limit)
		}
		calls <- offset
		page := <-result
		return page, <-failure
	}
	tt := ui.NewTester(a.view, 1200, 800)
	tt.Frame()
	if offset := wait(t, calls, "cloud first page"); offset != 0 {
		t.Fatalf("first offset %d", offset)
	}
	tt.Frame()
	tt.Frame()
	if len(calls) != 0 {
		t.Fatal("rendering repeated the cloud request")
	}
	result <- &netease.CloudPage{Tracks: []netease.Track{cloudTrack(1, "春日影"), cloudTrack(2, "迷星叫")}, Count: 4, NextOffset: 2, HasMore: true}
	failure <- nil
	refreshPost(t, posts)
	tt.Frame()
	if offset := wait(t, calls, "cloud next page"); offset != 2 {
		t.Fatalf("next offset %d", offset)
	}
	result <- nil
	failure <- netease.ErrOffline
	refreshPost(t, posts)
	tt.Frame()
	if len(a.cloud.Tracks) != 2 || a.cloud.Offset != 2 {
		t.Fatal("failed page discarded earlier songs")
	}
	if err := tt.Click("重试加载"); err != nil {
		t.Fatal(err)
	}
	if offset := wait(t, calls, "cloud retry"); offset != 2 {
		t.Fatalf("retry skipped offset: %d", offset)
	}
	result <- &netease.CloudPage{Tracks: []netease.Track{cloudTrack(2, "迷星叫"), cloudTrack(3, "壱雫空")}, Count: 4, NextOffset: 4}
	failure <- nil
	refreshPost(t, posts)
	tt.Frame()
	if len(a.cloud.Tracks) != 3 || a.cloud.More {
		t.Fatalf("duplicate page or wrong end: %+v", a.cloud)
	}
	if got := a.trackSuggestions("MYGO 壱雫空"); len(got) != 1 || got[0].Song != 3 {
		t.Fatalf("bar matching %+v", got)
	}
	a.pl.Cue([]netease.Track{cloudTrack(99, "当前歌曲")})
	tt.Frame()
	if err := tt.RightClick("壱雫空"); err != nil {
		t.Fatal(err)
	}
	if err := tt.ChooseMenuItem("加入队列"); err != nil {
		t.Fatal(err)
	}
	if q := a.pl.Queue(); len(q) != 2 || q[1].ID != 3 || !q[1].Cloud || a.pl.Current().ID != 99 {
		t.Fatalf("cloud queue %+v", q)
	}
}

// Retrying a failed refresh must start at zero even after the previous list reached its end.
func TestCloudRefreshRetryKeepsOldList(t *testing.T) {
	a, posts := cloudFixture(t)
	a.cloud = cloudvm.Cloud{CloudListing: cloudvm.CloudListing{Tracks: []netease.Track{cloudTrack(1, "旧歌曲")}, Offset: 100}, Loaded: true}
	calls := 0
	cloudSongs = func(_ context.Context, _, offset int) (*netease.CloudPage, error) {
		calls++
		if offset != 0 {
			t.Errorf("refresh offset %d", offset)
		}
		if calls == 1 {
			return nil, netease.ErrOffline
		}
		return &netease.CloudPage{Tracks: []netease.Track{cloudTrack(2, "新歌曲")}, Count: 1, NextOffset: 1}, nil
	}
	a.loadCloud(cloudvm.CloudRefresh)
	refreshPost(t, posts)
	if a.cloud.Tracks[0].ID != 1 || a.cloud.Failure == nil {
		t.Fatal("refresh failure lost old list")
	}
	a.loadCloud(cloudvm.CloudNextPage)
	refreshPost(t, posts)
	if calls != 2 || a.cloud.Failure != nil || len(a.cloud.Tracks) != 1 || a.cloud.Tracks[0].ID != 2 {
		t.Fatalf("refresh retry %+v", a.cloud)
	}
}

// Responses from a previous login cannot populate a new listener's cloud library.
func TestCloudRejectsPreviousAccount(t *testing.T) {
	a, posts := cloudFixture(t)
	started, release := make(chan struct{}), make(chan struct{})
	cloudSongs = func(context.Context, int, int) (*netease.CloudPage, error) {
		close(started)
		<-release
		return &netease.CloudPage{Tracks: []netease.Track{cloudTrack(1, "私人歌曲")}, Count: 1, NextOffset: 1}, nil
	}
	a.loadCloud(cloudvm.CloudNextPage)
	wait(t, started, "blocked cloud request")
	a.account.Generation++
	a.account.Profile = &netease.Profile{UserID: 2}
	a.cloud = cloudvm.Cloud{}
	close(release)
	refreshPost(t, posts)
	if a.cloud.Loaded || len(a.cloud.Tracks) != 0 || a.cloud.Loading() {
		t.Fatal("old account leaked into new library")
	}
}

// Appending and refreshing a cloud library cannot mutate a queue retained by the player.
func TestCloudKeepsPlayingQueue(t *testing.T) {
	a, posts := cloudFixture(t)
	tracks := make([]netease.Track, 1, 10)
	tracks[0] = cloudTrack(1, "第一首")
	a.cloud = cloudvm.Cloud{CloudListing: cloudvm.CloudListing{Tracks: tracks, More: true, Offset: 1}, Loaded: true}
	playerPosts := make(chan func(), 16)
	a.pl = player.New(func(fn func()) { playerPosts <- fn })
	a.pl.FetchLyrics = func(int64) (string, string, string, error) { return "", "", "", netease.ErrOffline }
	wasOffline := netease.Offline()
	netease.SetOffline(true)
	defer netease.SetOffline(wasOffline)
	// Cue borrows a fixture slice; exercise the real queue ownership boundary.
	a.pl.PlayQueueFrom(tracks, 0, a.playSource())
	for a.pl.Loading() || a.pl.MetadataLoading() {
		refreshPost(t, playerPosts)
	}
	cloudSongs = func(context.Context, int, int) (*netease.CloudPage, error) {
		return &netease.CloudPage{Tracks: []netease.Track{cloudTrack(2, "第二首")}, Count: 2, NextOffset: 2}, nil
	}
	a.loadCloud(cloudvm.CloudNextPage)
	refreshPost(t, posts)
	a.cloud.Tracks[0].Name = "刷新后的名称"
	if a.pl.Queue()[0].Name != "第一首" {
		t.Fatal("cloud response edited the active queue")
	}
	if len(a.pl.Queue()) != 1 {
		t.Fatal("pagination extended the active queue")
	}
	a.cloud.CloudListing = cloudvm.CloudListing{Tracks: []netease.Track{cloudTrack(3, "刷新结果")}}
	if a.pl.Current().ID != 1 || len(a.pl.Queue()) != 1 || a.pl.Queue()[0].Name != "第一首" {
		t.Fatal("refresh changed the playing queue")
	}
}

// With NM_FRAMES set, inspect cloud lists and filtering in light and dark themes.
func TestCloudFrames(t *testing.T) {
	dir := os.Getenv("NM_FRAMES")
	if dir == "" {
		t.Skip()
	}
	a, _ := cloudFixture(t)
	a.cloud = cloudvm.Cloud{CloudListing: cloudvm.CloudListing{More: true, Count: 203, Offset: 3, Tracks: []netease.Track{cloudTrack(1, "春日影（Live）"), cloudTrack(2, "迷星叫"), cloudTrack(3, "未匹配的上传歌曲")}}, Loaded: true}
	a.cloud.Tracks[2].Artists = nil

	for _, variant := range []struct {
		name                  string
		width                 int
		dark, search, loading bool
	}{
		{"cloud-light.png", 1200, false, false, false},
		{"cloud-dark.png", 1200, true, false, false},
		{"cloud-search-narrow.png", 800, true, true, false},
		{"cloud-loading-narrow.png", 800, false, false, true},
	} {
		a.searches.Query = ""
		a.searches.DismissSuggestions()
		a.cloud.Stop()
		if variant.loading {
			holdCloudLoad(t, &a.cloud)
		}
		a.cloud.More = variant.loading
		if variant.loading {
			a.cloud.Count = 203
		} else {
			a.cloud.Count = 3
		}
		tt := ui.NewTester(a.view, variant.width, 800)
		tt.SetDark(variant.dark)
		tt.Advance(time.Second)
		tt.Frame()
		if variant.search {
			typePlaylistSearch(t, tt, "未匹配")
		}
		f, err := os.Create(filepath.Join(dir, variant.name))
		if err != nil {
			t.Fatal(err)
		}
		err = png.Encode(f, tt.Image())
		f.Close()
		if err != nil {
			t.Fatal(err)
		}
	}
}

// Clicking a cloud row plays that uploaded song and keeps the displayed list as its queue.
func TestCloudRowStartsSelectedUpload(t *testing.T) {
	a, _ := cloudFixture(t)
	a.cloud = cloudvm.Cloud{CloudListing: cloudvm.CloudListing{Tracks: []netease.Track{cloudTrack(101, "上传的第一首"), cloudTrack(102, "上传的第二首")}}, Loaded: true}
	posts := make(chan func(), 16)
	a.pl = player.New(func(fn func()) { posts <- fn })
	a.pl.FetchLyrics = func(int64) (string, string, string, error) { return "", "", "", netease.ErrOffline }
	wasOffline := netease.Offline()
	netease.SetOffline(true)
	defer netease.SetOffline(wasOffline)
	tt := ui.NewTester(a.view, 1200, 800)
	tt.Frame()
	if err := tt.Click("上传的第二首"); err != nil {
		t.Fatal(err)
	}
	if tr := a.pl.Current(); tr == nil || tr.ID != 102 || !tr.Cloud || len(a.pl.Queue()) != 2 {
		t.Fatalf("selected upload: %+v", tr)
	}
	for a.pl.Loading() || a.pl.MetadataLoading() {
		refreshPost(t, posts)
	}
	if a.pl.Current().ID != 102 {
		t.Fatal("an offline response skipped the chosen upload")
	}
}

// Loading the whole cloud library continues after a failed page without losing the query or changing playback.
func TestCloudLoadAllRetriesAndFindsLaterSongs(t *testing.T) {
	a, posts := cloudFixture(t)
	a.cloud = cloudvm.Cloud{CloudListing: cloudvm.CloudListing{More: true, Offset: 1, Count: 3, Tracks: []netease.Track{cloudTrack(1, "第一页")}}, Loaded: true}
	a.trackSuggestions("最后一页")
	a.pl.Cue([]netease.Track{cloudTrack(99, "当前播放")})
	type reply struct {
		page *netease.CloudPage
		err  error
	}
	calls := make(chan int, 8)
	results := make(chan reply, 8)
	cloudSongs = func(_ context.Context, _, offset int) (*netease.CloudPage, error) {
		calls <- offset
		r := <-results
		return r.page, r.err
	}
	a.loadCloud(cloudvm.CloudNextPage)
	tt := ui.NewTester(a.view, 1200, 800)
	tt.Frame()
	if got := wait(t, calls, "full cloud scan"); got != 1 {
		t.Fatalf("first scan offset %d", got)
	}
	results <- reply{page: &netease.CloudPage{Tracks: []netease.Track{cloudTrack(2, "第二页")}, Count: 3, NextOffset: 2, HasMore: true}}
	refreshPost(t, posts)
	if got := wait(t, calls, "next scan page"); got != 2 {
		t.Fatalf("next scan offset %d", got)
	}
	results <- reply{err: netease.ErrOffline}
	refreshPost(t, posts)
	tt.Frame()
	if a.cloud.Loading() || a.cloud.Offset != 2 {
		t.Fatalf("failed scan state: %+v", a.cloud)
	}
	if err := tt.Click("重试加载"); err != nil {
		t.Fatal(err)
	}
	if got := wait(t, calls, "scan retry"); got != 2 {
		t.Fatalf("retry offset %d", got)
	}
	results <- reply{page: &netease.CloudPage{Tracks: []netease.Track{cloudTrack(3, "最后一页的歌曲")}, Count: 3, NextOffset: 3}}
	refreshPost(t, posts)
	tt.Frame()
	if a.cloud.More || a.cloud.Failure != nil || len(a.trackSuggestions("最后一页")) != 1 || a.trackSuggestions("最后一页")[0].Song != 3 {
		t.Fatalf("completed search: %+v", a.cloud)
	}
	if a.pl.Current().ID != 99 || len(a.pl.Queue()) != 1 {
		t.Fatal("loading the library changed playback")
	}
}

// Cancelling an obsolete request cancels its context and rejects its late response after a new request begins.
func TestCloudCancellationRejectsLatePage(t *testing.T) {
	a, posts := cloudFixture(t)
	started := make(chan int, 4)
	cancelled := make(chan struct{})
	oldRelease, newRelease := make(chan struct{}), make(chan struct{})
	releaseOld, releaseNew := sync.OnceFunc(func() { close(oldRelease) }), sync.OnceFunc(func() { close(newRelease) })
	defer releaseOld()
	defer releaseNew()
	var calls atomic.Int32
	cloudSongs = func(ctx context.Context, _, offset int) (*netease.CloudPage, error) {
		n := int(calls.Add(1))
		started <- n
		if n == 1 {
			<-ctx.Done()
			close(cancelled)
			<-oldRelease
			return &netease.CloudPage{Tracks: []netease.Track{cloudTrack(1, "过期结果")}, Count: 1, NextOffset: 1}, nil
		}
		<-newRelease
		return &netease.CloudPage{Tracks: []netease.Track{cloudTrack(2, "最新结果")}, Count: 1, NextOffset: 1}, nil
	}
	tt := ui.NewTester(a.view, 1200, 800)
	tt.Frame()
	wait(t, started, "first cloud request")
	a.cloud.Stop()
	wait(t, cancelled, "request context cancellation")
	a.loadCloud(cloudvm.CloudNextPage)
	wait(t, started, "resumed cloud request")
	releaseOld()
	refreshPost(t, posts)
	if !a.cloud.Loading() || len(a.cloud.Tracks) != 0 {
		t.Fatal("stopped page overwrote the newer request")
	}
	releaseNew()
	refreshPost(t, posts)
	if a.cloud.Loading() || len(a.cloud.Tracks) != 1 || a.cloud.Tracks[0].ID != 2 {
		t.Fatalf("resume result %+v", a.cloud)
	}
}

// A non-advancing server page stops a full scan instead of looping or claiming that the library is complete.
func TestCloudScanRejectsNonAdvancingPage(t *testing.T) {
	a, posts := cloudFixture(t)
	a.cloud = cloudvm.Cloud{CloudListing: cloudvm.CloudListing{More: true, Offset: 100, Tracks: []netease.Track{cloudTrack(1, "原有歌曲")}}, Loaded: true}
	var calls atomic.Int32
	cloudSongs = func(context.Context, int, int) (*netease.CloudPage, error) {
		calls.Add(1)
		return &netease.CloudPage{Count: 200, NextOffset: 100, HasMore: true}, nil
	}
	a.loadCloud(cloudvm.CloudNextPage)
	refreshPost(t, posts)
	if a.cloud.Failure == nil || a.cloud.Loading() || !a.cloud.More || calls.Load() != 1 || len(a.cloud.Tracks) != 1 {
		t.Fatalf("non-advancing page %+v", a.cloud)
	}
}

// Uploaded songs reuse the player's queue ordering, disabled actions, and removal without deleting the cloud library entry.
func TestCloudQueueActionsUsePlayerOrder(t *testing.T) {
	a, _ := cloudFixture(t)
	second, third := cloudTrack(2, "第二首"), cloudTrack(3, "第三首")
	a.cloud = cloudvm.Cloud{CloudListing: cloudvm.CloudListing{Tracks: []netease.Track{second, third}}, Loaded: true}
	a.pl.Cue([]netease.Track{cloudTrack(99, "当前歌曲"), third, second})
	tt := ui.NewTester(a.view, 1200, 800)
	tt.Frame()
	open := func() {
		t.Helper()
		if err := tt.RightClick(second.Name); err != nil {
			t.Fatal(err)
		}
	}
	open()
	if err := tt.ChooseMenuItem("下一首播放"); err != nil {
		t.Fatal(err)
	}
	if next, _ := a.pl.AdjacentTracks(); next == nil || next.ID != 2 || len(a.pl.Queue()) != 3 {
		t.Fatal("play-next duplicated or misplaced an existing queue entry")
	}
	open()
	if err := tt.ChooseMenuItem("下一首播放"); err == nil {
		t.Fatal("already-next action was enabled")
	}
	tt.CloseMenu()
	open()
	if err := tt.ChooseMenuItem("移除队列"); err != nil {
		t.Fatal(err)
	}
	if len(a.pl.Queue()) != 2 || a.pl.Current().ID != 99 || len(a.cloud.Tracks) != 2 {
		t.Fatal("queue removal changed the current track or cloud library")
	}
}

// Refreshing a fully loaded library reloads all pages so a whole-library search does not shrink to the first page.
func TestCloudRefreshPreservesFullSearchScope(t *testing.T) {
	a, posts := cloudFixture(t)
	a.cloud = cloudvm.Cloud{CloudListing: cloudvm.CloudListing{Offset: 1, Count: 1, Tracks: []netease.Track{cloudTrack(1, "旧版本")}}, Loaded: true}
	a.trackSuggestions("新版本")
	calls := make(chan int, 4)
	cloudSongs = func(_ context.Context, _, offset int) (*netease.CloudPage, error) {
		calls <- offset
		if offset == 0 {
			return &netease.CloudPage{Tracks: []netease.Track{cloudTrack(2, "第一页")}, Count: 2, NextOffset: 1, HasMore: true}, nil
		}
		return &netease.CloudPage{Tracks: []netease.Track{cloudTrack(3, "新版本")}, Count: 2, NextOffset: 2}, nil
	}
	tt := ui.NewTester(a.view, 1200, 800)
	tt.Frame()
	a.refreshCloud()
	refreshPost(t, posts)
	refreshPost(t, posts)
	if first, next := wait(t, calls, "refresh first page"), wait(t, calls, "refresh next page"); first != 0 || next != 1 {
		t.Fatalf("refresh offsets %d,%d", first, next)
	}
	if a.cloud.More || len(a.trackSuggestions("新版本")) != 1 || a.trackSuggestions("新版本")[0].Song != 3 {
		t.Fatalf("refresh search %+v", a.cloud)
	}
}

// The existing sidebar search plays and locates cloud matches with the whole cloud list as the queue.
func TestCloudSidebarSearchPlaysAndLocates(t *testing.T) {
	for _, keyboard := range []bool{false, true} {
		t.Run(fmt.Sprint(keyboard), func(t *testing.T) {
			a, _ := cloudFixture(t)
			tracks := make([]netease.Track, 80)
			for i := range tracks {
				tracks[i] = cloudTrack(int64(i+1), fmt.Sprintf("上传歌曲 %03d", i+1))
			}
			tracks[69].Alias = []string{"珍藏现场"}
			a.cloud = cloudvm.Cloud{CloudListing: cloudvm.CloudListing{Count: 80, Tracks: tracks}, Loaded: true}
			posts := make(chan func(), 16)
			a.pl = player.New(func(fn func()) { posts <- fn })
			a.pl.FetchLyrics = func(int64) (string, string, string, error) { return "", "", "", netease.ErrOffline }
			offline := netease.Offline()
			netease.SetOffline(true)
			defer netease.SetOffline(offline)
			tt := ui.NewTester(a.view, 1200, 800)
			tt.Frame()
			typePlaylistSearch(t, tt, "MYGO 珍藏")
			if keyboard {
				tt.Key(0, ui.KeyDown)
				tt.Key(0, ui.KeyEnter)
			} else if err := tt.Click("网盘：" + tracks[69].Name); err != nil {
				t.Fatal(err)
			}
			tt.Frame()
			if tr := a.pl.Current(); tr == nil || tr.ID != 70 || !tr.Cloud || len(a.pl.Queue()) != 80 || a.nav.Current.Page != pageCloud {
				t.Fatal("sidebar suggestion did not reuse the cloud queue")
			}
			for a.pl.Loading() || a.pl.MetadataLoading() {
				refreshPost(t, posts)
			}
			locatedRow(t, tt, tracks[69].Name, 760)
		})
	}
}

// Searching beyond loaded pages uses the same search popup and updates its matches as pages arrive.
func TestCloudSidebarSearchLoadsEntireLibrary(t *testing.T) {
	a, posts := cloudFixture(t)
	a.cloud = cloudvm.Cloud{CloudListing: cloudvm.CloudListing{More: true, Count: 2, Offset: 1, Tracks: []netease.Track{cloudTrack(1, "第一页")}}, Loaded: true}
	calls := make(chan int, 1)
	cloudSongs = func(_ context.Context, _, offset int) (*netease.CloudPage, error) {
		calls <- offset
		return &netease.CloudPage{Tracks: []netease.Track{cloudTrack(2, "后页歌曲")}, Count: 2, NextOffset: 2}, nil
	}
	tt := ui.NewTester(a.view, 1200, 800)
	tt.Frame()
	typePlaylistSearch(t, tt, "后页")
	if got := wait(t, calls, "search all cloud pages"); got != 1 {
		t.Fatalf("search offset %d", got)
	}
	for a.cloud.Loading() {
		refreshPost(t, posts)
	}
	tt.Frame()
	if _, found := tt.Find("网盘：后页歌曲"); !found {
		t.Fatal("new page did not appear in the existing search popup")
	}
	if a.searches.Query != "后页" || a.nav.Current.Page != pageCloud {
		t.Fatal("whole-cloud search lost query or navigated away")
	}
	if err := tt.Click("清空搜索"); err != nil {
		t.Fatal(err)
	}
	if a.searches.Query != "" || !tt.Focused("搜索") || len(a.cloud.Tracks) != 2 {
		t.Fatal("clearing the shared bar changed the cloud data or lost focus")
	}
}

// A stale suggestion cannot play a removed upload or a previous account's track.
func TestCloudSuggestionRevalidatesSource(t *testing.T) {
	for _, change := range []string{"reorder", "removed", "account", "page"} {
		t.Run(change, func(t *testing.T) {
			a, _ := cloudFixture(t)
			a.cloud = cloudvm.Cloud{CloudListing: cloudvm.CloudListing{Tracks: []netease.Track{cloudTrack(1, "匹配"), cloudTrack(2, "其他")}}, Loaded: true}
			a.pl.Cue([]netease.Track{cloudTrack(99, "当前")})
			suggestions := a.trackSuggestions("匹配")
			key := suggestions[0].Key
			switch change {
			case "reorder":
				a.cloud.Tracks = []netease.Track{cloudTrack(2, "其他"), cloudTrack(1, "匹配")}
			case "removed":
				a.cloud.Tracks = nil
			case "account":
				a.account.Profile = &netease.Profile{UserID: 2}
			case "page":
				a.nav.Current = route{Page: pageHome}
			}
			// Resolve only the reorder case; suppress I/O while observing its queue decision.
			if change == "reorder" {
				posts := make(chan func(), 16)
				a.pl = player.New(func(fn func()) { posts <- fn })
				a.pl.FetchLyrics = func(int64) (string, string, string, error) { return "", "", "", netease.ErrOffline }
				offline := netease.Offline()
				netease.SetOffline(true)
				defer netease.SetOffline(offline)
				a.chooseTrackSuggestion(key, "匹配")
				if a.pl.Current().ID != 1 {
					t.Fatal("stale index played another song")
				}
				for a.pl.Loading() || a.pl.MetadataLoading() {
					refreshPost(t, posts)
				}
			} else {
				a.chooseTrackSuggestion(key, "匹配")
				if a.pl.Current().ID != 99 {
					t.Fatal("stale suggestion changed playback")
				}
			}
		})
	}
}

// Reopening the cloud library uses the shared freshness interval instead of a separate refresh button.
func TestCloudRefreshesWhenReopenedAfterFreshnessInterval(t *testing.T) {
	a, posts := cloudFixture(t)
	now := time.Date(2026, 11, 22, 10, 0, 0, 0, time.UTC)
	a.library.Refresh.Now = func() time.Time { return now }
	a.cloud = cloudvm.Cloud{CloudListing: cloudvm.CloudListing{Tracks: []netease.Track{cloudTrack(1, "旧歌曲")}, Count: 1, Offset: 1}, Loaded: true, At: now}
	calls := make(chan int, 2)
	cloudSongs = func(_ context.Context, _, offset int) (*netease.CloudPage, error) {
		calls <- offset
		return &netease.CloudPage{Tracks: []netease.Track{cloudTrack(2, "新歌曲")}, Count: 1, NextOffset: 1}, nil
	}
	a.load(a.nav.Current)
	if a.cloud.Loading() {
		t.Fatal("reopening a fresh library repeated its request")
	}
	now = now.Add(viewmodel.ListsFresh + time.Second)
	a.load(a.nav.Current)
	if got := wait(t, calls, "automatic cloud refresh"); got != 0 {
		t.Fatalf("refresh offset %d", got)
	}
	refreshPost(t, posts)
	if a.cloud.Loading() || a.cloud.Tracks[0].ID != 2 || a.cloud.At != now {
		t.Fatal("automatic refresh did not replace the old library")
	}
}

// With NM_FRAMES set, inspect the same absent service artwork in the list, bar and all three large cover views.
func TestMissingArtworkFrames(t *testing.T) {
	dir := os.Getenv("NM_FRAMES")
	if dir == "" {
		t.Skip()
	}
	a, _ := cloudFixture(t)
	tr := cloudTrack(1, "没有专辑封面的歌曲")
	tr.Album.PicURL = "https://p1.music.126.net/x/109951171180358170.jpg"
	a.cloud = cloudvm.Cloud{CloudListing: cloudvm.CloudListing{Tracks: []netease.Track{tr}, Count: 1}, Loaded: true}
	a.pl.Cue([]netease.Track{tr})
	a.pl.SetLyrics(nil)
	for _, tc := range []struct {
		name string
		pane int
		open bool
	}{{"list", playbackview.PaneLyrics, false}, {"classic", playbackview.PaneLyrics, true}, {"stage", playbackview.PaneStage, true}, {"wall", playbackview.PaneWall, true}} {
		a.playbackState.Open, a.playbackState.Pane = tc.open, tc.pane
		tt := ui.NewTester(a.view, 1200, 800)
		tt.Frame()
		tt.Advance(2 * time.Second)
		tt.Frame()
		f, err := os.Create(filepath.Join(dir, "missing-cover-"+tc.name+".png"))
		if err != nil {
			t.Fatal(err)
		}
		err = png.Encode(f, tt.Image())
		f.Close()
		if err != nil {
			t.Fatal(err)
		}
	}
}

// Opening the cloud library loads every page without scrolling, preserves rows on failure and retries at the same offset.
func TestCloudAutomaticallyLoadsPagesAndRetries(t *testing.T) {
	a, posts := cloudFixture(t)
	tracks := make([]netease.Track, 100)
	for i := range tracks {
		tracks[i] = cloudTrack(int64(i+1), fmt.Sprintf("网盘歌曲 %03d", i+1))
	}
	a.cloud = cloudvm.Cloud{CloudListing: cloudvm.CloudListing{More: true, Count: 202, Offset: 100, Tracks: tracks}, Loaded: true}
	a.pl.Cue([]netease.Track{cloudTrack(999, "正在播放")})
	type reply struct {
		page *netease.CloudPage
		err  error
	}
	calls := make(chan int, 8)
	results := make(chan reply, 8)
	cloudSongs = func(_ context.Context, _, offset int) (*netease.CloudPage, error) {
		calls <- offset
		r := <-results
		return r.page, r.err
	}
	tt := ui.NewTester(a.view, 1200, 800)
	tt.Frame()
	tt.Frame()
	if got := wait(t, calls, "automatic next page"); got != 100 {
		t.Fatalf("next offset %d", got)
	}
	tt.Frame()
	tt.Frame()
	if len(calls) != 0 {
		t.Fatal("repainting started duplicate requests")
	}
	next := make([]netease.Track, 100)
	for i := range next {
		next[i] = cloudTrack(int64(101+i), fmt.Sprintf("网盘歌曲 %03d", 101+i))
	}
	results <- reply{page: &netease.CloudPage{Tracks: next, Count: 202, NextOffset: 200, HasMore: true}}
	refreshPost(t, posts)
	tt.Frame()
	if len(a.cloud.Tracks) != 200 || !a.cloud.Loading() || a.shell.PageScroll.Y != 0 {
		t.Fatal("background loading failed to continue or moved the list")
	}
	if a.pl.Current().ID != 999 || len(a.pl.Queue()) != 1 {
		t.Fatal("loading the library changed unrelated playback")
	}
	if got := wait(t, calls, "automatic last page"); got != 200 {
		t.Fatalf("last offset %d", got)
	}
	results <- reply{err: netease.ErrOffline}
	refreshPost(t, posts)
	tt.Frame()
	tt.Frame()
	if a.cloud.Loading() || len(calls) != 0 || len(a.cloud.Tracks) != 200 {
		t.Fatal("failed pagination looped or discarded loaded songs")
	}
	tt.Scroll(750, 400, 0, 100000)
	tt.Frame()
	if err := tt.Click("重试加载"); err != nil {
		t.Fatal(err)
	}
	if got := wait(t, calls, "retry same page"); got != 200 {
		t.Fatalf("retry skipped offset %d", got)
	}
	results <- reply{page: &netease.CloudPage{Tracks: []netease.Track{cloudTrack(201, "末页一"), cloudTrack(202, "末页二")}, Count: 202, NextOffset: 202}}
	refreshPost(t, posts)
	tt.Frame()
	tt.Frame()
	tt.Scroll(750, 400, 0, 100000)
	tt.Frame()
	if a.cloud.More || a.cloud.Loading() || len(calls) != 0 || len(a.cloud.Tracks) != 202 {
		t.Fatal("end-of-list continued loading or lost songs")
	}
}

// The shared floating locator reveals a loaded cloud row without changing pause, progress, queue or page.
func TestCloudLocateKeepsPlayback(t *testing.T) {
	for _, playing := range []bool{false, true} {
		t.Run(fmt.Sprint(playing), func(t *testing.T) {
			a, _ := cloudFixture(t)
			tracks := make([]netease.Track, 200)
			for i := range tracks {
				tracks[i] = cloudTrack(int64(i+1), fmt.Sprintf("定位网盘歌曲 %03d", i+1))
			}
			a.cloud = cloudvm.Cloud{CloudListing: cloudvm.CloudListing{Count: 200, Offset: 200, Tracks: tracks}, Loaded: true}
			a.pl.SuspendCache()
			a.pl.Cue([]netease.Track{tracks[149], cloudTrack(999, "另一首")})
			if playing {
				a.pl.Hold(43 * time.Second)
			}
			pos := a.pl.Position()
			tt := ui.NewTester(a.view, 1200, 800)
			tt.Frame()
			if dir := os.Getenv("NM_FRAMES"); dir != "" && !playing {
				tt.Advance(time.Second)
				tt.Frame()
				f, err := os.Create(filepath.Join(dir, "cloud-locate.png"))
				if err != nil {
					t.Fatal(err)
				}
				err = png.Encode(f, tt.Image())
				f.Close()
				if err != nil {
					t.Fatal(err)
				}
			}
			clickLocate(t, tt)
			locatedRow(t, tt, tracks[149].Name, 800)
			if a.pl.Current().ID != 150 || a.pl.Playing() != playing || a.pl.Position() != pos || len(a.pl.Queue()) != 2 || a.pl.Queue()[1].ID != 999 || a.nav.Current.Page != pageCloud {
				t.Fatal("cloud locator changed playback or queue")
			}
			tt.Scroll(750, 400, 0, -100000)
			tt.Frame()
			tt.SetSize(1000, 640)
			tt.Frame()
			clickLocate(t, tt)
			locatedRow(t, tt, tracks[149].Name, 640)
		})
	}
}

// A pending locator reveals its target as soon as it arrives while background loading continues to complete the library.
func TestCloudLocateRevealsBeforeLoadingCompletes(t *testing.T) {
	a, posts := cloudFixture(t)
	page := func(start int) []netease.Track {
		tracks := make([]netease.Track, 100)
		for i := range tracks {
			tracks[i] = cloudTrack(int64(start+i), fmt.Sprintf("分页定位歌曲 %03d", start+i))
		}
		return tracks
	}
	a.cloud = cloudvm.Cloud{CloudListing: cloudvm.CloudListing{More: true, Count: 400, Offset: 100, Tracks: page(1)}, Loaded: true}
	a.pl.SuspendCache()
	a.pl.Cue([]netease.Track{cloudTrack(250, "分页定位歌曲 250"), cloudTrack(999, "另一首")})
	a.pl.Hold(37 * time.Second)
	calls := make(chan int, 4)
	results := make(chan *netease.CloudPage, 4)
	cloudSongs = func(_ context.Context, _, offset int) (*netease.CloudPage, error) {
		calls <- offset
		return <-results, nil
	}
	tt := ui.NewTester(a.view, 1200, 800)
	tt.Frame()
	if err := tt.Click("定位正在播放的歌曲"); err != nil {
		t.Fatal(err)
	}
	if got := wait(t, calls, "locate first missing page"); got != 100 {
		t.Fatalf("first locate offset %d", got)
	}
	results <- &netease.CloudPage{Tracks: page(101), Count: 400, NextOffset: 200, HasMore: true}
	refreshPost(t, posts)
	if got := wait(t, calls, "locate target page"); got != 200 {
		t.Fatalf("next locate offset %d", got)
	}
	results <- &netease.CloudPage{Tracks: page(201), Count: 400, NextOffset: 300, HasMore: true}
	refreshPost(t, posts)
	tt.Frame()
	locatedRow(t, tt, "分页定位歌曲 250", 800)
	if a.cloud.Locate != 0 || !a.cloud.Loading() || len(a.cloud.Tracks) != 300 || !a.cloud.More {
		t.Fatal("locator did not reveal its target while the library kept loading")
	}
	if got := wait(t, calls, "remaining cloud page"); got != 300 {
		t.Fatalf("remaining page offset %d", got)
	}
	results <- &netease.CloudPage{Tracks: page(301), Count: 400, NextOffset: 400}
	refreshPost(t, posts)
	tt.Frame()
	locatedRow(t, tt, "分页定位歌曲 250", 800)
	if a.cloud.Loading() || a.cloud.More || len(a.cloud.Tracks) != 400 {
		t.Fatal("cloud library did not finish after locating the target")
	}
	if a.pl.Current().ID != 250 || !a.pl.Playing() || a.pl.Position() != 37*time.Second || len(a.pl.Queue()) != 2 {
		t.Fatal("loading locate pages changed playback")
	}
}

// A song change during a pending locate cancels the old target instead of scrolling to the previous song.
func TestCloudLocateDiscardsPreviousSongTarget(t *testing.T) {
	a, posts := cloudFixture(t)
	tracks := make([]netease.Track, 100)
	for i := range tracks {
		tracks[i] = cloudTrack(int64(i+1), fmt.Sprintf("前页 %d", i+1))
	}
	a.cloud = cloudvm.Cloud{CloudListing: cloudvm.CloudListing{More: true, Count: 300, Offset: 100, Tracks: tracks}, Loaded: true}
	a.pl.SuspendCache()
	a.pl.Cue([]netease.Track{cloudTrack(250, "旧定位目标")})
	calls := make(chan int, 4)
	release := make(chan struct{})
	unblock := sync.OnceFunc(func() { close(release) })
	defer unblock()
	cloudSongs = func(_ context.Context, _, offset int) (*netease.CloudPage, error) {
		calls <- offset
		<-release
		return &netease.CloudPage{Tracks: []netease.Track{cloudTrack(101, "新页")}, Count: 300, NextOffset: 200}, nil
	}
	tt := ui.NewTester(a.view, 1200, 800)
	tt.Frame()
	if err := tt.Click("定位正在播放的歌曲"); err != nil {
		t.Fatal(err)
	}
	wait(t, calls, "pending locate page")
	a.pl.Cue([]netease.Track{cloudTrack(999, "新的播放歌曲")})
	unblock()
	refreshPost(t, posts)
	tt.Frame()
	if a.cloud.Locate != 0 || a.cloud.Loading() || len(calls) != 0 || a.shell.PageScroll.Y != 0 || a.pl.Current().ID != 999 {
		t.Fatal("stale locate continued or moved the new song")
	}
}

// Leaving the cloud page during a locate must not scroll the newly opened playlist when the old page arrives.
func TestCloudLocateDoesNotScrollAnotherPage(t *testing.T) {
	a, posts := cloudFixture(t)
	tracks := make([]netease.Track, 100)
	for i := range tracks {
		tracks[i] = cloudTrack(int64(i+1), fmt.Sprintf("前页 %d", i+1))
	}
	a.cloud = cloudvm.Cloud{CloudListing: cloudvm.CloudListing{More: true, Count: 300, Offset: 100, Tracks: tracks}, Loaded: true}
	a.pl.SuspendCache()
	a.pl.Cue([]netease.Track{cloudTrack(250, "当前歌曲")})
	started, release := make(chan struct{}), make(chan struct{})
	unblock := sync.OnceFunc(func() { close(release) })
	defer unblock()
	cloudSongs = func(context.Context, int, int) (*netease.CloudPage, error) {
		close(started)
		<-release
		return &netease.CloudPage{Tracks: []netease.Track{cloudTrack(250, "当前歌曲")}, Count: 300, NextOffset: 200}, nil
	}
	tt := ui.NewTester(a.view, 1200, 800)
	tt.Frame()
	if err := tt.Click("定位正在播放的歌曲"); err != nil {
		t.Fatal(err)
	}
	wait(t, started, "pending locate")
	other := make([]netease.Track, 500)
	for i := range other {
		other[i] = netease.Track{ID: int64(10000 + i), Name: fmt.Sprintf("其他歌曲 %03d", i+1)}
	}
	a.library.Playlists[7] = &netease.Playlist{ID: 7, Name: "另一张歌单", Tracks: other}
	a.nav.Current = route{Page: pagePlaylist, ID: 7}
	tt.Frame()
	unblock()
	refreshPost(t, posts)
	tt.Frame()
	if a.cloud.Locate != 0 || a.cloud.Loading() || a.shell.PageScroll.Y != 0 || a.nav.Current != (route{Page: pagePlaylist, ID: 7}) {
		t.Fatal("old cloud locate affected the new page")
	}
}

// Navigating away and back before a locate response arrives must not revive the abandoned target.
func TestCloudLocateStaysCancelledAfterReturning(t *testing.T) {
	a, posts := cloudFixture(t)
	tracks := make([]netease.Track, 100)
	for i := range tracks {
		tracks[i] = cloudTrack(int64(i+1), fmt.Sprintf("前页 %d", i+1))
	}
	a.cloud = cloudvm.Cloud{CloudListing: cloudvm.CloudListing{More: true, Count: 300, Offset: 100, Tracks: tracks}, Loaded: true}
	a.pl.SuspendCache()
	a.pl.Cue([]netease.Track{cloudTrack(250, "当前歌曲")})
	started, release := make(chan struct{}), make(chan struct{})
	unblock := sync.OnceFunc(func() { close(release) })
	defer unblock()
	cloudSongs = func(context.Context, int, int) (*netease.CloudPage, error) {
		close(started)
		<-release
		return &netease.CloudPage{Tracks: []netease.Track{cloudTrack(250, "当前歌曲")}, Count: 300, NextOffset: 200}, nil
	}
	tt := ui.NewTester(a.view, 1200, 800)
	tt.Frame()
	if err := tt.Click("定位正在播放的歌曲"); err != nil {
		t.Fatal(err)
	}
	wait(t, started, "pending cloud locate")
	a.open(route{Page: pageLocal})
	a.back()
	if a.cloud.Locate != 0 {
		t.Fatal("leaving the page retained its pending target")
	}
	unblock()
	refreshPost(t, posts)
	tt.Frame()
	if a.cloud.Locate != 0 || a.cloud.Loading() || a.shell.PageScroll.Y != 0 || a.nav.Current.Page != pageCloud {
		t.Fatal("returning revived the old locate action")
	}
}

// Render an in-flight load through its command without exposing execution tokens.
func holdCloudLoad(t *testing.T, m *cloudvm.Cloud) {
	t.Helper()
	m.More = true
	m.Load(cloudvm.CloudNextPage, cloudvm.CloudServices{
		Fetch: func(ctx context.Context, _, _ int) (*netease.CloudPage, error) { <-ctx.Done(); return nil, ctx.Err() },
		Post:  func(func() bool) {},
	})
	t.Cleanup(m.Stop)
}
