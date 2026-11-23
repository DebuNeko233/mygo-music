package app

import (
	"fmt"
	"slices"
	"strings"

	"github.com/egoist/mygo/ui"

	"netease-music-mygo/internal/netease"
	"netease-music-mygo/internal/viewmodel"
)

// artistLinks shows a track's artists as its credits read, each a link to
// their page in color, brightening to hover under the pointer, and then,
// with album, a link to its album. An artist NetEase has no page for,
// credited by name alone, is plain text. An episode's host and podcast,
// in its artists' and album's place, link to the podcast's page.
func (a *app) artistLinks(c *ui.Context, tr *netease.Track, color, hover ui.Color, album ...bool) ui.Element {
	podcast := route{Page: pagePodcast, ID: tr.PodcastID}
	return ui.RichText(c).TextColor(color).Children(func() {
		for i, ar := range tr.Artists {
			if i > 0 {
				ui.Text(c, ", ")
			}
			switch {
			case tr.PodcastID != 0:
				a.link(c.Key(i), ar.Name, color, hover, podcast)
			case ar.ID != 0:
				a.link(c.Key(i), ar.Name, color, hover, route{Page: pageArtist, ID: ar.ID})
			default:
				ui.Text(c, ar.Name)
			}
		}
		if len(album) > 0 && album[0] && tr.Album.Name != "" {
			ui.Text(c, " — ")
			switch {
			case tr.PodcastID != 0:
				a.link(c.Key("album"), tr.Album.Name, color, hover, podcast)
			case tr.Album.ID != 0:
				a.link(c.Key("album"), tr.Album.Name, color, hover, route{Page: pageAlbum, ID: tr.Album.ID})
			default:
				ui.Text(c, tr.Album.Name)
			}
		}
	})
}

// link is text in a RichText that opens a page.
func (a *app) link(c *ui.Context, text string, color, hover ui.Color, r route) {
	link := ui.Link(c, text, "").Label("查看：" + text).TextColor(color)
	if link.Hovered() {
		link.TextColor(hover)
	}
	// Once built: a song's row is in a list, which a new page resets.
	link.OnClick(func() { a.openPage(r) })
}

// openPage opens a page in the main window, from wherever a link to it
// shows: over Now Playing, which closes, or the notch.
func (a *app) openPage(r route) {
	a.open(r)
	a.showMain()
	a.redraw()
}

// openArtist opens an artist's page in the main window.
func (a *app) openArtist(id int64) { a.openPage(route{Page: pageArtist, ID: id}) }

// These load an artist, their songs, albums and events from NetEase, and
// follow them; tests replace them.
var (
	artistDetail      = netease.ArtistDetail
	artistLatestSongs = netease.ArtistLatestSongs
	albumsBy          = netease.ArtistAlbums
	accountEvents     = netease.Events
	followArtist      = netease.FollowArtist
)

// artistPage shows who an artist is over their songs, the most popular or
// the newest, their albums or their events.
func (a *app) artistPage(c *ui.Context) {
	id := a.nav.Current.ID
	d := a.artist(id)
	if d.Profile == nil {
		if !d.Loading || d.Error != "" {
			a.centered(c, d.Error, func() { a.loadArtist(id) })
			return
		}
		s := a.pageShimmer(c)
		a.trackSkeleton(c, s, true, func(c *ui.Context) { a.artistHeaderSkeleton(c, s) })
		return
	}
	header := func(c *ui.Context) { a.artistHeader(c, d) }
	switch a.artists.Tab {
	case viewmodel.ArtistAlbums:
		a.scrollPage(c, func() {
			header(c)
			a.albumGrid(c, d.Albums)
			a.pageFoot(c, &d.AlbumsPage, func() { a.loadAlbums(id) })
			if d.AlbumsPage.Loaded && len(d.Albums) == 0 {
				a.empty(c, "暂无专辑")
			}
		})
		return
	case viewmodel.ArtistEvents:
		a.scrollPage(c, func() {
			header(c)
			a.artistEvents(c, d)
		})
		return
	}
	songs := d.Profile.Songs
	if a.artists.Tab == viewmodel.ArtistLatest {
		switch {
		case d.Latest != nil:
			songs = d.Latest
		case d.LatestLoading && d.LatestError == "":
			a.trackSkeleton(c, a.pageShimmer(c), true, header)
			return
		default:
			songs = nil
			header = func(c *ui.Context) {
				a.artistHeader(c, d)
				ui.Column(c).Padding(40, 0).Gap(12).AlignItems(ui.Center).Children(func() {
					ui.Text(c, d.LatestError).TextColor(a.pal.muted).FontSize(13)
					if iconButton(c.Key("retry"), iconReload, 18, a.pal.muted, "重新加载歌曲").Tooltip("重新加载歌曲").Clicked() {
						a.loadLatest(d.Profile.ID)
					}
				})
			}
		}
	}
	a.trackRows(c, songs, true, header)
}

