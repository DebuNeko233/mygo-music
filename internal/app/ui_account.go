package app

import (
	"fmt"
	"runtime"

	"github.com/egoist/mygo/ui"
	"rsc.io/qr"

	"netease-music-mygo/internal/netease"
)

// loginDialog shows the QR code to scan with the NetEase app on a phone.
func (a *app) loginDialog(c *ui.Context) {
	p := a.pal
	l := &a.account.Login
	ui.DialogBase(c.Key("login"), &l.Open, func(backdrop, panel ui.Element) {
		backdrop.Background(ui.RGBA(0, 0, 0, 0.35))
		panel.Width(340).Padding(28, 28, 26, 28).Radius(22).Gap(6).AlignItems(ui.Center).
			Background(p.bg).Border(1, p.hairline).Shadow(0, 24, 60, -12, ui.RGBA(0, 0, 0, 0.4)).
			Transition(ui.ElementTransition{Animation: &ui.Snappy, Enter: &ui.Motion{Scale: 0.94}, Exit: &ui.Motion{Scale: 0.96}})
		title, sub := "扫码登录", "打开网易云音乐 App，扫一扫登录"
		switch {
		case l.Finished:
			title, sub = "登录成功", "欢迎回来，"+a.profileName()
		case l.Err != "":
			sub = l.Err
		case l.Status == netease.QRScanned:
			title, sub = "扫描成功", "请在手机上确认登录"
		case l.Status == netease.QRExpired:
			sub = "二维码已失效"
		}
		ui.Text(c, title).FontSize(20).FontWeight(700)
		ui.Text(c, sub).FontSize(13).TextColor(p.muted).TextAlign(ui.Center).Margin(0, 0, 14, 0)

		ui.Box(c).Size(212, 212).Radius(16).Background(ui.RGB(255, 255, 255)).Border(1, p.hairline).Children(func() {
			if l.Code != nil {
				code := l.Code
				opacity := float32(1)
				if l.Status == netease.QRScanned || l.Status == netease.QRExpired || l.Finished || l.Err != "" {
					opacity = 0.12
				}
				ui.Box(c).Absolute().Top(0).Left(0).Right(0).Bottom(0).Opacity(opacity).
					Draw(func(pt *ui.Painter, r ui.Rect) { drawQR(pt, r, code, 14) })
			} else if l.Err == "" {
				// On white whatever the appearance, as the code is.
				s := shimmer{base: ui.RGBA(0, 0, 0, 0.08), sheen: ui.RGBA(0, 0, 0, 0.03)}.in(c)
				qrSkeleton(c, s, 14).Absolute().Top(0).Left(0).Right(0).Bottom(0)
			}
			ui.Column(c).Absolute().Top(0).Left(0).Right(0).Bottom(0).Center().Gap(10).Children(func() {
				switch {
				case l.Finished || l.Status == netease.QRScanned:
					if bmp := bitmap(a.art.get(netease.Sized(l.Avatar, 160), false)); bmp != nil {
						pic := ui.Box(c).Size(72, 72).Radius(36).Shadow(0, 6, 16, -4, ui.RGBA(0, 0, 0, 0.3))
						in := artIn(pic, bmp)
						pic.Children(func() { artImage(c, bmp, 36, in) })
					}
					if l.Scanner != "" {
						ui.Text(c, l.Scanner).FontSize(14).FontWeight(600).TextColor(ui.Hex("#1d1d1f"))
					}
				case l.Status == netease.QRExpired || l.Err != "":
					if pillButton(c.Key("refresh"), iconRepeat, "刷新二维码", p.accent, p.onAccent).Clicked() {
						a.openLogin()
					}
				}
			})
		})
		ui.Text(c, "登录后，每日推荐与歌单将根据你的口味生成").FontSize(11.5).TextColor(p.faint).Margin(16, 0, 0, 0)
	})
	if !l.Open {
		a.account.CloseLogin()
	}
}

// drawQR draws a QR code in r, as crisp squares on white, inside a quiet
// zone of pad.
func drawQR(p *ui.Painter, r ui.Rect, code *qr.Code, pad float32) {
	n := code.Size
	cell := (min(r.W, r.H) - 2*pad) / float32(n)
	x0 := r.X + (r.W-cell*float32(n))/2
	y0 := r.Y + (r.H-cell*float32(n))/2
	ink := ui.Hex("#141416")
	for y := range n {
		for x := range n {
			if code.Black(x, y) {
				// A hair larger, so neighbors join without seams.
				p.Fill(ui.Rect{X: x0 + float32(x)*cell, Y: y0 + float32(y)*cell, W: cell + 0.3, H: cell + 0.3}, ink, 0)
			}
		}
	}
}

