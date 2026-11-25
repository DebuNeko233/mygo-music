/*
呈现七个可选侧边栏页面的显示开关，将修改交给现有配置保存与界面刷新流程。
*/

package settings

import "github.com/egoist/mygo/ui"

func (a *View) sidebarSettings(c *ui.Context, row Row) {
	hidden := &a.Config.HiddenSidebarPages
	for _, item := range []struct {
		key, label string
		hidden     *bool
	}{
		{"daily", "每日推荐", &hidden.Daily},
		{"recent", "最近播放", &hidden.Recent},
		{"fm", "私人 FM", &hidden.FM},
		{"charts", "排行榜", &hidden.Charts},
		{"podcasts", "播客", &hidden.Podcasts},
		{"local", "本地歌曲", &hidden.Local},
		{"cloud", "音乐网盘", &hidden.Cloud},
	} {
		row(item.label, "在侧边栏显示此入口", func() {
			visible := !*item.hidden
			// Commit the inverse binding after input; rebuilding a local bool
			// before Changed is observed would otherwise discard the toggle.
			ui.Switch(c.Key("sidebar-"+item.key), &visible).Label("显示" + item.label).OnClick(func() {
				*item.hidden = !visible
				a.Changed()
			})
		})
	}
}
