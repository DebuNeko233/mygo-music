package app

import (
	"time"

	"github.com/egoist/mygo/ui"

	"netease-music-mygo/internal/desktop/components"
	"netease-music-mygo/internal/netease"
	recentvm "netease-music-mygo/internal/viewmodel/recent"
)

var recentTabTitles = [...]string{"歌曲", "歌单", "专辑", "播客", "声音"}

func (a *app) recentPage(c *ui.Context) {
	if a.account.Profile == nil {
		a.centered(c, "登录后查看最近播放", nil)
		return
	}
	a.loadRecent()
	d := &a.recent
	tab := a.recent.Selected
	state := &d.Tabs[tab]
	header := func(c *ui.Context) { a.recentHeader(c) }
	if !state.Shown {
		if d.ReadingSnapshot || state.Loading {
			a.trackSkeleton(c, a.pageShimmer(c), false, header)
			return
		}
		a.scrollPage(c, func() {
			header(c)
			a.centered(c, "最近播放加载失败", func() { a.loadRecentTab(tab) })
		})
		return
	}
	switch a.recent.Selected {
	case recentvm.RecentPlaylists:
		a.scrollPage(c, func() {
			header(c)
			if len(d.Playlists) == 0 {
				a.empty(c, "最近没有听过歌单")
				return
			}
			ui.Grid(c).Columns(a.columns(c, 168)).GapX(20).GapY(28).Children(func() {
				for i := range d.Playlists {
					a.playlistCard(c.Key(d.Playlists[i].Item.ID), &d.Playlists[i].Item)
				}
			})
		})
	case recentvm.RecentAlbums:
		a.scrollPage(c, func() {
			header(c)
			if len(d.Albums) == 0 {
				a.empty(c, "最近没有听过专辑")
				return
			}
			a.albumGrid(c, d.AlbumItems)
		})
	case recentvm.RecentPodcasts:
		a.scrollPage(c, func() {
			header(c)
			if len(d.Podcasts) == 0 {
				a.empty(c, "最近没有听过播客")
				return
			}
			a.podcastGrid(c, len(d.Podcasts), func(i int) *netease.Podcast { return &d.Podcasts[i].Item })
		})
	case recentvm.RecentVoices:
		a.scrollPage(c, func() {
			header(c)
			if len(d.Voices) == 0 {
				a.empty(c, "最近没有听过声音")
				return
			}
			for i := range d.Voices {
				a.voiceRow(c, &d.Voices[i])
			}
		})
	default:
		if len(d.Tracks) == 0 {
			a.scrollPage(c, func() {
				header(c)
				a.empty(c, "最近没有听过歌曲")
			})
			return
		}
		// When and where each song was played, as its row is built: a
		// frame builds the rows in view, not the hundreds of the record.
		now := c.Now()
		played := func(i int) string { return playedAt(d.Songs[i].At, d.Songs[i].Device, now) }
		// The songs come from albums of their own: their covers say which.
		a.trackRowsMeta(c, d.Tracks, true, played, header)
	}
}

// recentHeader is the top of the recent page: what it lists, and the tabs of
// the kinds of play record NetEase keeps.
func (a *app) recentHeader(c *ui.Context) {
	a.heading(c, "最近播放", "手机、电脑和网页版上听过的")
	ui.Row(c).AlignItems(ui.Center).Margin(34, 0, 14, 0).Children(func() {
		ui.Text(c, recentTabTitles[a.recent.Selected]).FontSize(19).FontWeight(700).LetterSpacing(-0.3)
		ui.Spacer(c)
		components.Segmented(c.Key("tab"), &a.recent.Selected, recentTabTitles[:]...)
	})
}

// voiceRow is an episode the listener played under 声音, which plays when
// clicked, under the podcast it is on.
func (a *app) voiceRow(c *ui.Context, v *netease.Played[netease.Voice]) {
	p := a.pal
	tr := v.Item.Program.Track(&v.Item.Podcast)
	row := ui.ButtonBase(c.Key(v.Item.Program.ID)).Height(54).Padding(0, 12).Gap(14).Radius(10).AlignItems(ui.Center).FocusRing(false).Label(tr.Name)
	if row.Hovered() {
		row.Background(p.Hover)
	}
	row.Children(func() {
		a.cover(c, tr.Album.PicURL, 96, 38, 6)
		ui.Column(c).Grow(3).Basis(0).MinWidth(0).Gap(3).Children(func() {
			ui.Text(c, tr.Name).FontSize(14).FontWeight(500).SingleLine()
			ui.Text(c, v.Item.Podcast.Name).FontSize(12.5).TextColor(p.Muted).SingleLine()
		})
		ui.Text(c, playedAt(v.At, v.Device, c.Now())).FontSize(12.5).TextColor(p.Faint).MinWidth(88).TextAlign(ui.End)
	})
	if row.Clicked() {
		a.pl.PlayQueue([]netease.Track{tr}, 0)
	}
}

// playedAt says when and on what the listener played something, as
// "12分钟前 · 手机".
func playedAt(at time.Time, device string, now time.Time) string {
	when := agoLabel(now.Sub(at))
	if where := deviceName(device); where != "" {
		return when + " · " + where
	}
	return when
}