func (a *app) profileName() string {
	if a.account.Profile == nil {
		return ""
	}
	return a.account.Profile.Nickname
}

// accountRow sits at the foot of the sidebar: who is signed in, with a
// dropdown to open their page or the settings or sign out, or a way to
// sign in.
func (a *app) accountRow(c *ui.Context) {
	p := a.pal
	b := ui.ButtonBase(c.Key("account")).Height(40).PaddingX(8).Gap(10).Radius(10).AlignItems(ui.Center).Justify(ui.Start).FocusRing(false)
	if b.Hovered() || a.accountOpen && a.account.Profile != nil {
		b.Background(p.hover)
	}
	if a.account.Profile == nil {
		a.accountOpen = false
		b.Label("登录").Children(func() {
			ui.Box(c).Size(26, 26).Radius(13).Background(p.press).Center().Children(func() {
				ui.Icon(c, iconUser).Size(14, 14).TextColor(p.muted)
			})
			ui.Text(c, "登录网易云音乐").FontSize(13).FontWeight(500).TextColor(p.muted)
		})
		if b.Clicked() {
			a.openLogin()
		}
		return
	}
	prof := a.account.Profile
	b.Label(prof.Nickname).Expanded(a.accountOpen).Children(func() {
		a.avatar(c, prof.AvatarURL, 26, p.press)
		ui.Text(c, prof.Nickname).FontSize(13).FontWeight(600).SingleLine().Grow(1).Shrink(1).MinWidth(0)
		ui.Icon(c, iconUpDown).Size(13, 13).TextColor(p.faint)
	})
	if b.Clicked() {
		a.accountOpen = !a.accountOpen
	}
	a.accountMenu(c, b)
}

// accountMenu drops up from the account row, as wide as it, in the colors
// of the volume popover: the listener's own page and the settings, then
// signing out.
func (a *app) accountMenu(c *ui.Context, row ui.Element) {
	pop := popoverColors(a.pal.dark)
	ui.PopoverBase(c.Key("account-menu"), row, &a.accountOpen, func(panel ui.Element) {
		panel.AttachTo(row, ui.AnchorTopStart, ui.AnchorBottomStart).Margin(0, 0, 6, 0).Padding(5).Gap(1).
			Radius(12).Background(pop.fill).Border(0.5, pop.edge).Shadow(0, 8, 24, -4, ui.RGBA(0, 0, 0, 0.2)).
			Role(ui.RoleMenu).FocusGroup(ui.Vertical).Label("账户").TransformOrigin(0.5, 1).
			Transition(ui.ElementTransition{Animation: &ui.Snappy, Enter: &ui.Motion{Scale: 0.96, Y: 4}, Exit: &ui.Motion{Scale: 0.98}})
		if w := row.Bounds().W; w > 0 {
			panel.Width(w)
		}
		shortcut := "Ctrl+,"
		if runtime.GOOS == "darwin" {
			shortcut = "⌘,"
		}
		if a.menuItem(c, iconUser, "我的主页", "").AutoFocus().Clicked() {
			a.accountOpen = false
			a.open(route{page: pageUser, id: a.account.Profile.UserID})
		}
		if a.menuItem(c, iconSettings, "设置…", shortcut).Clicked() {
			a.accountOpen = false
			a.openSettings()
		}
		ui.Box(c).Height(0.5).Margin(4, 8).Background(pop.edge)
		if a.menuItem(c, iconSignOut, "退出登录", "").Clicked() {
			a.accountOpen = false
			a.signOut()
		}
	})
}

// menuItem is an item of a dropdown: an icon, its label, and the shortcut
// that does the same, if any.
func (a *app) menuItem(c *ui.Context, icon *ui.SVG, label, shortcut string) ui.Element {
	p := a.pal
	b := ui.ButtonBase(c.Key(label)).Role(ui.RoleMenuItem).Label(label).Height(30).PaddingX(8).Gap(9).Radius(7).
		AlignItems(ui.Center).Justify(ui.Start).FocusRing(false)
	if b.Hovered() || b.FocusVisible() {
		b.Background(p.press)
	}
	b.Children(func() {
		ui.Icon(c, icon).Size(15, 15).TextColor(p.muted)
		ui.Text(c, label).FontSize(13).Grow(1)
		if shortcut != "" {
			ui.Text(c, shortcut).FontSize(12).TextColor(p.faint)
		}
	})
	return b
}

