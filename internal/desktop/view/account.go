/*
呈现扫码登录、钥匙串帮助、用户信息和账号菜单，通过注入命令执行操作。
*/

package view

import (
	"runtime"

	"github.com/egoist/mygo/ui"
	"rsc.io/qr"

	"netease-music-mygo/internal/desktop/view/components"
	"netease-music-mygo/internal/desktop/view/page"
	"netease-music-mygo/internal/navigation"
	"netease-music-mygo/internal/netease"
	accountvm "netease-music-mygo/internal/viewmodel/account"
)

type Account struct {
	*page.Page
	Model                        *accountvm.Account
	MenuOpen                     bool
	OpenLogin, Settings, SignOut func()
	Open                         func(navigation.Route)
	Avatar                       func(*ui.Context, string, float32, ui.Color)
	AvatarImage                  func(string) *ui.Bitmap
	CopyText                     func(string) error
	Notify                       func(string)
}

// loginDialog shows the QR code to scan with the NetEase app on a phone.
func (a *Account) Login(c *ui.Context) {
	p := *a.Palette
	l := &a.Model.Login
	ui.DialogBase(c.Key("login"), &l.Open, func(backdrop, panel ui.Element) {
		backdrop.Background(ui.RGBA(0, 0, 0, 0.35))
		panel.Width(340).Padding(28, 28, 26, 28).Radius(22).Gap(6).AlignItems(ui.Center).
			Background(p.Background).Border(1, p.Hairline).Shadow(0, 24, 60, -12, ui.RGBA(0, 0, 0, 0.4)).
			Transition(ui.ElementTransition{Animation: &ui.Snappy, Enter: &ui.Motion{Scale: 0.94}, Exit: &ui.Motion{Scale: 0.96}})
		title, sub := "扫码登录", "打开网易云音乐 App，扫一扫登录"
		switch {
		case l.Finished:
			title, sub = "登录成功", "欢迎回来，"+a.ProfileName()
		case l.Err != "":
			sub = l.Err
		case l.Status == netease.QRScanned:
			title, sub = "扫描成功", "请在手机上确认登录"
		case l.Status == netease.QRExpired:
			sub = "二维码已失效"
		}
		ui.Text(c, title).FontSize(20).FontWeight(700)
		ui.Text(c, sub).FontSize(13).TextColor(p.Muted).TextAlign(ui.Center).Margin(0, 0, 14, 0)

		ui.Box(c).Size(212, 212).Radius(16).Background(ui.RGB(255, 255, 255)).Border(1, p.Hairline).Children(func() {
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
				s := components.NewShimmer(c, ui.RGBA(0, 0, 0, .08), ui.RGBA(0, 0, 0, .03))
				components.QRSkeleton(c, s, 14).Absolute().Top(0).Left(0).Right(0).Bottom(0)
			}
			ui.Column(c).Absolute().Top(0).Left(0).Right(0).Bottom(0).Center().Gap(10).Children(func() {
				switch {
				case l.Finished || l.Status == netease.QRScanned:
					if bmp := a.AvatarImage(netease.Sized(l.Avatar, 160)); bmp != nil {
						pic := ui.Box(c).Size(72, 72).Radius(36).Shadow(0, 6, 16, -4, ui.RGBA(0, 0, 0, 0.3))
						in := components.ArtIn(pic, bmp)
						pic.Children(func() { components.ArtImage(c, bmp, 36, in) })
					}
					if l.Scanner != "" {
						ui.Text(c, l.Scanner).FontSize(14).FontWeight(600).TextColor(ui.Hex("#1d1d1f"))
					}
				case l.Status == netease.QRExpired || l.Err != "":
					if components.PillButton(c.Key("refresh"), components.IconRepeat, "刷新二维码", p.Accent, p.OnAccent).Clicked() {
						a.OpenLogin()
					}
				}
			})
		})
		ui.Text(c, "登录后，每日推荐与歌单将根据你的口味生成").FontSize(11.5).TextColor(p.Faint).Margin(16, 0, 0, 0)
		if l.Keyring {
			ui.Column(c).Width(284).Padding(14, 14, 12, 14).Gap(8).Margin(12, 0, 0, 0).
				Radius(12).Background(p.Hover).Border(1, p.Hairline).Children(func() {
				ui.Text(c, "需要启用系统钥匙串").FontSize(13).FontWeight(600).TextColor(p.Warn)
				ui.Text(c, "系统没有可用的凭据存储服务，应用无法保存登录状态。安装并启用 GNOME Keyring、KWallet 或 KeePassXC 后，重新登录即可长期保持登录。").
					FontSize(12).TextColor(p.Text)
				ui.Text(c, "Linux 桌面通常自带其中一种；复制帮助可查看安装、启用和验证步骤。").FontSize(11.5).TextColor(p.Muted)
				ui.Row(c).Gap(8).Children(func() {
					if components.PillButton(c.Key("copy-keyring-help"), components.IconCopy, "复制设置帮助", p.Accent, p.OnAccent).Clicked() {
						if a.CopyText(netease.KeyringHelp) == nil {
							a.Notify("钥匙串设置帮助已复制")
						} else {
							a.Notify("复制失败，请重试")
						}
					}
					if components.PillButton(c.Key("retry-login"), components.IconRepeat, "重试登录", p.Press, p.Text).Clicked() {
						a.OpenLogin()
					}
				})
			})
		}
	})
	if !l.Open {
		a.Model.CloseLogin()
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

func (a *Account) ProfileName() string {
	if a.Model.Profile == nil {
		return ""
	}
	return a.Model.Profile.Nickname
}

// accountRow sits at the foot of the sidebar: who is signed in, with a
// dropdown to open their page or the settings or sign out, or a way to
// sign in.
func (a *Account) Row(c *ui.Context) {
	p := *a.Palette
	b := ui.ButtonBase(c.Key("account")).Height(40).PaddingX(8).Gap(10).Radius(10).AlignItems(ui.Center).Justify(ui.Start).FocusRing(false)
	if b.Hovered() || a.MenuOpen && a.Model.Profile != nil {
		b.Background(p.Hover)
	}
	if a.Model.Profile == nil {
		a.MenuOpen = false
		b.Label("登录").Children(func() {
			ui.Box(c).Size(26, 26).Radius(13).Background(p.Press).Center().Children(func() {
				ui.Icon(c, components.IconUser).Size(14, 14).TextColor(p.Muted)
			})
			ui.Text(c, "登录网易云音乐").FontSize(13).FontWeight(500).TextColor(p.Muted)
		})
		if b.Clicked() {
			a.OpenLogin()
		}
		return
	}
	prof := a.Model.Profile
	b.Label(prof.Nickname).Expanded(a.MenuOpen).Children(func() {
		a.Avatar(c, prof.AvatarURL, 26, p.Press)
		ui.Text(c, prof.Nickname).FontSize(13).FontWeight(600).SingleLine().Grow(1).Shrink(1).MinWidth(0)
		ui.Icon(c, components.IconUpDown).Size(13, 13).TextColor(p.Faint)
	})
	if b.Clicked() {
		a.MenuOpen = !a.MenuOpen
	}
	a.Menu(c, b)
}

// accountMenu drops up from the account row, as wide as it, in the colors
// of the volume popover: the listener's own page and the settings, then
// signing out.
func (a *Account) Menu(c *ui.Context, row ui.Element) {
	pop := components.PopoverColors(a.Palette.Dark)
	ui.PopoverBase(c.Key("account-menu"), row, &a.MenuOpen, func(panel ui.Element) {
		panel.AttachTo(row, ui.AnchorTopStart, ui.AnchorBottomStart).Margin(0, 0, 6, 0).Padding(5).Gap(1).
			Radius(12).Background(pop.Fill).Border(0.5, pop.Edge).Shadow(0, 8, 24, -4, ui.RGBA(0, 0, 0, 0.2)).
			Role(ui.RoleMenu).FocusGroup(ui.Vertical).Label("账户").TransformOrigin(0.5, 1).
			Transition(ui.ElementTransition{Animation: &ui.Snappy, Enter: &ui.Motion{Scale: 0.96, Y: 4}, Exit: &ui.Motion{Scale: 0.98}})
		if w := row.Bounds().W; w > 0 {
			panel.Width(w)
		}
		shortcut := "Ctrl+,"
		if runtime.GOOS == "darwin" {
			shortcut = "⌘,"
		}
		if a.menuItem(c, components.IconUser, "我的主页", "").AutoFocus().Clicked() {
			a.MenuOpen = false
			a.Open(navigation.Route{Page: navigation.User, ID: a.Model.Profile.UserID})
		}
		if a.menuItem(c, components.IconSettings, "设置…", shortcut).Clicked() {
			a.MenuOpen = false
			a.Settings()
		}
		ui.Box(c).Height(0.5).Margin(4, 8).Background(pop.Edge)
		if a.menuItem(c, components.IconSignOut, "退出登录", "").Clicked() {
			a.MenuOpen = false
			a.SignOut()
		}
	})
}

// menuItem is an item of a dropdown: an icon, its label, and the shortcut
// that does the same, if any.
func (a *Account) menuItem(c *ui.Context, icon *ui.SVG, label, shortcut string) ui.Element {
	p := *a.Palette
	b := ui.ButtonBase(c.Key(label)).Role(ui.RoleMenuItem).Label(label).Height(30).PaddingX(8).Gap(9).Radius(7).
		AlignItems(ui.Center).Justify(ui.Start).FocusRing(false)
	if b.Hovered() || b.FocusVisible() {
		b.Background(p.Press)
	}
	b.Children(func() {
		ui.Icon(c, icon).Size(15, 15).TextColor(p.Muted)
		ui.Text(c, label).FontSize(13).Grow(1)
		if shortcut != "" {
			ui.Text(c, shortcut).FontSize(12).TextColor(p.Faint)
		}
	})
	return b
}
