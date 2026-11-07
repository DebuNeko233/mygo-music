package app

import (
	"fmt"
	"image/png"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/egoist/mygo/ui"

	"netease-music-mygo/internal/netease"
)

func locateFixture(t *testing.T, current int, playing bool) (*app, *netease.Playlist, *ui.Tester) {
	t.Helper()
	isolateUserData(t)
	a := newTestApp()
	a.settings = defaultSettings()
	pl := &netease.Playlist{ID: 7, Name: "定位测试歌单", Tracks: make([]netease.Track, 400)}
	for i := range pl.Tracks {
		pl.Tracks[i] = netease.Track{ID: int64(i + 1), Name: fmt.Sprintf("测试歌曲 %03d", i+1), Duration: 200000, Artists: []netease.Artist{{Name: "测试歌手"}}, Album: netease.Album{Name: "测试专辑"}}
	}
	a.route = route{page: pagePlaylist, id: 7}
	a.playlists[7] = pl
	// The current song is queue index zero but is far down the displayed list.
	// Suspend loading so a broken locate action cannot launch network/audio jobs.
	a.pl.SuspendCache()
	a.pl.Cue([]netease.Track{pl.Tracks[current], {ID: 1001, Name: "另一张歌单的歌曲"}})
	if playing {
		a.pl.Hold(43 * time.Second)
	}
	tt := ui.NewTester(a.view, 1100, 760)
	tt.Frame()
	return a, pl, tt
}

func locatedRow(t *testing.T, tt *ui.Tester, name string, h float32) ui.Rect {
	t.Helper()
	row, ok := tt.AccessByRole(ui.RoleButton, name)
	if !ok || row.Offscreen || row.Bounds.Y < topBarHeight || row.Bounds.Y+row.Bounds.H > h-playerBarHeight {
		t.Fatalf("current track not visible between the page and player bars: %+v, found %v", row.Bounds, ok)
	}
	return row.Bounds
}

