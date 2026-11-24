package app

import (
	"fmt"
	"strings"
	"time"

	"github.com/egoist/mygo/ui"

	"netease-music-mygo/internal/desktop/components"
	"netease-music-mygo/internal/netease"
	podcastvm "netease-music-mygo/internal/viewmodel/podcast"
)

// These load a podcast and its episodes from NetEase, and subscribe to it,
// and the podcasts the tab picks and the most popular; tests replace them.
var (
	podcastDetail    = netease.PodcastDetail
	podcastPrograms  = netease.PodcastPrograms
	subscribePodcast = netease.SubscribePodcast

	recommendedPodcasts = netease.RecommendedPodcasts
	hotPodcasts         = netease.HotPodcasts
)

func (a *app) podcastPage(c *ui.Context) {
	p := a.pal
	id := a.nav.Current.ID
	d := a.podcasts.Pages.Get(id)
	if d.Podcast == nil {
		if !d.Loading {
			a.centered(c, d.Error, func() { a.loadPodcast(id) })
			return
		}
		s := a.pageShimmer(c)
		a.trackSkeleton(c, s, true, func(c *ui.Context) { a.playlistHeaderSkeleton(c, s) })
		return
	}
	pod := d.Podcast
	cur := a.pl.Current()
	a.scrollPage(c, func() {
		const coverSize = 212
		ui.Row(c).Gap(detailHeaderGap).AlignItems(ui.End).Padding(0, 0, 8, 0).Children(func() {
			ui.Box(c).Size(coverSize, coverSize).Radius(14).Shadow(0, 18, 40, -10, p.CardShade).Children(func() {
				a.coverFill(c, pod.PicURL, 600, 14)
			})
			ui.Column(c).Grow(1).Shrink(1).MinWidth(0).Gap(8).Children(func() {
				kind := "播客"
				if pod.Category != "" {
					kind += " · " + pod.Category
				}
				ui.Text(c, kind).FontSize(12).FontWeight(600).TextColor(p.Accent)
				ui.Text(c, pod.Name).FontSize(30).FontWeight(800).LetterSpacing(-0.6).MaxLines(2).LineHeight(1.2)
				ui.RichText(c).FontSize(13).TextColor(p.Muted).Children(func() {
					if pod.DJ.UserID != 0 {
						a.link(c.Key("dj"), pod.DJ.Nickname, p.Muted, p.Text, route{Page: pageUser, ID: pod.DJ.UserID})
						ui.Text(c, " · ")
					}
					ui.Text(c, fmt.Sprintf("%d 期 · %s 人订阅", pod.ProgramCount, count(int(pod.SubCount))))
				})
				a.headerDescription(c, pod.Name+" · 简介", pod.Description, coverSize)
				ui.Row(c).Gap(10).Margin(10, 0, 0, 0).Children(func() {
					if components.PillButton(c.Key("play"), components.IconPlay, "播放", p.Accent, p.OnAccent).Clicked() {
						a.playProgram(d, 0)
					}
					icon, label := components.IconPlus, "订阅"
					if a.podcasts.IsSubscribed(pod) {
						icon, label = components.IconCheck, "已订阅"
					}
					if components.PillButton(c.Key("subscribe"), icon, label, p.Hover.Over(p.Background), p.Text).Clicked() {
						a.toggleSubscribe(d)
					}
				})
			})
		})
		a.section(c, "节目")
		for i := range d.Programs {
			a.programRow(c.Key(d.Programs[i].ID), d, i, cur)
		}
		a.pageFoot(c, &d.Page, func() { a.loadPrograms(id) })
		if d.Page.Loaded && len(d.Programs) == 0 {
			a.empty(c, "暂无节目")
		}
	})
}

// programRow is an episode in its podcast's list, which a click plays.
func (a *app) programRow(c *ui.Context, d *podcastvm.PodcastPage, i int, cur *netease.Track) {
	p := a.pal
	pr := &d.Programs[i]
	playing := cur != nil && cur.ProgramID == pr.ID
	row := ui.ButtonBase(c).Height(60).PaddingX(12).Gap(14).Radius(8).AlignItems(ui.Center).FocusRing(false).
		Label(pr.Name).Disabled(pr.MainSong.ID == 0)
	switch {
	case playing:
		row.Background(p.Accent.Alpha(0.08))
	case row.Hovered():
		row.Background(p.Hover)
	}
	row.Children(func() {
		cover := pr.CoverURL
		if cover == "" {
			cover = d.Podcast.PicURL
		}
		a.cover(c, cover, 96, 40, 6)
		ui.Column(c).Grow(1).Shrink(1).MinWidth(0).Gap(3).Children(func() {
			title := ui.Text(c, pr.Name).FontSize(13.5).FontWeight(500).SingleLine()
			if playing {
				title.TextColor(p.Accent)
			}
			ui.Text(c, time.UnixMilli(pr.CreateTime).Format("2006-01-02")).FontSize(12).TextColor(p.Muted)
		})
		if playing {
			a.nowPlayingBars(c)
		}
		// A liked episode's heart shows; the others' as the pointer is on them.
		ui.Box(c).Size(32, 32).Center().Children(func() {
			if a.library.LikedPrograms[pr.ID] || row.Hovered() {
				tr := pr.Track(d.Podcast)
				a.likeButton(c.Key(fmt.Sprintf("like-%d", pr.ID)), &tr, 15, p.Muted)
			}
		})
		ui.Text(c, components.Clock(time.Duration(pr.Duration)*time.Millisecond)).FontSize(12).FontFeatures("tnum").TextColor(p.Muted)
	})
	if row.Clicked() {
		a.playProgram(d, i)
	}
}

