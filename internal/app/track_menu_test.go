/*
验证歌曲菜单按需构建，避免提前创建无效界面内容。
*/

package app

import (
	"testing"

	"github.com/egoist/mygo/ui"

	"netease-music-mygo/internal/features/netease"
)

// Idle rows do not create track menus, and a requested menu still acts on its own song.
func TestTrackMenuBuildsOnRequest(t *testing.T) {
	a := newTestApp()
	tracks := []netease.Track{{ID: 91, Name: "菜单测试歌曲", Duration: 180000}}
	builds := 0
	var chosen int64
	a.views.tracks.SongItems = func(tr *netease.Track, pid int64) func(*ui.Menu) {
		builds++
		return func(m *ui.Menu) {
			if m.Item("执行歌曲操作").Chosen() {
				chosen = tr.ID
			}
		}
	}
	tt := ui.NewTester(func(c *ui.Context) {
		a.pal = paletteFor(c.Theme())
		a.views.tracks.Rows(c, tracks, false, func(c *ui.Context) {})
	}, 1000, 700)
	tt.Frame()
	tt.Frame()
	if builds != 0 {
		t.Fatalf("idle rows created %d menus", builds)
	}
	if err := tt.RightClick(tracks[0].Name); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	if builds == 0 {
		t.Fatal("requested menu was not built")
	}
	if err := tt.ChooseMenuItem("执行歌曲操作"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	if chosen != 91 {
		t.Fatalf("menu acted on song %d", chosen)
	}
}
