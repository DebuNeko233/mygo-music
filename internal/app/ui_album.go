package app

import (
	"fmt"
	"strings"
	"time"

	"github.com/egoist/mygo/ui"

	"netease-music-mygo/internal/desktop/components"
	"netease-music-mygo/internal/netease"
)

// albumDetail loads an album from NetEase; tests replace it.
var (
	albumDetail = netease.AlbumDetail
)

func (a *app) albumPage(c *ui.Context) {
	id := a.nav.Current.ID
	// Whether the listener saved this album is whether the albums they saved
	// list it: load that once, as the page may be the first thing opened.
	a.freshenAlbums()
	d := a.albums.Pages.Get(id)
	if d.Album == nil {
		if !d.Loading || d.Error != "" {
			a.centered(c, d.Error, func() { a.loadAlbum(id) })
			return
		}
		s := a.pageShimmer(c)
		a.trackSkeleton(c, s, false, func(c *ui.Context) { a.playlistHeaderSkeleton(c, s) })
		return
	}
	a.trackRows(c, d.Album.Songs, false, func(c *ui.Context) { a.albumHeader(c, d.Album) })
}

func (a *app) albumHeader(c *ui.Context, al *netease.AlbumProfile) {
	p := a.pal
	const coverSize = 212
	ui.Row(c).Gap(detailHeaderGap).AlignItems(ui.End).Padding(0, 0, 26, 0).Children(func() {
		ui.Box(c).Size(coverSize, coverSize).Radius(14).Shadow(0, 18, 40, -10, p.CardShade).Children(func() {
			a.coverFill(c, al.PicURL, 600, 14)
		})
		ui.Column(c).Grow(1).Shrink(1).MinWidth(0).Gap(8).Children(func() {
			ui.Text(c, "专辑").FontSize(12).FontWeight(600).TextColor(p.Accent)
			ui.Text(c, al.Name).FontSize(30).FontWeight(800).LetterSpacing(-0.6).MaxLines(2).LineHeight(1.2)
			ui.RichText(c).FontSize(13).TextColor(p.Muted).Children(func() {
				for i, ar := range al.Artists {
					if i > 0 {
						ui.Text(c, ", ")
					}
					a.link(c.Key(i), ar.Name, p.Muted, p.Text, route{Page: pageArtist, ID: ar.ID})
				}
				meta := fmt.Sprintf(" · %d 首", len(al.Songs))
				if al.PublishTime > 0 {
					meta = " · " + time.UnixMilli(al.PublishTime).Format("2006-01-02") + meta
				}
				ui.Text(c, meta)
			})
			a.headerDescription(c, al.Name+" · 简介", al.Description, coverSize)
			ui.Row(c).Gap(10).Margin(10, 0, 0, 0).Children(func() {
				if components.PillButton(c.Key("play"), components.IconPlay, "播放", p.Accent, p.OnAccent).Clicked() {
					if a.pl.Shuffle() {
						a.pl.ToggleShuffle()
					}
					a.pl.PlayQueueFrom(al.Songs, firstPlayable(al.Songs), a.playSource())
				}
				icon, label := components.IconPlus, "收藏"
				if a.albumSaved(al.ID) {
					icon, label = components.IconCheck, "已收藏"
				}
				if components.PillButton(c.Key("save"), icon, label, p.Hover.Over(p.Background), p.Text).Clicked() {
					a.toggleAlbumSaved(*al)
				}
				components.IconButton(c.Key("more"), components.IconMore, 18, p.Muted, "更多").Menu(func(m *ui.Menu) {
					if m.Item("下载全部").Disabled(len(al.Songs) == 0).Chosen() {
						a.enqueueDownloads(al.Songs)
					}
				})
			})
		})
	})
}

// albumGrid shows albums as cards, each opening its page.
func (a *app) albumGrid(c *ui.Context, albums []netease.AlbumProfile) {
	ui.Grid(c).Columns(a.columns(c, 156)).GapX(20).GapY(28).Children(func() {
		for i := range albums {
			a.albumCard(c.Key(albums[i].ID), &albums[i])
		}
	})
}

// albumCard is an album's cover, lifting under the pointer, over its name
// and the year it came out.
func (a *app) albumCard(c *ui.Context, al *netease.AlbumProfile) {
	p := a.pal
	card := ui.ButtonBase(c).Column().Gap(10).AlignItems(ui.Stretch).FocusRing(false).Label(al.Name)
	hovered := card.Hovered()
	card.Children(func() {
		art := ui.Box(c).AspectRatio(1).Radius(10)
		lift := ui.Pose{}
		if hovered {
			lift = ui.Pose{Y: -3}
			art.Shadow(0, 12, 28, -6, p.CardShade)
		} else {
			art.Shadow(0, 2, 8, -2, p.CardShade)
		}
		art.AnimatePose(lift, ui.Snappy)
		art.Children(func() { a.coverFill(c, al.PicURL, 400, 10) })
		ui.Text(c, al.Name).FontSize(13).FontWeight(500).MaxLines(2).LineHeight(1.35)
		var meta []string
		if a.nav.Current.Page != pageArtist {
			meta = append(meta, al.ArtistNames())
		}
		if al.PublishTime > 0 {
			meta = append(meta, time.UnixMilli(al.PublishTime).Format("2006"))
		}
		ui.Text(c, strings.Join(meta, " · ")).FontSize(12).TextColor(p.Muted).SingleLine().Margin(-6, 0, 0, 0)
	})
	if card.Clicked() {
		a.open(route{Page: pageAlbum, ID: al.ID})
	}
}
