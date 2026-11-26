/*
呈现侧栏歌单与收藏专辑分区，维护分区展开和收起交互。
*/

package library

import (
	"fmt"

	"github.com/egoist/mygo/ui"

	"netease-music-mygo/internal/features/navigation"
	"netease-music-mygo/internal/features/netease"
	"netease-music-mygo/internal/ui/view/components"
)

// sidebarSection draws a heading in the sidebar that opens and closes what it
// lists, with extra beside the arrow if not nil, and reports whether what it
// lists shows.
func (a *View) sidebarSection(c *ui.Context, title string, collapsed *bool, extra func()) bool {
	p := *a.Palette
	ui.Row(c.Key(title)).AlignItems(ui.Center).Margin(18, 4, 6, 6).Children(func() {
		b := ui.ButtonBase(c.Key("toggle")).Grow(1).Height(26).PaddingX(4).Gap(6).Radius(6).
			AlignItems(ui.Center).Justify(ui.Start).Role(ui.RoleDisclosure).Label(title)
		if b.Clicked() {
			*collapsed = !*collapsed
		}
		b.Expanded(!*collapsed)
		if b.Hovered() {
			b.Background(p.Hover)
		}
		b.Children(func() {
			ui.Text(c, title).FontSize(11.5).FontWeight(600).TextColor(p.Faint).Grow(1)
			arrow := components.IconDown
			if *collapsed {
				arrow = components.IconForward
			}
			ui.Icon(c, arrow).Size(12, 12).TextColor(p.Faint)
		})
		if extra != nil {
			extra()
		}
	})
	return !*collapsed
}

// myPlaylists lists the listener's playlists under a header that opens
// and closes them: those they made, if mine, with a button to make one
// and a menu to rename them, or else with a menu to unsave them.
func (a *View) Playlists(c *ui.Context, title string, lists []netease.Playlist, mine bool, collapsed *bool) {
	if len(lists) == 0 {
		return
	}
	p := *a.Palette
	var new func()
	if mine {
		new = func() {
			if components.IconButton(c.Key("new"), components.IconPlus, 13, p.Faint, "新建歌单").Size(26, 26).Radius(6).Clicked() {
				a.NewPlaylist(nil)
			}
		}
	}
	if !a.sidebarSection(c, title, collapsed, new) {
		return
	}
	for i := range lists {
		pl := &lists[i]
		selected := a.Nav.Current.Page == navigation.Playlist && a.Nav.Current.ID == pl.ID && !a.Playback.Open
		b := ui.ButtonBase(c.Key(pl.ID)).Height(34).PaddingX(8).Gap(10).Radius(8).AlignItems(ui.Center).Justify(ui.Start).FocusRing(false).Label(pl.Name)
		switch {
		case selected:
			b.Background(p.Press)
		case b.Hovered():
			b.Background(p.Hover)
		}
		b.Children(func() {
			a.Cover(c, pl.Cover(), 80, 22, 5)
			ui.Text(c, pl.Name).FontSize(13).SingleLine().Shrink(1).MinWidth(0)
		})
		switch {
		case mine && pl.SpecialType != 5:
			b.ContextMenu(func(m *ui.Menu) {
				if m.Item("重命名…").Chosen() {
					a.Rename(pl)
				}
				m.Separator()
				if m.Item("删除歌单…").Chosen() {
					a.ConfirmDelete(pl)
				}
			})
		case !mine:
			b.ContextMenu(func(m *ui.Menu) {
				if m.Item("取消收藏").Chosen() {
					a.ToggleSaved(*pl)
				}
			})
		}
		if b.Clicked() {
			a.Open(navigation.Route{Page: navigation.Playlist, ID: pl.ID})
		}
	}
}

// myAlbums lists the albums the listener saved under a header that opens and
// closes them, each opening its page, with a menu to unsave it.
func (a *View) Albums(c *ui.Context, albums []netease.AlbumProfile, collapsed *bool) {
	if len(albums) == 0 {
		return
	}
	p := *a.Palette
	if !a.sidebarSection(c, "收藏的专辑", collapsed, nil) {
		return
	}
	for i := range albums {
		al := &albums[i]
		selected := a.Nav.Current.Page == navigation.Album && a.Nav.Current.ID == al.ID && !a.Playback.Open
		b := ui.ButtonBase(c.Key(fmt.Sprintf("album/%d", al.ID))).Height(34).PaddingX(8).Gap(10).Radius(8).
			AlignItems(ui.Center).Justify(ui.Start).FocusRing(false).Label(al.Name)
		switch {
		case selected:
			b.Background(p.Press)
		case b.Hovered():
			b.Background(p.Hover)
		}
		b.Children(func() {
			a.Cover(c, al.PicURL, 80, 22, 5)
			ui.Text(c, al.Name).FontSize(13).SingleLine().Shrink(1).MinWidth(0)
		})
		b.ContextMenu(func(m *ui.Menu) {
			if m.Item("取消收藏").Chosen() {
				a.ToggleAlbumSaved(*al)
			}
		})
		if b.Clicked() {
			a.Open(navigation.Route{Page: navigation.Album, ID: al.ID})
		}
	}
}