func clickLocate(t *testing.T, tt *ui.Tester) {
	t.Helper()
	if err := tt.Click("定位正在播放的歌曲"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	if _, ok := tt.Find("定位正在播放的歌曲"); ok {
		t.Fatal("locate remained after the current row became visible")
	}
}

// Locating an unbuilt virtual row must use the displayed playlist's index,
// and preserve the current song, pause state, progress, queue and page.
func TestPlaylistLocateFindsCurrentWithoutPlaying(t *testing.T) {
	for _, playing := range []bool{false, true} {
		t.Run(fmt.Sprint(playing), func(t *testing.T) {
			a, pl, tt := locateFixture(t, 249, playing)
			name := pl.Tracks[249].Name
			if row, ok := tt.AccessByRole(ui.RoleButton, name); ok && !row.Offscreen {
				t.Fatal("fixture current row already visible")
			}
			queue := slices.Clone(a.pl.Queue())
			pos := a.pl.Position()
			beforeRoute := a.route
			clickLocate(t, tt)
			locatedRow(t, tt, name, 760)
			if a.pl.Current().ID != 250 || a.pl.Playing() != playing || a.pl.Position() != pos || !reflect.DeepEqual(a.pl.Queue(), queue) || a.route != beforeRoute || a.nowPlaying {
				t.Fatal("locating changed playback, queue or page")
			}
		})
	}
}

// The floating control stays above the player while the list scrolls or
// the window resizes, and both pointer and keyboard activation can locate.
func TestPlaylistLocateFloatsAndSupportsKeyboard(t *testing.T) {
	a, pl, tt := locateFixture(t, 199, false)
	before, ok := tt.Find("定位正在播放的歌曲")
	if !ok {
		t.Fatal("no locate control")
	}
	tt.Scroll(750, 400, 0, 100000)
	tt.Advance(time.Second)
	tt.Frame()
	after, _ := tt.Find("定位正在播放的歌曲")
	if before != after {
		t.Fatalf("locate scrolled with songs: %v -> %v", before, after)
	}
	clickLocate(t, tt)
	locatedRow(t, tt, pl.Tracks[199].Name, 760)
	tt.Scroll(750, 400, 0, -100000)
	tt.Advance(time.Second)
	tt.Frame()
	// The button disappears after locating and loses focus. Tab to the
	// newly shown control before activating it with the keyboard again.
	for i := 0; i < 200 && !tt.Focused("定位正在播放的歌曲"); i++ {
		tt.Key(0, ui.KeyTab)
	}
	if !tt.Focused("定位正在播放的歌曲") {
		t.Fatal("locate cannot receive keyboard focus")
	}
	tt.Key(0, ui.KeyEnter)
	tt.Frame()
	locatedRow(t, tt, pl.Tracks[199].Name, 760)
	tt.Scroll(750, 400, 0, -100000)
	tt.Advance(time.Second)
	tt.Frame()
	tt.SetSize(1000, 640)
	tt.Frame()
	button, _ := tt.Find("定位正在播放的歌曲")
	if button.Y+button.H > 640-playerBarHeight-8 || button.X < sidebarWidth || button.X+button.W > 1000 {
		t.Fatal("locate overlaps the player or leaves the page after resize")
	}
	clickLocate(t, tt)
	locatedRow(t, tt, pl.Tracks[199].Name, 640)
	if a.pl.Playing() {
		t.Fatal("locating resumed paused playback")
	}
}

// Changing the current song or refreshing/reordering the playlist must
// resolve its new index when clicked, without automatically chasing it.
func TestPlaylistLocateUsesLatestSongAndList(t *testing.T) {
	a, pl, tt := locateFixture(t, 99, false)
	clickLocate(t, tt)
	a.pl.Cue([]netease.Track{pl.Tracks[349]})
	tt.Frame()
	if row, ok := tt.AccessByRole(ui.RoleButton, pl.Tracks[349].Name); ok && !row.Offscreen {
		t.Fatal("song change scrolled the page without a click")
	}
	clickLocate(t, tt)
	locatedRow(t, tt, pl.Tracks[349].Name, 760)
	current := *a.pl.Current()
	slices.Reverse(pl.Tracks)
	pl.Tracks = append([]netease.Track{{ID: 2001, Name: "新加入的歌曲", Duration: 200000}}, pl.Tracks...)
	tt.Frame()
	clickLocate(t, tt)
	locatedRow(t, tt, current.Name, 760)
	if a.pl.Current().ID != current.ID {
		t.Fatal("playlist refresh changed what plays")
	}
	pl.Tracks = []netease.Track{{ID: 2001, Name: "新加入的歌曲", Duration: 200000}}
	tt.Frame()
	if _, ok := tt.Find("定位正在播放的歌曲"); ok {
		t.Fatal("locate remained after current song was removed")
	}
}

// Scrolling hides the control while any part of the row is visible;
// overscan rows and rows fully covered by the bars must not hide it.
func TestPlaylistLocateFollowsVisibleArea(t *testing.T) {
	a, pl, tt := locateFixture(t, 99, false)
	clickLocate(t, tt)
	name := pl.Tracks[99].Name
	original := locatedRow(t, tt, name, 760)
	offset := a.pageScroll.Y
	// Match the platform bar insets in this headless fixture.
	top := float32(buttonsHeight)
	for _, tc := range []struct {
		name  string
		y     float32
		shown bool
	}{
		{"partly below top", top - original.H + 8, false},
		{"covered by top", top - original.H - 1, true},
		{"back in middle", original.Y, false},
		{"partly above player", 760 - playerBarHeight - 20, false},
		{"covered by player", 760 - playerBarHeight + 20, true},
		{"back again", original.Y, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			a.pageScroll.Y = offset + original.Y - tc.y
			tt.Frame()
			_, shown := tt.Find("定位正在播放的歌曲")
			if shown != tc.shown {
				row, _ := tt.AccessByRole(ui.RoleButton, name)
				t.Fatalf("locate shown %v, want %v; row %+v", shown, tc.shown, row.Bounds)
			}
		})
	}
	// A larger window can expose a row previously hidden below the player.
	a.pageScroll.Y = offset + original.Y - (760 - playerBarHeight + 20)
	tt.Frame()
	tt.SetSize(1100, 980)
	tt.Frame()
	if _, shown := tt.Find("定位正在播放的歌曲"); shown {
		t.Fatal("locate remained when resizing revealed the current row")
	}
	tt.SetSize(1100, 760)
	tt.Frame()
	if _, shown := tt.Find("定位正在播放的歌曲"); !shown {
		t.Fatal("locate did not return after resizing covered the row")
	}
}

// A current song visible at page entry needs no locate button, including
// when a different visible song becomes current without scrolling.
func TestPlaylistLocateHiddenForVisibleSong(t *testing.T) {
	a, pl, tt := locateFixture(t, 0, false)
	locatedRow(t, tt, pl.Tracks[0].Name, 760)
	if _, shown := tt.Find("定位正在播放的歌曲"); shown {
		t.Fatal("locate shown for the visible current song")
	}
	a.pl.Cue([]netease.Track{pl.Tracks[1]})
	tt.Frame()
	locatedRow(t, tt, pl.Tracks[1].Name, 760)
	if _, shown := tt.Find("定位正在播放的歌曲"); shown {
		t.Fatal("locate shown after switching to another visible song")
	}
	tt.Scroll(750, 400, 0, 100000)
	tt.Advance(time.Second)
	tt.Frame()
	if _, shown := tt.Find("定位正在播放的歌曲"); !shown {
		t.Fatal("locate did not return after manually scrolling away")
	}
}

