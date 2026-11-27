/*
验证切页与前进后退恢复阅读位置，覆盖延迟加载、行变更和账号边界。
*/

package app

import (
	"fmt"
	"math"
	"testing"
	"time"

	"github.com/egoist/mygo/ui"

	"netease-music-mygo/internal/features/netease"
	searchvm "netease-music-mygo/internal/ui/viewmodel/search"
)

func navigationPositionFixture(t *testing.T) (*app, *ui.Tester, []netease.Track) {
	t.Helper()
	isolateUserData(t)
	a := newTestApp()
	a.settings = defaultSettings()
	tracks := make([]netease.Track, 300)
	for i := range tracks {
		tracks[i] = netease.Track{ID: int64(i + 1), Name: fmt.Sprintf("位置曲目%d", i+1)}
	}
	for _, id := range []int64{41, 42} {
		a.library.Playlists[id] = &netease.Playlist{ID: id, Name: fmt.Sprintf("位置歌单%d", id), Tracks: tracks}
	}
	for i := range 60 {
		a.home.Charts = append(a.home.Charts, netease.Playlist{ID: int64(100 + i), Name: fmt.Sprintf("位置榜单%d", i+1)})
	}
	a.nav.Current = route{Page: pageCharts}
	tt := ui.NewTesterWith(a.view, 1180, 780, ui.TesterOptions{Preferences: ui.Preferences{ReduceMotion: true}})
	t.Cleanup(a.pl.Close)
	return a, tt, tracks
}

func settleNavigation(tt *ui.Tester) {
	tt.Frame()
	tt.Advance(time.Second)
	tt.Frame()
}

func readingRow(t *testing.T, tt *ui.Tester, tracks []netease.Track) (string, float32) {
	t.Helper()
	for _, tr := range tracks {
		if item, ok := tt.AccessByRole(ui.RoleButton, tr.Name); ok && !item.Offscreen && item.Bounds.Y >= buttonsHeight+pageGap && item.Bounds.Y+item.Bounds.H < 780-playerBarHeight {
			return tr.Name, item.Bounds.Y
		}
	}
	t.Fatal("no song row below the toolbar")
	return "", 0
}

func requireReadingRow(t *testing.T, tt *ui.Tester, name string, before float32) {
	t.Helper()
	row, ok := tt.AccessByRole(ui.RoleButton, name)
	if !ok || row.Offscreen || math.Abs(float64(row.Bounds.Y-before)) > 54 {
		t.Fatalf("reading row %s was not restored: y=%v, want near %v (exists=%v)", name, row.Bounds.Y, before, ok)
	}
}

// Back/Forward and sidebar revisits restore each page, without replaying the
// restore on subsequent frames or disturbing a page covered by Now Playing.
func TestNavigationRestoresPagePositions(t *testing.T) {
	a, tt, tracks := navigationPositionFixture(t)
	tt.Wheel(700, 400, 0, 1900)
	settleNavigation(tt)
	chartY := a.shell.PageScroll.Y
	if chartY < 500 {
		t.Fatal("charts fixture did not scroll")
	}
	first := route{Page: pagePlaylist, ID: 41}
	a.open(first)
	settleNavigation(tt)
	tt.Wheel(700, 400, 0, 5300)
	settleNavigation(tt)
	name, before := readingRow(t, tt, tracks)
	if err := tt.Click("返回"); err != nil {
		t.Fatal(err)
	}
	settleNavigation(tt)
	if a.nav.Current.Page != pageCharts || math.Abs(float64(a.shell.PageScroll.Y-chartY)) > 0.1 {
		t.Fatal("back lost the charts offset")
	}
	if err := tt.Click("前进"); err != nil {
		t.Fatal(err)
	}
	settleNavigation(tt)
	requireReadingRow(t, tt, name, before)
	// Reopening through another navigation entry also keeps the last place.
	a.open(route{Page: pagePlaylist, ID: 42})
	settleNavigation(tt)
	if a.shell.PageScroll.Y > 1 {
		t.Fatal("a new page inherited the preceding list's offset")
	}
	a.open(first)
	settleNavigation(tt)
	requireReadingRow(t, tt, name, before)
	tt.Wheel(700, 400, 0, 780)
	settleNavigation(tt)
	moved, y := readingRow(t, tt, tracks)
	if moved == name {
		t.Fatal("restoration prevented further scrolling")
	}
	a.playbackState.Open = true
	settleNavigation(tt)
	a.back()
	settleNavigation(tt)
	requireReadingRow(t, tt, moved, y)
}