// sidebarSection draws a heading in the sidebar that opens and closes what it
// lists, with extra beside the arrow if not nil, and reports whether what it
// lists shows.
func (a *app) sidebarSection(c *ui.Context, title string, collapsed *bool, extra func()) bool {
	p := a.pal
	ui.Row(c.Key(title)).AlignItems(ui.Center).Margin(18, 4, 6, 6).Children(func() {
		b := ui.ButtonBase(c.Key("toggle")).Grow(1).Height(26).PaddingX(4).Gap(6).Radius(6).
			AlignItems(ui.Center).Justify(ui.Start).Role(ui.RoleDisclosure).Label(title)
		if b.Clicked() {
			*collapsed = !*collapsed
		}
		b.Expanded(!*collapsed)
		if b.Hovered() {
			b.Background(p.hover)
		}
		b.Children(func() {
			ui.Text(c, title).FontSize(11.5).FontWeight(600).TextColor(p.faint).Grow(1)
			arrow := iconDown
			if *collapsed {
				arrow = iconForward
			}
			ui.Icon(c, arrow).Size(12, 12).TextColor(p.faint)
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
func (a *app) myPlaylists(c *ui.Context, title string, lists []netease.Playlist, mine bool, collapsed *bool) {
	if len(lists) == 0 {
		return
	}
	p := a.pal
	var new func()
	if mine {
		new = func() {
			if iconButton(c.Key("new"), iconPlus, 13, p.faint, "新建歌单").Size(26, 26).Radius(6).Clicked() {
				a.newPlaylist(nil)
			}
		}
	}
	if !a.sidebarSection(c, title, collapsed, new) {
		return
	}
	for i := range lists {
		pl := &lists[i]
		selected := a.route.page == pagePlaylist && a.route.id == pl.ID && !a.nowPlaying
		b := ui.ButtonBase(c.Key(pl.ID)).Height(34).PaddingX(8).Gap(10).Radius(8).AlignItems(ui.Center).Justify(ui.Start).FocusRing(false).Label(pl.Name)
		switch {
		case selected:
			b.Background(p.press)
		case b.Hovered():
			b.Background(p.hover)
		}
		b.Children(func() {
			a.cover(c, pl.Cover(), 80, 22, 5)
			ui.Text(c, pl.Name).FontSize(13).SingleLine().Shrink(1).MinWidth(0)
		})
		switch {
		case mine && pl.SpecialType != 5:
			b.ContextMenu(func(m *ui.Menu) {
				if m.Item("重命名…").Chosen() {
					a.startRename(pl)
				}
				m.Separator()
				if m.Item("删除歌单…").Chosen() {
					a.confirmDelete(pl)
				}
			})
		case !mine:
			b.ContextMenu(func(m *ui.Menu) {
				if m.Item("取消收藏").Chosen() {
					a.toggleSaved(*pl)
				}
			})
		}
		if b.Clicked() {
			a.open(route{page: pagePlaylist, id: pl.ID})
		}
	}
}

// myAlbums lists the albums the listener saved under a header that opens and
// closes them, each opening its page, with a menu to unsave it.
func (a *app) myAlbums(c *ui.Context, albums []netease.AlbumProfile, collapsed *bool) {
	if len(albums) == 0 {
		return
	}
	p := a.pal
	if !a.sidebarSection(c, "收藏的专辑", collapsed, nil) {
		return
	}
	for i := range albums {
		al := &albums[i]
		selected := a.route.page == pageAlbum && a.route.id == al.ID && !a.nowPlaying
		b := ui.ButtonBase(c.Key(fmt.Sprintf("album/%d", al.ID))).Height(34).PaddingX(8).Gap(10).Radius(8).
			AlignItems(ui.Center).Justify(ui.Start).FocusRing(false).Label(al.Name)
		switch {
		case selected:
			b.Background(p.press)
		case b.Hovered():
			b.Background(p.hover)
		}
		b.Children(func() {
			a.cover(c, al.PicURL, 80, 22, 5)
			ui.Text(c, al.Name).FontSize(13).SingleLine().Shrink(1).MinWidth(0)
		})
		b.ContextMenu(func(m *ui.Menu) {
			if m.Item("取消收藏").Chosen() {
				a.toggleAlbumSaved(*al)
			}
		})
		if b.Clicked() {
			a.open(route{page: pageAlbum, id: al.ID})
		}
	}
}