// Refreshing can move an unbuilt current row into the same viewport without
// changing the scroll offset or range. Its first measured frame must hide locate.
func TestPlaylistLocateVisibilityAfterReorder(t *testing.T) {
	_, pl, tt := locateFixture(t, 249, false)
	name := pl.Tracks[249].Name
	pl.Tracks[0], pl.Tracks[249] = pl.Tracks[249], pl.Tracks[0]
	tt.Frame()
	locatedRow(t, tt, name, 760)
	if _, shown := tt.Find("定位正在播放的歌曲"); shown {
		t.Fatal("locate remained after refresh moved the current row into view")
	}
	pl.Tracks[0], pl.Tracks[249] = pl.Tracks[249], pl.Tracks[0]
	tt.Frame()
	if _, shown := tt.Find("定位正在播放的歌曲"); !shown {
		t.Fatal("locate did not return when refresh moved the row out of view")
	}
}

// Renaming or refreshing a playlist header used to leave locate hidden
// when text wrapping covered the current row without changing the list range.
func TestPlaylistLocateFollowsHeaderChanges(t *testing.T) {
	for _, field := range []string{"name", "description"} {
		t.Run(field, func(t *testing.T) {
			a, pl, tt := locateFixture(t, 7, false)
			longName := strings.Repeat("A very long playlist name ", 20)
			longDescription := strings.Repeat("A playlist description with several words. ", 30)
			if field == "name" {
				pl.Description = longDescription
			} else {
				pl.Name = longName
			}
			tt.Frame()
			row, ok := tt.AccessByRole(ui.RoleButton, pl.Tracks[7].Name)
			if !ok {
				t.Fatal("fixture current row not built")
			}
			// Keep only the top two DIPs of the row visible, independently of
			// font metrics. A slightly taller header will cover it completely.
			h := int(row.Bounds.Y) + playerBarHeight + 2
			tt.SetSize(1100, h)
			tt.Move(100, 100)
			tt.Advance(time.Second)
			tt.Frame()
			if _, shown := tt.Find("定位正在播放的歌曲"); shown {
				t.Fatal("locate shown before header grows")
			}
			first, last := a.trackList.Visible()
			offset := a.pageScroll.Y
			name, description := pl.Name, pl.Description
			if field == "name" {
				pl.Name = longName
			} else {
				pl.Description = longDescription
			}
			tt.Frame()
			row, ok = tt.AccessByRole(ui.RoleButton, pl.Tracks[7].Name)
			if !ok || row.Bounds.Y < float32(h-playerBarHeight) {
				t.Fatal("fixture header did not cover the row")
			}
			if lo, hi := a.trackList.Visible(); lo != first || hi != last || a.pageScroll.Y != offset {
				t.Fatal("fixture changed the list range or scroll offset")
			}
			if _, shown := tt.Find("定位正在播放的歌曲"); !shown {
				t.Fatal("locate remained hidden after header covered current row")
			}
			pl.Name, pl.Description = name, description
			tt.Frame()
			if _, shown := tt.Find("定位正在播放的歌曲"); shown {
				t.Fatal("locate remained after shorter header revealed the row")
			}
		})
	}
}

// No control is offered while loading, without a current song, on another
// kind of page, or when the current song does not belong to this playlist.
func TestPlaylistLocateOnlyForMatchingPlaylist(t *testing.T) {
	for _, scenario := range []string{"no current", "another playlist", "empty", "loading", "daily"} {
		t.Run(scenario, func(t *testing.T) {
			a, pl, tt := locateFixture(t, 49, false)
			switch scenario {
			case "no current":
				a.pl = newTestApp().pl
			case "another playlist":
				a.pl.Cue([]netease.Track{{ID: 9001, Name: "别处的歌曲"}})
			case "empty":
				pl.Tracks = nil
			case "loading":
				delete(a.playlists, 7)
				a.loadingPl[7] = true
			case "daily":
				a.route = route{page: pageDaily}
				a.daily = pl.Tracks
			}
			tt.Frame()
			if _, ok := tt.Find("定位正在播放的歌曲"); ok {
				t.Fatalf("offered locate for %s", scenario)
			}
		})
	}
}

// With NM_FRAMES set, inspect the circular floating control in both themes
// before and after it reveals the song in a long playlist.
func TestPlaylistLocateFrames(t *testing.T) {
	dir := os.Getenv("NM_FRAMES")
	if dir == "" {
		t.Skip()
	}
	for _, dark := range []bool{false, true} {
		_, _, tt := locateFixture(t, 39, false)
		tt.SetDark(dark)
		tt.Frame()
		theme := "light"
		if dark {
			theme = "dark"
		}
		for _, stage := range []string{"before", "after"} {
			if stage == "after" {
				clickLocate(t, tt)
			}
			tt.Move(100, 100)
			tt.Advance(time.Second)
			tt.Frame()
			f, err := os.Create(filepath.Join(dir, "playlist-locate-"+theme+"-"+stage+".png"))
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
}
