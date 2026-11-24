package view

import (
	"errors"
	"fmt"

	"github.com/egoist/mygo/ui"

	"netease-music-mygo/internal/desktop/view/page"
	"netease-music-mygo/internal/netease"
	accountvm "netease-music-mygo/internal/viewmodel/account"
	cloudvm "netease-music-mygo/internal/viewmodel/cloud"
)

type Cloud struct {
	*page.Page
	Model         *cloudvm.Cloud
	Account       *accountvm.Account
	Load          func(cloudvm.CloudLoadMode)
	OpenLogin     func()
	Current       func() *netease.Track
	CurrentIndex  func([]netease.Track) int
	SyncViewport  func(*ui.Context, string, string, int, int)
	Locate        func(*ui.Context, int, bool)
	LocatePlaying func(*ui.Context, bool, func())
}

func (a *Cloud) Build(c *ui.Context) {
	if a.Account.Profile == nil {
		top, bottom := a.Insets(c)
		ui.Column(c).Grow(1).Padding(top, 0, bottom, 0).Center().Gap(12).Children(func() {
			ui.Text(c, "登录后查看音乐网盘").FontSize(14).TextColor(a.Palette.Muted)
			ui.PrimaryButton(c.Key("cloud-sign-in"), "登录网易云音乐").OnClick(a.OpenLogin)
		})
		return
	}
	d := a.Model
	if !d.Loading() && d.Failure == nil && (!d.Loaded || d.More) {
		a.Load(cloudvm.CloudNextPage)
	}
	header := func(c *ui.Context) { a.Header(c) }
	if !d.Loaded && d.Loading() {
		a.Skeleton(c, true, header)
		return
	}
	at := a.CurrentIndex(d.Tracks)
	a.SyncViewport(c, "音乐网盘", CloudSummary(d), len(d.Tracks), at)
	var footer []func(*ui.Context)
	if d.Failure != nil || d.Loaded && !d.Loading() && len(d.Tracks) == 0 {
		footer = append(footer, func(c *ui.Context) {
			switch {
			case d.Failure != nil:
				ui.Column(c).Center().Gap(12).Padding(24, 0, 24, 0).Children(func() {
					message := "网盘加载失败，请重试"
					if errors.Is(d.Failure, netease.ErrNotLoggedIn) {
						message = "登录已失效，请重新登录"
					}
					ui.Text(c, message).FontSize(13).TextColor(a.Palette.Muted)
					if errors.Is(d.Failure, netease.ErrNotLoggedIn) {
						ui.Button(c.Key("cloud-login"), "重新登录").OnClick(a.OpenLogin)
					} else {
						ui.Button(c.Key("cloud-retry"), "重试加载").OnClick(func() { a.Load(cloudvm.CloudNextPage) })
					}
				})
			default:
				a.Empty(c, "网盘中还没有歌曲")
			}
		})
	}
	visible := a.TrackRows(c, d.Tracks, true, header, footer...)
	if at >= 0 {
		a.Locate(c, at, visible)
	} else if current := a.Current(); current != nil && current.Cloud && (d.More || d.Refreshing()) {
		a.LocatePlaying(c, false, func() {
			if current := a.Current(); current != nil && current.Cloud {
				d.Locate = current.Key()
				a.Load(cloudvm.CloudNextPage)
			}
		})
	}
}

func CloudSummary(d *cloudvm.Cloud) string {
	if !d.Loaded {
		return ""
	}
	return fmt.Sprintf("%d 首歌曲", d.Count)
}

func (a *Cloud) Header(c *ui.Context) {
	p := *a.Palette
	ui.Row(c).Padding(0, 12, 14, 12).Gap(12).AlignItems(ui.Center).Children(func() {
		ui.Column(c).Grow(1).Gap(4).Children(func() {
			ui.Text(c, "音乐网盘").FontSize(30).FontWeight(700).LetterSpacing(-0.6)
			if info := CloudSummary(a.Model); info != "" {
				ui.Text(c, info).FontSize(12).TextColor(p.Muted)
			}
		})
	})
}
