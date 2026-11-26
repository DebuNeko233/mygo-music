/*
验证侧边栏页面开关即时生效、保存恢复及搜索操作，同时保留当前页面和播放状态。
*/

package app

import (
	"image/png"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/egoist/mygo/ui"

	"netease-music-mygo/internal/netease"
	"netease-music-mygo/internal/player"
	"netease-music-mygo/internal/preferences"
)

// Hiding each optional entry persists independently without changing the current page or queue; restoring it works after reload.
func TestSidebarSettingsApplyAndReload(t *testing.T) {
	isolateUserData(t)
	a := newTestApp()
	a.settings.Path = filepath.Join(t.TempDir(), "settings.json")
	a.nav.Current = route{Page: pageLocal}
	player.Cue(a.pl, []netease.Track{{ID: 42, Name: "playing", Duration: 120000}})
	player.Hold(a.pl, 12*time.Second)
	before := a.nav.Current
	main := ui.NewTester(a.sidebar, 220, 800)
	settingsWindow := ui.NewTester(a.views.settings.Build, 780, 640)
	showSettingsPage(t, settingsWindow, "侧边栏")
	expected := preferences.SidebarPages{}
	items := []struct {
		label  string
		hidden *bool
	}{
		{"每日推荐", &expected.Daily}, {"最近播放", &expected.Recent},
		{"私人 FM", &expected.FM}, {"排行榜", &expected.Charts},
		{"播客", &expected.Podcasts}, {"本地歌曲", &expected.Local},
		{"音乐网盘", &expected.Cloud},
	}
	for _, item := range items {
		clickSettingsSwitch(t, settingsWindow, "显示"+item.label)
		*item.hidden = true
		main.Frame()
		for _, other := range items {
			_, visible := main.Find(other.label)
			if visible == *other.hidden {
				t.Fatalf("changing %s affected %s incorrectly: visible=%v", item.label, other.label, visible)
			}
		}
		if saved := readCacheSettings(t, a.settings.Path); saved.HiddenSidebarPages != expected {
			t.Fatalf("sidebar choices not saved independently: %+v, want %+v", saved.HiddenSidebarPages, expected)
		}
		if a.nav.Current != before || a.pl.Current().ID != 42 || len(a.pl.Queue()) != 1 || !a.pl.Playing() || a.pl.Position() != 12*time.Second {
			t.Fatal("hiding a sidebar entry changed the current page or playback")
		}
	}
	if _, ok := main.Find("为你推荐"); !ok {
		t.Fatal("hiding optional entries removed the permanent home entry")
	}
	reloaded, ok := preferences.Load(a.settings.Path)
	if !ok {
		t.Fatal("could not reload sidebar settings")
	}
	a.settings = settings(reloaded)
	main.Frame()
	for _, item := range items {
		if _, ok := main.Find(item.label); ok {
			t.Fatalf("hidden entry %s returned after reload", item.label)
		}
		clickSettingsSwitch(t, settingsWindow, "显示"+item.label)
		main.Frame()
		if _, ok := main.Find(item.label); !ok {
			t.Fatalf("entry %s did not return after enabling", item.label)
		}
	}
	if saved := readCacheSettings(t, a.settings.Path); saved.HiddenSidebarPages != (preferences.SidebarPages{}) {
		t.Fatalf("restored entries were not saved: %+v", saved.HiddenSidebarPages)
	}
}

// A sidebar switch found through settings search stays usable at the minimum window size and changes only its own entry.
func TestSidebarSettingsSearchAndResize(t *testing.T) {
	isolateUserData(t)
	a := newTestApp()
	tt := ui.NewTester(a.views.settings.Build, 640, 420)
	tt.Key(ui.Cmd, ui.KeyF)
	tt.Type("音乐网盘")
	tt.Frame()
	clickSettingsSwitch(t, tt, "显示音乐网盘")
	if a.settings.HiddenSidebarPages != (preferences.SidebarPages{Cloud: true}) {
		t.Fatal("search toggle did not hide only the cloud entry")
	}
	showSettingsPage(t, tt, "侧边栏")
	tt.Scroll(500, 300, 0, 500)
	tt.Advance(time.Second)
	clickSettingsSwitch(t, tt, "显示音乐网盘")
	if a.settings.HiddenSidebarPages.Cloud {
		t.Fatal("hidden entry could not be restored from the scrolled settings page")
	}
}

// Render the sidebar controls in both appearances and supported sizes to inspect switch layout and scrolling.
func TestSidebarSettingsFrames(t *testing.T) {
	dir := os.Getenv("NM_FRAMES")
	if dir == "" {
		t.Skip("set NM_FRAMES to inspect sidebar settings")
	}
	isolateUserData(t)
	for _, size := range []struct {
		name string
		w, h int
	}{{"normal", 780, 640}, {"minimum", 640, 420}} {
		for _, dark := range []bool{false, true} {
			a := newTestApp()
			a.settings.HiddenSidebarPages = preferences.SidebarPages{FM: true, Cloud: true}
			a.preferences.Open("sidebar")
			tt := ui.NewTester(a.views.settings.Build, size.w, size.h)
			tt.SetDark(dark)
			tt.SetTitleBar(ui.TitleBar{Height: 40, Left: 78})
			tt.Advance(time.Second)
			appearance := "light"
			if dark {
				appearance = "dark"
			}
			f, err := os.Create(filepath.Join(dir, "sidebar-settings-"+size.name+"-"+appearance+".png"))
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
