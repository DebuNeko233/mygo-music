/*
将播放器命令和派生状态连接到 Dock 菜单，菜单资源由 App 持有并在退出时释放。
*/

package app

import (
	"github.com/egoist/mygo"

	"netease-music-mygo/internal/features/player"
)

func (a *app) newDockMenu() *mygo.Menu {
	run := func(fn func()) func(*mygo.MenuItem, *mygo.Window) {
		return func(*mygo.MenuItem, *mygo.Window) {
			if a.quitting {
				return
			}
			fn()
			a.redraw()
		}
	}
	repeat := []*mygo.MenuItem{}
	for _, option := range []struct {
		id, label string
		mode      player.RepeatMode
	}{
		{"repeat-off", "关闭", player.RepeatOff},
		{"repeat-all", "列表循环", player.RepeatAll},
		{"repeat-one", "单曲循环", player.RepeatOne},
	} {
		repeat = append(repeat, &mygo.MenuItem{ID: option.id, Label: option.label, Type: mygo.MenuItemRadio,
			Click: run(func() { a.pl.SetRepeat(option.mode) })})
	}
	shuffle := []*mygo.MenuItem{}
	for _, option := range []struct {
		id, label string
		enabled   bool
	}{
		{"shuffle-off", "关闭", false},
		{"shuffle-on", "开启", true},
	} {
		shuffle = append(shuffle, &mygo.MenuItem{ID: option.id, Label: option.label, Type: mygo.MenuItemRadio,
			Click: run(func() { a.pl.SetShuffle(option.enabled) })})
	}
	return mygo.NewMenu([]*mygo.MenuItem{
		{Label: "重复播放", Submenu: repeat},
		{Label: "随机播放", Submenu: shuffle},
		mygo.Separator(),
		{ID: "play", Label: "播放", Click: run(a.pl.TogglePlay)},
		{ID: "next", Label: "下一首", Click: run(a.pl.Next)},
		{ID: "previous", Label: "上一首", Click: run(a.pl.Prev)},
	})
}

// Only send changed properties to AppKit; progress notifications must not
// rebuild menus or repeatedly update native items while the menu is open.
func (a *app) syncDockMenu() {
	if a.dockMenu == nil || a.quitting {
		return
	}
	play := a.dockMenu.ItemByID("play")
	label := "播放"
	if a.pl.Playing() {
		label = "暂停"
	}
	if play.Label != label {
		play.SetLabel(label)
	}
	// Manual next/previous wrap ordinary queues, including a single song.
	// Next at the end of a radio asks its feed; a temporary song may finish.
	enabled := len(a.pl.Queue()) > 0
	for _, id := range []string{"play", "next", "previous"} {
		item := a.dockMenu.ItemByID(id)
		if item.IsEnabled() != enabled {
			item.SetEnabled(enabled)
		}
	}
	for _, state := range []struct {
		id      string
		checked bool
	}{
		{"repeat-off", a.pl.Repeat() == player.RepeatOff},
		{"repeat-all", a.pl.Repeat() == player.RepeatAll},
		{"repeat-one", a.pl.Repeat() == player.RepeatOne},
		{"shuffle-off", !a.pl.Shuffle()},
		{"shuffle-on", a.pl.Shuffle()},
	} {
		item := a.dockMenu.ItemByID(state.id)
		if item.IsChecked() != state.checked {
			item.SetChecked(state.checked)
		}
	}
}
