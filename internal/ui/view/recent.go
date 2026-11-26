/*
呈现最近播放记录的分类、标题及节目行，并连接记录播放操作。
*/

package view

import (
	"time"

	"github.com/egoist/mygo/ui"

	"netease-music-mygo/internal/features/netease"
	"netease-music-mygo/internal/ui/view/components"
	"netease-music-mygo/internal/ui/view/page"
	accountvm "netease-music-mygo/internal/ui/viewmodel/account"
	recentvm "netease-music-mygo/internal/ui/viewmodel/recent"
)

type Recent struct {
	*page.Page
	Model    *recentvm.Recent
	Account  *accountvm.Account
	Load     func()
	LoadTab  func(int)
	Play     func([]netease.Track)
	PlayedAt func(time.Time, string, time.Time) string
}

var RecentTabTitles = [...]string{"歌曲", "歌单", "专辑", "播客", "声音"}

func (a *Recent) Build(c *ui.Context) {
	if a.Account.Profile == nil {
		a.Centered(c, "登录后查看最近播放", nil)
		return
	}
	a.Load()
	d := a.Model
	tab := a.Model.Selected
	state := &d.Tabs[tab]
	header := func(c *ui.Context) { a.Header(c) }
	if !state.Shown {
		if d.ReadingSnapshot || state.Loading {
			a.Skeleton(c, false, header)
			return
		}
		a.Scroll(c, func() {
			header(c)
			a.Centered(c, "最近播放加载失败", func() { a.LoadTab(tab) })
		})
		return
	}
	switch a.Model.Selected {
	case recentvm.RecentPlaylists:
		a.Scroll(c, func() {
			header(c)
			if len(d.Playlists) == 0 {
				a.Empty(c, "最近没有听过歌单")
				return
			}
			ui.Grid(c).Columns(a.Columns(c, 168)).GapX(20).GapY(28).Children(func() {
				for i := range d.Playlists {
					a.PlaylistCard(c.Key(d.Playlists[i].Item.ID), &d.Playlists[i].Item)
				}
			})
		})
	case recentvm.RecentAlbums:
		a.Scroll(c, func() {
			header(c)
			if len(d.Albums) == 0 {
				a.Empty(c, "最近没有听过专辑")
				return
			}
			a.AlbumGrid(c, d.AlbumItems)
		})
	case recentvm.RecentPodcasts:
		a.Scroll(c, func() {
			header(c)
			if len(d.Podcasts) == 0 {
				a.Empty(c, "最近没有听过播客")
				return
			}
			a.PodcastGrid(c, len(d.Podcasts), func(i int) *netease.Podcast { return &d.Podcasts[i].Item })
		})
	case recentvm.RecentVoices:
		a.Scroll(c, func() {
			header(c)
			if len(d.Voices) == 0 {
				a.Empty(c, "最近没有听过声音")
				return
			}
			for i := range d.Voices {
				a.VoiceRow(c, &d.Voices[i])
			}
		})
	default:
		if len(d.Tracks) == 0 {
			a.Scroll(c, func() {
				header(c)
				a.Empty(c, "最近没有听过歌曲")
			})
			return
		}
		// When and where each song was played, as its row is built: a
		// frame builds the rows in view, not the hundreds of the record.
		now := c.Now()
		played := func(i int) string { return a.PlayedAt(d.Songs[i].At, d.Songs[i].Device, now) }
		// The songs come from albums of their own: their covers say which.
		a.TrackRowsMeta(c, d.Tracks, true, played, header)
	}
}

// recentHeader is the top of the recent page: what it lists, and the tabs of
// the kinds of play record NetEase keeps.
func (a *Recent) Header(c *ui.Context) {
	a.Heading(c, "最近播放", "手机、电脑和网页版上听过的")
	ui.Row(c).AlignItems(ui.Center).Margin(34, 0, 14, 0).Children(func() {
		ui.Text(c, RecentTabTitles[a.Model.Selected]).FontSize(19).FontWeight(700).LetterSpacing(-0.3)
		ui.Spacer(c)
		components.Segmented(c.Key("tab"), &a.Model.Selected, RecentTabTitles[:]...)
	})
}

// VoiceRow is an episode the listener played under 声音, which plays when
// clicked, under the podcast it is on.
func (a *Recent) VoiceRow(c *ui.Context, v *netease.Played[netease.Voice]) {
	p := *a.Palette
	tr := v.Item.Program.Track(&v.Item.Podcast)
	row := ui.ButtonBase(c.Key(v.Item.Program.ID)).Height(54).Padding(0, 12).Gap(14).Radius(10).AlignItems(ui.Center).FocusRing(false).Label(tr.Name)
	if row.Hovered() {
		row.Background(p.Hover)
	}
	row.Children(func() {
		a.Cover(c, tr.Album.PicURL, 96, 38, 6)
		ui.Column(c).Grow(3).Basis(0).MinWidth(0).Gap(3).Children(func() {
			ui.Text(c, tr.Name).FontSize(14).FontWeight(500).SingleLine()
			ui.Text(c, v.Item.Podcast.Name).FontSize(12.5).TextColor(p.Muted).SingleLine()
		})
		ui.Text(c, a.PlayedAt(v.At, v.Device, c.Now())).FontSize(12.5).TextColor(p.Faint).MinWidth(88).TextAlign(ui.End)
	})
	if row.Clicked() {
		a.Play([]netease.Track{tr})
	}
}