// scrollPage scrolls a page of cards and rows, inset as one of tracks is:
// a tab of a page that lists tracks puts its header in the same place.
func (a *app) scrollPage(c *ui.Context, content func()) {
	top, bottom := a.insets(c)
	ui.Scroll(c.Key(a.nav.Current)).TrackScroll(&a.pageScroll).Grow(1).Padding(top+pageGap, trackPagePad, bottom+32, trackPagePad).ScrollbarInsets(top, 0, bottom, 0).Children(func() {
		a.startMark(c)
		defer a.endMark(c)
		content()
	})
}

// empty says a list has nothing in it.
func (a *app) empty(c *ui.Context, msg string) {
	ui.Text(c, msg).FontSize(13).TextColor(a.pal.muted).Padding(40, 0).AlignSelf(ui.Center)
}

// artistPhotoSize is the width of an artist's round photo on their page.
const artistPhotoSize = 212

func (a *app) artistHeader(c *ui.Context, d *viewmodel.ArtistPage) {
	p := a.pal
	ar := d.Profile
	// What the buttons play: the songs listed, or else the most popular.
	songs := ar.Songs
	if a.artists.Tab == viewmodel.ArtistLatest && d.Latest != nil {
		songs = d.Latest
	}
	ui.Row(c).Gap(detailHeaderGap).AlignItems(ui.End).Padding(0, 0, 8, 0).Children(func() {
		photo := ar.Avatar
		if photo == "" {
			photo = ar.PicURL
		}
		ui.Box(c).Size(artistPhotoSize, artistPhotoSize).Radius(artistPhotoSize/2).Shadow(0, 18, 40, -10, p.cardShade).Children(func() {
			a.coverFill(c, photo, 600, artistPhotoSize/2)
		})
		ui.Column(c).Grow(1).Shrink(1).MinWidth(0).Gap(8).Children(func() {
			ui.Text(c, "歌手").FontSize(12).FontWeight(600).TextColor(p.accent)
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
			ui.Text(c, strings.Join(meta, " · ")).FontSize(13).TextColor(p.muted)
			a.headerDescription(c, ar.Name+" · 简介", ar.BriefDesc, artistPhotoSize)
			ui.Row(c).Gap(10).Margin(10, 0, 0, 0).Children(func() {
				if pillButton(c.Key("play"), iconPlay, "播放", p.accent, p.onAccent).Clicked() {
					if a.pl.Shuffle() {
						a.pl.ToggleShuffle()
					}
					a.pl.PlayQueueFrom(songs, firstPlayable(songs), a.playSource())
				}
				if pillButton(c.Key("shuffle"), iconShuffle, "随机播放", p.hover.Over(p.bg), p.text).Clicked() {
					if !a.pl.Shuffle() {
						a.pl.ToggleShuffle()
					}
					a.pl.PlayQueueFrom(songs, firstPlayable(songs), a.playSource())
				}
				icon, label := iconPlus, "关注"
				if ar.Followed {
					icon, label = iconCheck, "已关注"
				}
				if pillButton(c.Key("follow"), icon, label, p.hover.Over(p.bg), p.text).Clicked() {
					a.toggleFollow(ar.ID)
				}
			})
		})
	})
	ui.Row(c).AlignItems(ui.Center).Margin(34, 0, 14, 0).Children(func() {
		ui.Text(c, artistTabTitles[a.artists.Tab]).FontSize(19).FontWeight(700).LetterSpacing(-0.3)
		ui.Spacer(c)
		if segmented(c.Key("tab"), &a.artists.Tab, "热门", "最新", "专辑", "动态") {
			a.loadArtist(ar.ID)
		}
	})
}

var artistTabTitles = [...]string{"歌曲", "歌曲", "专辑", "动态"}

func (a *app) artistHeaderSkeleton(c *ui.Context, s shimmer) {
	ui.Row(c).Gap(28).AlignItems(ui.End).Padding(0, 0, 8, 0).Children(func() {
		s.bone(c, artistPhotoSize/2).Size(artistPhotoSize, artistPhotoSize)
		ui.Column(c).Grow(1).Shrink(1).MinWidth(0).Gap(8).Children(func() {
			s.text(c, 12, 1.3).Width(24)
			s.text(c, 30, 1.2).WidthPercent(32)
			s.text(c, 13, 1.3).Width(160)
			ui.Column(c).Margin(2, 0, 0, 0).Children(func() {
				s.text(c, 12.5, 1.45).WidthPercent(78)
				s.text(c, 12.5, 1.45).WidthPercent(52)
			})
			ui.Row(c).Gap(10).Margin(10, 0, 0, 0).Children(func() {
				s.bone(c, 18).Size(92, 36)
				s.bone(c, 18).Size(120, 36)
			})
		})
	})
	a.section(c, "歌曲")
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
