package app

import (
	"image/png"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/egoist/mygo/ui"
)

func showSettingsPage(t *testing.T, tt *ui.Tester, title string) {
	t.Helper()
	tt.Frame()
	item, ok := tt.AccessByRole(ui.RoleButton, title)
	if !ok {
		t.Fatalf("no settings navigation for %q", title)
	}
	r := item.Bounds
	tt.ClickAt(r.X+r.W/2, r.Y+r.H/2)
	tt.Frame()
}

func clickSettingsSwitch(t *testing.T, tt *ui.Tester, label string) {
	t.Helper()
	item, ok := tt.AccessByRole(ui.RoleSwitch, label)
	if !ok {
		t.Fatalf("no switch %q", label)
	}
	r := item.Bounds
	tt.ClickAt(r.X+r.W/2, r.Y+r.H/2)
	tt.Frame()
}

// A setting changed through a page or a cross-page search is applied and
// saved, and leaving search keeps the setting in its own category.
func TestSettingsDesignNavigationAndSearch(t *testing.T) {
	isolateUserData(t)
	a := newTestApp()
	a.settings = defaultSettings()
	a.settings.path = filepath.Join(t.TempDir(), "settings.json")
	oldAccent := accentChosen
	defer func() { accentChosen = oldAccent }()
	tt := ui.NewTester(a.settingsView, 780, 640)
	showSettingsPage(t, tt, "播放")
	before := a.settings.HotComments
	clickSettingsSwitch(t, tt, "间奏时显示热评")
	if a.settings.HotComments == before || readCacheSettings(t, a.settings.path).HotComments == before {
		t.Fatal("page change did not apply and save the preference")
	}
	tt.Key(ui.Cmd, ui.KeyF)
	tt.Type("描边")
	tt.Frame()
	if a.settingsQuery != "描边" {
		t.Fatal("find shortcut did not focus settings search")
	}
	slider, ok := tt.AccessByRole(ui.RoleSlider, "调整描边厚度")
	if !ok {
		t.Fatal("cross-page search did not find the desktop outline control")
	}
	r := slider.Bounds
	tt.ClickAt(r.X+r.W*0.6, r.Y+r.H/2)
	tt.Key(0, ui.KeyRight)
	tt.Frame()
	width := a.settings.LyricOutlineWidth
	if width <= 0 || readCacheSettings(t, a.settings.path).LyricOutlineWidth != width {
		t.Fatal("search result did not apply and save the desktop outline width")
	}
	showSettingsPage(t, tt, "桌面歌词")
	if a.settingsQuery != "" || a.settingsPage != "lyrics" || a.settings.LyricOutlineWidth != width {
		t.Fatal("leaving search changed the setting or failed to open its page")
	}
}

// Searching wide background rows must still allow clearing the selected
// pictures; discovery itself must never construct or execute their controls.
func TestSettingsDesignSearchBackgroundRows(t *testing.T) {
	isolateUserData(t)
	a := newTestApp()
	a.settings = defaultSettings()
	a.settings.BackdropHome = []string{"fixture.png"}
	a.settings.path = filepath.Join(t.TempDir(), "settings.json")
	tt := ui.NewTester(a.settingsView, 780, 640)
	tt.Frame()
	tt.Key(ui.Cmd, ui.KeyF)
	tt.Type("主页面背景")
	tt.Frame()
	if err := tt.Click("清空"); err != nil {
		t.Fatal(err)
	}
	if len(a.settings.BackdropHome) != 0 || len(readCacheSettings(t, a.settings.path).BackdropHome) != 0 {
		t.Fatal("background search result did not clear and save the pictures")
	}
}

// Shrinking the window keeps the navigation and search usable, and leaving
// a long scrolled page resets the next page to its first interactive control.
func TestSettingsDesignResizeAndScroll(t *testing.T) {
	isolateUserData(t)
	a := newTestApp()
	a.settings = defaultSettings()
	tt := ui.NewTester(a.settingsView, 780, 640)
	tt.SetTitleBar(ui.TitleBar{Height: 40, Left: 78})
	showSettingsPage(t, tt, "桌面歌词")
	tt.Scroll(550, 400, 0, 10000)
	tt.Advance(time.Second)
	tt.Frame()
	tt.Frame()
	if !a.settingsUnderTop {
		t.Fatal("long settings page did not scroll")
	}
	tt.SetSize(640, 420)
	showSettingsPage(t, tt, "播放")
	tt.Frame()
	if a.settingsUnderTop {
		t.Fatal("new page inherited the previous page's scroll")
	}
	before := a.settings.Normalize
	clickSettingsSwitch(t, tt, "音量均衡")
	if a.settings.Normalize == before {
		t.Fatal("playback control unusable at minimum size")
	}
	field, ok := tt.Find("搜索设置")
	if !ok {
		t.Fatal("search disappeared after resize")
	}
	// The icon overlays the input; clicking it must focus the same field.
	tt.ClickAt(field.X+10, field.Y+field.H/2)
	tt.Type("烟雾")
	tt.Frame()
	beforeFog := a.settings.Fog
	clickSettingsSwitch(t, tt, "烟雾背景")
	if a.settings.Fog == beforeFog {
		t.Fatal("search control unusable after resize")
	}
}

// With NM_FRAMES set, inspect the sidebar, search and fork-specific rows
// in both appearances, at the normal and minimum supported window sizes.
func TestSettingsDesignFrames(t *testing.T) {
	dir := os.Getenv("NM_FRAMES")
	if dir == "" {
		t.Skip()
	}
	isolateUserData(t)
	for _, size := range []struct {
		name string
		w, h int
	}{{"normal", 780, 640}, {"minimum", 640, 420}} {
		for _, dark := range []bool{false, true} {
			a := newTestApp()
			a.settings = defaultSettings()
			tt := ui.NewTester(a.settingsView, size.w, size.h)
			tt.SetDark(dark)
			tt.SetTitleBar(ui.TitleBar{Height: 40, Left: 78})
			appearance := "light"
			if dark {
				appearance = "dark"
			}
			for _, page := range []string{"通用", "外观", "桌面歌词", "缓存", "search"} {
				if page == "search" {
					tt.Key(ui.Cmd, ui.KeyF)
					tt.Type("描边")
					tt.Frame()
				} else {
					showSettingsPage(t, tt, page)
				}
				f, err := os.Create(filepath.Join(dir, "settings-"+size.name+"-"+appearance+"-"+page+".png"))
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
}
