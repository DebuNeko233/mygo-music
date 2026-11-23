package app

import (
	"errors"
	"fmt"

	"github.com/egoist/mygo/ui"

	"netease-music-mygo/internal/netease"
)

func (a *app) resolveCloudLocate() {
	d := &a.cloud
	if d.locate == 0 {
		return
	}
	current := a.pl.Current()
	if a.route.page != pageCloud || a.nowPlaying || current == nil || current.Key() != d.locate {
		d.locate = 0
		return
	}
	if d.failure != nil {
		a.toast = "定位歌曲失败，请稍后重试"
		if errors.Is(d.failure, netease.ErrNotLoggedIn) {
			a.toast = "登录已失效，请重新登录"
		}
		return
	}
	if at := a.currentTrackIndex(d.tracks); at >= 0 {
		d.locate = 0
		a.revealTrack(at)
	} else if d.pending == nil && !d.more {
		d.locate = 0
		a.toast = "当前网盘中未找到这首歌曲"
	}
}

func (a *app) cloudPage(c *ui.Context) {
	if a.profile == nil {
		top, bottom := a.insets(c)
		ui.Column(c).Grow(1).Padding(top, 0, bottom, 0).Center().Gap(12).Children(func() {
			ui.Text(c, "登录后查看音乐网盘").FontSize(14).TextColor(a.pal.muted)
			ui.PrimaryButton(c.Key("cloud-sign-in"), "登录网易云音乐").OnClick(a.openLogin)
		})
		return
	}
	d := &a.cloud
	if !d.loading() && d.failure == nil && (!d.loaded || d.more) {
		a.loadCloud(cloudNextPage)
	}
	header := func(c *ui.Context) { a.cloudHeader(c) }
	if !d.loaded && d.loading() {
		a.trackSkeleton(c, a.pageShimmer(c), true, header)
		return
	}
	at := a.currentTrackIndex(d.tracks)
	a.syncTrackViewport(c, trackHeaderKey{name: "音乐网盘", description: d.summary(), tracks: len(d.tracks)}, at)
	var footer []func(*ui.Context)
	if d.failure != nil || d.loaded && !d.loading() && len(d.tracks) == 0 {
		footer = append(footer, func(c *ui.Context) {
			switch {
			case d.failure != nil:
				ui.Column(c).Center().Gap(12).Padding(24, 0, 24, 0).Children(func() {
					message := "网盘加载失败，请重试"
					if errors.Is(d.failure, netease.ErrNotLoggedIn) {
						message = "登录已失效，请重新登录"
					}
					ui.Text(c, message).FontSize(13).TextColor(a.pal.muted)
					if errors.Is(d.failure, netease.ErrNotLoggedIn) {
						ui.Button(c.Key("cloud-login"), "重新登录").OnClick(a.openLogin)
					} else {
						ui.Button(c.Key("cloud-retry"), "重试加载").OnClick(func() { a.loadCloud(cloudNextPage) })
					}
				})
			default:
				a.empty(c, "网盘中还没有歌曲")
			}
		})
	}
	visible := a.trackRows(c, d.tracks, true, header, footer...)
	if at >= 0 {
		a.trackLocateButton(c, at, visible)
	} else if current := a.pl.Current(); current != nil && current.Cloud && (d.more || d.pending != nil) {
		a.locatePlayingButton(c, false, func() {
			if current := a.pl.Current(); current != nil && current.Cloud {
				d.locate = current.Key()
				a.loadCloud(cloudNextPage)
			}
		})
	}
}

func (d *cloudViewModel) summary() string {
	if !d.loaded {
		return ""
	}
	return fmt.Sprintf("%d 首歌曲", d.count)
}

func (a *app) cloudHeader(c *ui.Context) {
	p := a.pal
	ui.Row(c).Padding(0, 12, 14, 12).Gap(12).AlignItems(ui.Center).Children(func() {
		ui.Column(c).Grow(1).Gap(4).Children(func() {
			ui.Text(c, "音乐网盘").FontSize(30).FontWeight(700).LetterSpacing(-0.6)
			if info := a.cloud.summary(); info != "" {
				ui.Text(c, info).FontSize(12).TextColor(p.muted)
			}
		})
	})
}