// A return through a loading page keeps its saved song identity until real
// rows arrive, even when the song has moved far beyond MyGo's search range.
func TestNavigationPositionWaitsForRows(t *testing.T) {
	a, tt, tracks := navigationPositionFixture(t)
	first := route{Page: pagePlaylist, ID: 41}
	a.open(first)
	settleNavigation(tt)
	tt.Wheel(700, 400, 0, 5300)
	settleNavigation(tt)
	name, before := readingRow(t, tt, tracks)
	a.open(route{Page: pagePlaylist, ID: 42})
	settleNavigation(tt)
	delete(a.library.Playlists, 41)
	a.library.Loading[41] = true
	a.back()
	settleNavigation(tt)
	// Leave the skeleton before it finishes. It must not overwrite the saved row.
	a.goForward()
	settleNavigation(tt)
	a.back()
	settleNavigation(tt)
	// A failed/empty response may build a header and retry control, not rows.
	a.library.Playlists[41] = &netease.Playlist{ID: 41, Name: "等待重试"}
	delete(a.library.Loading, 41)
	settleNavigation(tt)
	a.goForward()
	settleNavigation(tt)
	a.back()
	settleNavigation(tt)
	inserted := make([]netease.Track, 1200)
	for i := range inserted {
		inserted[i] = netease.Track{ID: int64(10000 + i), Name: fmt.Sprintf("插入曲目%d", i)}
	}
	changed := append(inserted, tracks...)
	a.library.Playlists[41] = &netease.Playlist{ID: 41, Name: "已刷新歌单", Tracks: changed}
	delete(a.library.Loading, 41)
	tt.SetSize(1000, 780)
	settleNavigation(tt)
	requireReadingRow(t, tt, name, before)
	// Later frames do not reapply the navigation.
	tt.Wheel(700, 400, 0, 700)
	settleNavigation(tt)
	if next, _ := readingRow(t, tt, changed); next == name {
		t.Fatal("the completed restore still owns the viewport")
	}
}

// A deleted anchor and a drastically shorter replacement cannot restore an
// out-of-range row or an empty viewport; returning still shows valid content.
func TestNavigationPositionClampsDeletedRows(t *testing.T) {
	a, tt, tracks := navigationPositionFixture(t)
	a.open(route{Page: pagePlaylist, ID: 41})
	settleNavigation(tt)
	tt.Wheel(700, 400, 0, 9500)
	settleNavigation(tt)
	old, _ := readingRow(t, tt, tracks)
	a.open(route{Page: pagePlaylist, ID: 42})
	settleNavigation(tt)
	a.library.Playlists[41].Tracks = append([]netease.Track(nil), tracks[:8]...)
	a.back()
	settleNavigation(tt)
	if name, _ := readingRow(t, tt, tracks[:8]); name == old || a.shell.PageScroll.Y > a.shell.PageScroll.MaxY {
		t.Fatal("deleted rows left the restored viewport outside the content")
	}
}

// Repeated songs restore the occurrence being read, rather than jumping to
// the first occurrence of the same song ID elsewhere in the playlist.
func TestNavigationPositionRepeatedSongs(t *testing.T) {
	a, tt, tracks := navigationPositionFixture(t)
	for i := 150; i < len(tracks); i++ {
		tracks[i].ID = tracks[i-150].ID
	}
	a.open(route{Page: pagePlaylist, ID: 41})
	settleNavigation(tt)
	tt.Wheel(700, 400, 0, 10000)
	settleNavigation(tt)
	name, y := readingRow(t, tt, tracks)
	for _, tr := range tracks[:150] {
		if tr.Name == name {
			t.Fatal("fixture did not reach the second occurrence")
		}
	}
	a.open(route{Page: pagePlaylist, ID: 42})
	settleNavigation(tt)
	a.back()
	settleNavigation(tt)
	requireReadingRow(t, tt, name, y)
}

