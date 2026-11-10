package app

import (
	"testing"
	"time"

	"github.com/egoist/mygo/ui"

	"netease-music-mygo/internal/netease"
)

// Each playlist header hides only its own rows without changing the page
// or queue, and Enter restores their navigation.
func TestSidebarPlaylistCollapse(t *testing.T) {
	for _, tc := range []struct {
		name, header, row, other string
		playlist                 int64
	}{
		{"mine", "我的歌单", "睡前", "飙升榜", 11},
		{"saved", "收藏的歌单", "飙升榜", "睡前", 20},
	} {
		t.Run(tc.name, func(t *testing.T) {
			a := editTestApp()
			a.playlists[tc.playlist] = &netease.Playlist{ID: tc.playlist, Name: tc.row}
			a.pl.Cue(a.playlists[10].Tracks)
			before := a.route
			tt := ui.NewTester(a.view, 1200, 800)
			tt.Frame()

			if err := tt.Click(tc.header); err != nil {
				t.Fatal(err)
			}
			tt.Frame()
			if _, ok := tt.Find(tc.row); ok {
				t.Error("the collapsed section still builds its playlist rows")
			}
			if _, ok := tt.Find(tc.other); !ok {
				t.Error("collapsing one section hid the other section's rows")
			}
			if a.route != before || len(a.pl.Queue()) != 2 || len(a.mine) != 3 || len(a.saved) != 1 {
				t.Error("collapsing the section changed the page, queue or playlists")
			}

			tt.Key(0, ui.KeyEnter)
			tt.Frame()
			if err := tt.Click(tc.row); err != nil {
				t.Fatalf("the restored playlist cannot be opened: %v", err)
			}
			tt.Frame()
			if a.route != (route{page: pagePlaylist, id: tc.playlist}) {
				t.Errorf("opened %+v, want playlist %d", a.route, tc.playlist)
			}
		})
	}
}

// The albums the listener saved have their own header in the sidebar, each
// opening its page, and the header hides them without losing one.
func TestSidebarSavedAlbums(t *testing.T) {
	a := editTestApp()
	a.subAlbums = []netease.AlbumProfile{
		{Album: netease.Album{ID: 77, Name: "神的游戏"}, Artists: []netease.Artist{{ID: 3, Name: "张悬"}}},
		{Album: netease.Album{ID: 78, Name: "城市"}, Artists: []netease.Artist{{ID: 3, Name: "张悬"}}},
	}
	a.subsShown = true
	// The saved albums were just loaded, so nothing asks NetEase for them.
	a.listRefresh.albums.at = time.Now()
	a.albums.set(77, &albumData{album: &netease.AlbumProfile{
		Album:   netease.Album{ID: 77, Name: "神的游戏"},
		Artists: []netease.Artist{{ID: 3, Name: "张悬"}},
	}})
	tt := ui.NewTester(a.view, 1200, 800)
	tt.Frame()
	if err := tt.Click("神的游戏"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	if a.route != (route{page: pageAlbum, id: 77}) {
		t.Fatalf("opened %+v, want album 77", a.route)
	}
	if err := tt.Click("收藏的专辑"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	if _, ok := tt.Find("城市"); ok {
		t.Error("the collapsed albums still show")
	}
	if !a.albumsCollapsed || len(a.subAlbums) != 2 {
		t.Error("collapsing the albums changed which are saved")
	}
}

// The plus still opens the naming dialog with both sections collapsed, without expanding either one.
func TestNewPlaylistInCollapsedSidebar(t *testing.T) {
	a := editTestApp()
	tt := ui.NewTester(a.view, 1200, 800)
	tt.Frame()
	for _, header := range []string{"我的歌单", "收藏的歌单"} {
		if err := tt.Click(header); err != nil {
			t.Fatal(err)
		}
		tt.Frame()
	}
	if !a.mineCollapsed || !a.savedCollapsed {
		t.Fatal("the sections did not collapse independently")
	}
	if err := tt.Click("新建歌单"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	if !a.naming.open {
		t.Error("the plus did not open the naming dialog")
	}
	if !a.mineCollapsed || !a.savedCollapsed {
		t.Error("the plus also toggled a playlist section")
	}
}