func (a *app) podcastGrid(c *ui.Context, count int, podcast func(int) *netease.Podcast) {
	const gap, outset = 8, 12
	cols := a.columns(c, 340)
	width := (trackPageWidth(c) + 2*outset - gap*float32(cols-1)) / float32(cols)
	ui.Grid(c).Columns(cols).GapX(gap).GapY(4).Margin(0, -outset).Children(func() {
		for i := 0; i < count; i++ {
			pod := podcast(i)
			a.podcastRow(c.Key(pod.ID), pod, width)
		}
	})
}

// podcastRow is a podcast in a list, opening its page.
func (a *app) podcastRow(c *ui.Context, pod *netease.Podcast, width float32) {
	p := a.pal
	const pad, gap, cover = 12, 14, 64
	row := ui.ButtonBase(c).Padding(10, pad).Gap(gap).Radius(10).AlignItems(ui.Center).FocusRing(false).Label(pod.Name)
	if row.Hovered() {
		row.Background(p.Hover)
	}
	row.Children(func() {
		a.cover(c, pod.PicURL, 160, cover, 8)
		ui.Column(c).Grow(1).Shrink(1).MinWidth(0).Gap(4).Children(func() {
			ui.Text(c, pod.Name).FontSize(14).FontWeight(600).SingleLine()
			meta := []string{fmt.Sprintf("%d 期", pod.ProgramCount)}
			if pod.DJ.Nickname != "" {
				meta = append([]string{pod.DJ.Nickname}, meta...)
			}
			ui.Text(c, strings.Join(meta, " · ")).FontSize(12.5).TextColor(p.Muted).SingleLine()
			a.descriptionSummary(c, pod.Name+" · 简介", pod.Description, width-2*pad-gap-cover, 1)
		})
	})
	if row.Clicked() {
		a.open(route{Page: pagePodcast, ID: pod.ID})
	}
}

// programPane is what the episode playing is about, in Now Playing, in
// place of a song's lyrics.
func (a *app) programPane(c *ui.Context, tr *netease.Track, width, height float32) {
	const topPad, rightPad, bottomPad = 4, 12, 48
	column := ui.Scroll(c.Key(tr.ProgramID)).Grow(1).Gap(6).Padding(topPad, rightPad, bottomPad, 0)
	column.Children(func() {
		font := ui.Font{Family: c.Theme().Font, Size: 15, LineHeight: 1.7}
		text := strings.TrimSpace(tr.Description)
		if text == "" {
			text = "这期节目没有简介"
		} else if d := measureDescription(column, text, width-rightPad, font, false); d.height > height-topPad-bottomPad {
			a.descriptionMore(c, tr.Name+" · 简介", text, true)
		}
		ui.Text(c, text).FontSize(font.Size).LineHeight(font.LineHeight).TextColor(white.Alpha(0.75))
	})
}

func (a *app) podcastsPage(c *ui.Context) {
	t := a.podcasts.Home
	if !t.HotPage.Loaded && !t.Loading {
		a.centered(c, t.Error, a.loadPodcastTab)
		return
	}
	top, bottom := a.insets(c)
	ui.Scroll(c.Key("podcasts")).TrackScroll(&a.shell.PageScroll).Grow(1).Padding(top+pageGap, pagePad, bottom+48, pagePad).ScrollbarInsets(top, 0, bottom, 0).Children(func() {
		a.startMark(c)
		defer a.endMark(c)
		a.heading(c, "播客", "")
		cols := a.columns(c, 150)
		if !t.HotPage.Loaded {
			s := a.pageShimmer(c)
			a.section(c, "精选播客").Margin(0, 0, 14, 0)
			a.playlistCardsSkeleton(c, s, cols, 150, true)
			a.section(c, "热门播客")
			a.playlistCardsSkeleton(c, s, cols*2, 150, true)
			return
		}
		// The first list starts right under the heading.
		first := true
		list := func(title string, pods []netease.Podcast) {
			if len(pods) == 0 {
				return
			}
			if s := a.section(c, title); first {
				s.Margin(0, 0, 14, 0)
			}
			first = false
			ui.Grid(c.Key(title)).Columns(cols).GapX(20).GapY(28).Children(func() {
				for i := range pods {
					a.podcastCard(c.Key(pods[i].ID), &pods[i])
				}
			})
		}
		list("我的订阅", t.Subs)
		list("精选播客", t.Picks)
		list("热门播客", t.Hot)
		a.pageFoot(c, &t.HotPage, a.loadHotPodcasts)
	})
}

// podcastCard is a podcast's cover over its name and, for those the
// listener subscribes to, its newest episode; for the others, why to
// listen.
func (a *app) podcastCard(c *ui.Context, pod *netease.Podcast) {
	sub := pod.LastProgram
	if sub == "" {
		sub = pod.RcmdText
	}
	if sub == "" {
		sub = pod.DJ.Nickname
	}
	id := pod.ID
	if a.coverCard(c, pod.Name, pod.PicURL, pod.PlayCount, sub, func() { a.playPodcast(id) }) {
		a.open(route{Page: pagePodcast, ID: id})
	}
}