// Different search queries and kinds do not borrow another search's position.
func TestNavigationSearchPositionsAreIndependent(t *testing.T) {
	a, tt, tracks := navigationPositionFixture(t)
	queries := []route{{Page: pageSearch, Query: "one", Kind: netease.SearchSongs}, {Page: pageSearch, Query: "two", Kind: netease.SearchSongs}, {Page: pageSearch, Query: "one", Kind: netease.SearchArtists}}
	for _, r := range queries {
		d := &searchvm.SearchPage{Res: netease.SearchResult{Songs: tracks}}
		d.Page.Loaded = true
		a.searches.Pages.Set(r, d)
	}
	a.open(queries[0])
	settleNavigation(tt)
	tt.Wheel(700, 400, 0, 2600)
	settleNavigation(tt)
	name, y := readingRow(t, tt, tracks)
	for _, r := range queries[1:] {
		a.open(r)
		settleNavigation(tt)
		if a.shell.PageScroll.Y > 1 {
			t.Fatal("new search borrowed another result's position")
		}
	}
	a.back()
	settleNavigation(tt)
	a.back()
	settleNavigation(tt)
	requireReadingRow(t, tt, name, y)
}

// A search tab navigates while the old scrolling header builds. Its partial
// header offset must be saved after that list finishes, without resetting it.
func TestNavigationPositionFromListHeader(t *testing.T) {
	a, tt, tracks := navigationPositionFixture(t)
	songs := route{Page: pageSearch, Query: "header", Kind: netease.SearchSongs}
	artists := route{Page: pageSearch, Query: "header", Kind: netease.SearchArtists}
	for _, r := range []route{songs, artists} {
		d := &searchvm.SearchPage{Res: netease.SearchResult{Songs: tracks}}
		d.Page.Loaded = true
		a.searches.Pages.Set(r, d)
	}
	a.open(songs)
	settleNavigation(tt)
	a.open(route{Page: pagePlaylist, ID: 42})
	settleNavigation(tt)
	a.open(songs)
	settleNavigation(tt)
	tt.Wheel(700, 400, 0, 35)
	settleNavigation(tt)
	y := a.shell.PageScroll.Y
	if y < 20 {
		t.Fatal("header did not scroll")
	}
	if err := tt.Click("歌手"); err != nil {
		t.Fatal(err)
	}
	settleNavigation(tt)
	if a.nav.Current != artists {
		t.Fatal("the header did not navigate")
	}
	// Search tabs replace the current route; the older song visit remains
	// reachable behind the intervening playlist and uses its latest position.
	a.back()
	settleNavigation(tt)
	a.back()
	settleNavigation(tt)
	if a.nav.Current != songs || math.Abs(float64(a.shell.PageScroll.Y-y)) > 0.1 {
		t.Fatalf("header navigation lost the partial header offset: %v -> %v", y, a.shell.PageScroll.Y)
	}
}

// Account transitions retire positions immediately and reset the still-visible
// route on its next build, instead of saving the old account's viewport again.
func TestNavigationPositionAccountLifetime(t *testing.T) {
	a, posts := offlineFixture(t)
	fakeOfflineHome(t)
	r := route{Page: pageCharts}
	a.nav.Current = r
	var charts []netease.Playlist
	for i := range 40 {
		charts = append(charts, netease.Playlist{ID: int64(i + 1), Name: fmt.Sprintf("账号榜单%d", i+1)})
	}
	a.home.Charts = charts
	tt := ui.NewTesterWith(a.view, 1180, 780, ui.TesterOptions{Preferences: ui.Preferences{ReduceMotion: true}})
	tt.Wheel(700, 400, 0, 1900)
	settleNavigation(tt)
	if a.shell.PageScroll.Y < 500 {
		t.Fatal("account fixture did not scroll")
	}
	a.open(route{Page: pageFM})
	settleNavigation(tt)
	a.back()
	settleNavigation(tt)
	a.signedIn(nil)
	for a.home.Loading {
		refreshPost(t, posts)
	}
	a.home.Charts = charts
	settleNavigation(tt)
	if a.shell.PageScroll.Y > 1 {
		t.Fatal("account change kept the visible route's old offset")
	}
	a.open(route{Page: pageFM})
	settleNavigation(tt)
	a.back()
	settleNavigation(tt)
	if a.shell.PageScroll.Y > 1 {
		t.Fatal("navigation saved the old account's offset again after reset")
	}
}
