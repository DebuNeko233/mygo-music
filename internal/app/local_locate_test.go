/*
验证本地歌曲定位使用本地身份且不改变当前播放。
*/

package app

import (
	"fmt"
	"image/png"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/egoist/mygo/ui"

	"netease-music-mygo/internal/features/localcatalog"
	"netease-music-mygo/internal/features/netease"
	"netease-music-mygo/internal/features/player"
)

func localLocateFixture(t *testing.T, playing bool) (*app, []localcatalog.Song, *ui.Tester) {
	t.Helper()
	isolateUserData(t)
	a := newTestApp()
	a.settings = defaultSettings()
	songs := make([]localcatalog.Song, 250)
	for i := range songs {
		songs[i] = localcatalog.Song{Track: netease.Track{LocalID: int64(i + 1), ID: int64(i + 1), Name: fmt.Sprintf("本地歌曲 %03d", i+1), Duration: 180000}, Path: "../features/player/testdata/seek.flac"}
	}
	a.local.Replace(localcatalog.Catalog{Songs: songs})
	a.nav.Current = route{Page: pageLocal}
	a.pl.SuspendCache()
	player.Cue(a.pl, []netease.Track{songs[149].Track, {ID: 999, Name: "另一首在线歌曲"}})
	if playing {
		player.Hold(a.pl, 36*time.Second)
	}
	tt := ui.NewTester(a.view, 1200, 800)
	tt.Frame()
	return a, songs, tt
}

// The local library shares the floating locator and preserves pause, progress, queue and files.
func TestLocalLocateKeepsPlayback(t *testing.T) {
	for _, playing := range []bool{false, true} {
		t.Run(fmt.Sprint(playing), func(t *testing.T) {
			a, songs, tt := localLocateFixture(t, playing)
			pos := a.pl.Position()
			if dir := os.Getenv("NM_FRAMES"); dir != "" && !playing {
				f, err := os.Create(filepath.Join(dir, "local-locate.png"))
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
			locatedRow(t, tt, songs[149].Track.Name, 800)
			if a.pl.Current().LocalID != 150 || a.pl.Playing() != playing || a.pl.Position() != pos || len(a.pl.Queue()) != 2 || a.pl.Queue()[1].ID != 999 || a.nav.Current.Page != pageLocal || len(a.local.Tracks) != 250 {
				t.Fatal("local locate changed playback, queue or library")
			}
			tt.Scroll(750, 400, 0, -100000)
			tt.Frame()
			a.local.Status = strings.Repeat("正在扫描本地音乐文件夹 ", 8)
			tt.SetSize(1000, 640)
			tt.Frame()
			for i := 0; i < 200 && !tt.Focused("定位正在播放的歌曲"); i++ {
				tt.Key(0, ui.KeyTab)
			}
			if !tt.Focused("定位正在播放的歌曲") {
				t.Fatal("local locator cannot receive keyboard focus")
			}
			tt.Key(0, ui.KeyEnter)
			tt.Frame()
			locatedRow(t, tt, songs[149].Track.Name, 640)
			if a.pl.Current().LocalID != 150 || a.pl.Playing() != playing || a.pl.Position() != pos {
				t.Fatal("keyboard locate changed playback")
			}
		})
	}
}

// A remote song with the same numeric ID must not be located as a local file, nor may an absent file expose a target.
func TestLocalLocateUsesLocalIdentity(t *testing.T) {
	a, _, tt := localLocateFixture(t, false)
	for _, tr := range []netease.Track{{ID: 150, Name: "在线歌曲"}, {LocalID: 999, Name: "未收入曲库的本地文件"}} {
		player.Cue(a.pl, []netease.Track{tr})
		tt.Frame()
		if _, shown := tt.Find("定位正在播放的歌曲"); shown {
			t.Fatal("locator offered an unrelated local row")
		}
	}
}
