package app

import (
	"errors"
	"testing"
	"time"

	"github.com/egoist/mygo/ui"

	"netease-music-mygo/internal/netease"
)

// albumTestApp is signed in, showing one album that the listener has not
// saved.
func albumTestApp(t *testing.T) *app {
	t.Helper()
	isolateUserData(t)
	a := newTestApp()
	a.settings = defaultSettings()
	a.profile = &netease.Profile{UserID: 1, Nickname: "小明"}
	a.albums.set(77, &albumData{album: &netease.AlbumProfile{
		Album:   netease.Album{ID: 77, Name: "神的游戏", PicURL: "https://p1.music.126.net/cover.jpg"},
		Artists: []netease.Artist{{ID: 3, Name: "张悬"}},
		Songs: []netease.Track{
			{ID: 900, Name: "玫瑰色的你", Artists: []netease.Artist{{ID: 3, Name: "张悬"}}, Duration: 240000},
			{ID: 901, Name: "蓝天白云", Artists: []netease.Artist{{ID: 3, Name: "张悬"}}, Duration: 220000},
		},
	}})
	a.route = route{page: pageAlbum, id: 77}
	// The saved albums were just loaded, so the page does not ask again.
	a.listRefresh.albums.at = time.Now()
	return a
}

// fakeAlbumSave answers for the albums the listener saves and tells on each
// time one is saved or unsaved; subsBroken fails them instead.
func fakeAlbumSave(t *testing.T, fail bool) chan saveCall {
	t.Helper()
	calls := make(chan saveCall, 1)
	old := subscribeAlbum
	t.Cleanup(func() { subscribeAlbum = old })
	subscribeAlbum = func(id int64, save bool) error {
		calls <- saveCall{id, save}
		if fail {
			return errors.New("offline")
		}
		return nil
	}
	oldList := subscribedAlbums
	t.Cleanup(func() { subscribedAlbums = oldList })
	subscribedAlbums = func() ([]netease.AlbumProfile, error) { return nil, nil }
	return calls
}

// Clicking 收藏 on an album's page saves it with NetEase, and the app knows
// the listener saved it without saying so over what the button reads.
func TestSaveAlbumFromItsPage(t *testing.T) {
	calls := fakeAlbumSave(t, false)
	a := albumTestApp(t)
	posts := testPosts(a)
	tt := ui.NewTester(a.view, 1200, 800)
	tt.Frame()
	if err := tt.Click("收藏"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	if got := nextSave(t, calls); got != (saveCall{77, true}) {
		t.Errorf("asked NetEase for %+v, want album 77 saved", got)
	}
	refreshPost(t, posts)
	if !a.albumSaved(77) || a.toast != "" {
		t.Errorf("album 77 saved %v, toast %q, want saved and nothing said", a.albumSaved(77), a.toast)
	}
	if len(a.subAlbums) != 1 || a.subAlbums[0].Songs != nil {
		t.Errorf("saved albums are %+v, want album 77 alone, without its songs", a.subAlbums)
	}
}

// An album the listener saved reads 已收藏 on its page, and clicking that
// unsaves it.
func TestUnsaveAlbumFromItsPage(t *testing.T) {
	calls := fakeAlbumSave(t, false)
	a := albumTestApp(t)
	a.subAlbums, a.subsShown = []netease.AlbumProfile{{Album: netease.Album{ID: 77, Name: "神的游戏"}}}, true
	posts := testPosts(a)
	tt := ui.NewTester(a.view, 1200, 800)
	tt.Frame()
	if err := tt.Click("已收藏"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	if got := nextSave(t, calls); got != (saveCall{77, false}) {
		t.Errorf("asked NetEase for %+v, want album 77 unsaved", got)
	}
	refreshPost(t, posts)
	if a.albumSaved(77) || a.toast != "" {
		t.Errorf("album 77 saved %v, toast %q, want unsaved and nothing said", a.albumSaved(77), a.toast)
	}
}

// When NetEase refuses to save an album, the app says so and puts the album
// back as the listener had it.
func TestAlbumSaveFailureReverts(t *testing.T) {
	fakeAlbumSave(t, true)
	a := albumTestApp(t)
	posts := testPosts(a)
	tt := ui.NewTester(a.view, 1200, 800)
	tt.Frame()
	if err := tt.Click("收藏"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	if !a.albumSaved(77) {
		t.Fatal("the album did not read saved at once")
	}
	refreshPost(t, posts)
	if a.albumSaved(77) || a.toast != "操作失败，请稍后再试" {
		t.Errorf("album 77 saved %v, toast %q, want unsaved and 操作失败，请稍后再试", a.albumSaved(77), a.toast)
	}
}

// The listener's own page lists the albums they saved, each opening its page.
func TestSavedAlbumsOnOwnPage(t *testing.T) {
	fakeSubscriptions(t)
	a := albumTestApp(t)
	user := netease.User{UserID: 1, Nickname: "小明"}
	a.users.set(1, &userData{user: &user})
	a.subAlbums, a.subsShown = []netease.AlbumProfile{{Album: netease.Album{ID: 77, Name: "神的游戏"}, Artists: []netease.Artist{{ID: 3, Name: "张悬"}}}}, true
	posts := testPosts(a)
	a.open(route{page: pageUser, id: 1})
	for a.users.get(1).subsLoading > 0 {
		refreshPost(t, posts)
	}
	tt := ui.NewTester(a.view, 1200, 1200)
	tt.Frame()
	if err := tt.Click("神的游戏"); err != nil {
		t.Fatal(err)
	}
	if a.route.page != pageAlbum || a.route.id != 77 {
		t.Fatalf("route = %+v, want the page of album 77", a.route)
	}
}
