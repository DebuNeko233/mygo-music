/*
呈现歌手详情、标题和歌手链接，按模型状态组合歌曲、专辑及动态内容。
*/

package artist

import (
	"fmt"
	"slices"
	"strings"

	"github.com/egoist/mygo/ui"

	"netease-music-mygo/internal/features/navigation"
	"netease-music-mygo/internal/features/netease"
	"netease-music-mygo/internal/ui/view/components"
	"netease-music-mygo/internal/ui/view/page"
	artistvm "netease-music-mygo/internal/ui/viewmodel/artist"
)

type View struct {
	*page.Page
	Model                                                  *artistvm.Artists
	Lookup                                                 func(int64) *artistvm.ArtistPage
	Load, LoadAlbums, LoadEvents, LoadLatest, ToggleFollow func(int64)
	OpenPage                                               func(navigation.Route)
	Play                                                   func([]netease.Track, bool)
	Count                                                  func(int) string
}

// artistLinks shows a track's artists as its credits read, each a link to
// their page in color, brightening to hover under the pointer, and then,
// with album, a link to its album. An artist NetEase has no page for,
// credited by name alone, is plain text. An episode's host and podcast,
// in its artists' and album's place, link to the podcast's page.
func (a *View) Links(c *ui.Context, tr *netease.Track, color, hover ui.Color, album bool) ui.Element {
	podcast := navigation.Route{Page: navigation.Podcast, ID: tr.PodcastID}
	return ui.RichText(c).TextColor(color).Children(func() {
		for i, ar := range tr.Artists {
			if i > 0 {
				ui.Text(c, ", ")
			}
			switch {
			case tr.PodcastID != 0:
				a.Link(c.Key(i), ar.Name, color, hover, podcast)
			case ar.ID != 0:
				a.Link(c.Key(i), ar.Name, color, hover, navigation.Route{Page: navigation.Artist, ID: ar.ID})
			default:
				ui.Text(c, ar.Name)
			}
		}
		if album && tr.Album.Name != "" {
			ui.Text(c, " — ")
			switch {
			case tr.PodcastID != 0:
				a.Link(c.Key("album"), tr.Album.Name, color, hover, podcast)
			case tr.Album.ID != 0:
				a.Link(c.Key("album"), tr.Album.Name, color, hover, navigation.Route{Page: navigation.Album, ID: tr.Album.ID})
			default:
				ui.Text(c, tr.Album.Name)
			}
		}
	})
}

// Link is text in a RichText that opens a page.
func (a *View) Link(c *ui.Context, text string, color, hover ui.Color, r navigation.Route) {
	link := ui.Link(c, text, "").Label("查看：" + text).TextColor(color)
	if link.Hovered() {
		link.TextColor(hover)
	}
	// Once built: a song's row is in a list, which a new page resets.
	link.OnClick(func() { a.OpenPage(r) })
}

// artistPage shows who an artist is over their songs, the most popular or
// the newest, their albums or their events.
func (a *View) Build(c *ui.Context) {
	id := a.Nav.Current.ID
	d := a.Lookup(id)
	if d.Profile == nil {
		if !d.Loading || d.Error != "" {
			a.Centered(c, d.Error, func() { a.Load(id) })
			return
		}
		a.Skeleton(c, true, func(c *ui.Context) { a.HeaderSkeleton(c, components.PageShimmer(c, a.Palette.Dark)) })
		return
	}
	header := func(c *ui.Context) { a.Header(c, d) }
	switch a.Model.Tab {
	case artistvm.ArtistAlbums:
		a.ScrollReady(c, d.AlbumsPage.Loaded, func() {
			header(c)
			a.AlbumGrid(c, d.Albums)
			a.Foot(c, &d.AlbumsPage, func() { a.LoadAlbums(id) })
			if d.AlbumsPage.Loaded && len(d.Albums) == 0 {
				a.Empty(c, "暂无专辑")
			}
		})
		return
	case artistvm.ArtistEvents:
		a.ScrollReady(c, d.EventsPage.Loaded, func() {
			header(c)
			a.Events(c, d)
		})
		return
	}
	songs := d.Profile.Songs
	if a.Model.Tab == artistvm.ArtistLatest {
		switch {
		case d.Latest != nil:
			songs = d.Latest
		case d.LatestLoading && d.LatestError == "":
			a.Skeleton(c, true, header)
			return
		default:
			songs = nil
			header = func(c *ui.Context) {
				a.Header(c, d)
				ui.Column(c).Padding(40, 0).Gap(12).AlignItems(ui.Center).Children(func() {
					ui.Text(c, d.LatestError).TextColor(a.Palette.Muted).FontSize(13)
					if components.IconButton(c.Key("retry"), components.IconReload, 18, a.Palette.Muted, "重新加载歌曲").Tooltip("重新加载歌曲").Clicked() {
						a.LoadLatest(d.Profile.ID)
					}
				})
			}
		}
	}
	a.TrackRows(c, songs, true, header)
}

func (a *View) Header(c *ui.Context, d *artistvm.ArtistPage) {
	p := *a.Palette
	ar := d.Profile
	// What the buttons play: the songs listed, or else the most popular.
	songs := ar.Songs
	if a.Model.Tab == artistvm.ArtistLatest && d.Latest != nil {
		songs = d.Latest
	}
	ui.Row(c).Gap(page.DetailHeaderGap).AlignItems(ui.End).Padding(0, 0, 8, 0).Children(func() {
		photo := ar.Avatar
		if photo == "" {
			photo = ar.PicURL
		}
		ui.Box(c).Size(page.ArtistPhotoSize, page.ArtistPhotoSize).Radius(page.ArtistPhotoSize/2).Shadow(0, 18, 40, -10, p.CardShade).Children(func() {
			a.CoverFill(c, photo, 600, page.ArtistPhotoSize/2)
		})
		ui.Column(c).Grow(1).Shrink(1).MinWidth(0).Gap(8).Children(func() {
			ui.Text(c, "歌手").FontSize(12).FontWeight(600).TextColor(p.Accent)
			ui.Text(c, ar.Name).FontSize(30).FontWeight(800).LetterSpacing(-0.6).MaxLines(2).LineHeight(1.2)
			var meta []string
			if aka := distinct(append([]string{ar.Trans}, ar.Alias...)); len(aka) > 0 {
				meta = append(meta, strings.Join(aka, " / "))
			}
			if ar.MusicSize > 0 {
				meta = append(meta, fmt.Sprintf("%d 首歌曲", ar.MusicSize))
			}
			if ar.AlbumSize > 0 {
				meta = append(meta, fmt.Sprintf("%d 张专辑", ar.AlbumSize))
			}
			ui.Text(c, strings.Join(meta, " · ")).FontSize(13).TextColor(p.Muted)
			a.HeaderDescription(c, ar.Name+" · 简介", ar.BriefDesc, page.ArtistPhotoSize)
			ui.Row(c).Gap(10).Margin(10, 0, 0, 0).Children(func() {
				if components.PillButton(c.Key("play"), components.IconPlay, "播放", p.Accent, p.OnAccent).Clicked() {
					a.Play(songs, false)
				}
				if components.PillButton(c.Key("shuffle"), components.IconShuffle, "随机播放", p.Hover.Over(p.Background), p.Text).Clicked() {
					a.Play(songs, true)
				}
				icon, label := components.IconPlus, "关注"
				if ar.Followed {
					icon, label = components.IconCheck, "已关注"
				}
				if components.PillButton(c.Key("follow"), icon, label, p.Hover.Over(p.Background), p.Text).Clicked() {
					a.ToggleFollow(ar.ID)
				}
			})
		})
	})
	ui.Row(c).AlignItems(ui.Center).Margin(34, 0, 14, 0).Children(func() {
		ui.Text(c, TabTitles[a.Model.Tab]).FontSize(19).FontWeight(700).LetterSpacing(-0.3)
		ui.Spacer(c)
		if components.Segmented(c.Key("tab"), &a.Model.Tab, "热门", "最新", "专辑", "动态") {
			a.Load(ar.ID)
		}
	})
}

var TabTitles = [...]string{"歌曲", "歌曲", "专辑", "动态"}

func (a *View) HeaderSkeleton(c *ui.Context, s components.Shimmer) {
	ui.Row(c).Gap(28).AlignItems(ui.End).Padding(0, 0, 8, 0).Children(func() {
		s.Bone(c, page.ArtistPhotoSize/2).Size(page.ArtistPhotoSize, page.ArtistPhotoSize)
		ui.Column(c).Grow(1).Shrink(1).MinWidth(0).Gap(8).Children(func() {
			s.Text(c, 12, 1.3).Width(24)
			s.Text(c, 30, 1.2).WidthPercent(32)
			s.Text(c, 13, 1.3).Width(160)
			ui.Column(c).Margin(2, 0, 0, 0).Children(func() {
				s.Text(c, 12.5, 1.45).WidthPercent(78)
				s.Text(c, 12.5, 1.45).WidthPercent(52)
			})
			ui.Row(c).Gap(10).Margin(10, 0, 0, 0).Children(func() {
				s.Bone(c, 18).Size(92, 36)
				s.Bone(c, 18).Size(120, 36)
			})
		})
	})
	a.Section(c, "歌曲")
}

// distinct returns ss without empty strings, nor any after its first: an
// artist's translated name is often among their aliases too.
func distinct(ss []string) []string {
	var out []string
	for _, s := range ss {
		if s != "" && !slices.Contains(out, s) {
			out = append(out, s)
		}
	}
	return out
}
