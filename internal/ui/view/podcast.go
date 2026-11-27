/*
呈现播客推荐、详情和节目列表，并连接订阅与节目播放交互。
*/

package view

import (
	"fmt"
	"strings"
	"time"

	"github.com/egoist/mygo/ui"

	"netease-music-mygo/internal/features/navigation"
	"netease-music-mygo/internal/features/netease"
	"netease-music-mygo/internal/ui/view/components"
	"netease-music-mygo/internal/ui/view/page"
	"netease-music-mygo/internal/ui/view/shell"
	libraryvm "netease-music-mygo/internal/ui/viewmodel/library"
	podcastvm "netease-music-mygo/internal/ui/viewmodel/podcast"
)

type Podcast struct {
	*page.Page
	Model             *podcastvm.Podcasts
	Library           *libraryvm.Library
	Load, PlayPodcast func(int64)
	LoadPrograms      func(int64)
	ToggleSubscribe   func(*podcastvm.PodcastPage)
	PlayProgram       func(*podcastvm.PodcastPage, int)
	LoadHome, LoadHot func()
	Open              func(navigation.Route)
	Count             func(int) string
	Current           func() *netease.Track
	Playing           func() bool
	PlayingBars       func(*ui.Context, ...ui.Color)
	LikeButton        func(*ui.Context, *netease.Track, float32, ui.Color) ui.Element
	DescriptionMore   func(*ui.Context, string, string, bool)
}

func (a *Podcast) Build(c *ui.Context) {
	p := *a.Palette
	id := a.Nav.Current.ID
	d := a.Model.Pages.Get(id)
	if d.Podcast == nil {
		if !d.Loading {
			a.Centered(c, d.Error, func() { a.Load(id) })
			return
		}
		a.Skeleton(c, true, func(c *ui.Context) { a.HeaderSkeleton(c) })
		return
	}
	pod := d.Podcast
	cur := a.Current()
	a.Scroll(c, func() {
		const coverSize = 212
		ui.Row(c).Gap(page.DetailHeaderGap).AlignItems(ui.End).Padding(0, 0, 8, 0).Children(func() {
			ui.Box(c).Size(coverSize, coverSize).Radius(14).Shadow(0, 18, 40, -10, p.CardShade).Children(func() {
				a.CoverFill(c, pod.PicURL, 600, 14)
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
						a.Link(c.Key("dj"), pod.DJ.Nickname, p.Muted, p.Text, navigation.Route{Page: navigation.User, ID: pod.DJ.UserID})
						ui.Text(c, " · ")
					}
					ui.Text(c, fmt.Sprintf("%d 期 · %s 人订阅", pod.ProgramCount, a.Count(int(pod.SubCount))))
				})
				a.HeaderDescription(c, pod.Name+" · 简介", pod.Description, coverSize)
				ui.Row(c).Gap(10).Margin(10, 0, 0, 0).Children(func() {
					if components.PillButton(c.Key("play"), components.IconPlay, "播放", p.Accent, p.OnAccent).Clicked() {
						a.PlayProgram(d, 0)
					}
					icon, label := components.IconPlus, "订阅"
					if a.Model.IsSubscribed(pod) {
						icon, label = components.IconCheck, "已订阅"
					}
					if components.PillButton(c.Key("subscribe"), icon, label, p.Hover.Over(p.Background), p.Text).Clicked() {
						a.ToggleSubscribe(d)
					}
				})
			})
		})
		a.Section(c, "节目")
		for i := range d.Programs {
			a.ProgramRow(c.Key(d.Programs[i].ID), d, i, cur)
		}
		a.Foot(c, &d.Page, func() { a.LoadPrograms(id) })
		if d.Page.Loaded && len(d.Programs) == 0 {
			a.Empty(c, "暂无节目")
		}
	})
}

// ProgramRow is an episode in its podcast's list, which a click plays.
func (a *Podcast) ProgramRow(c *ui.Context, d *podcastvm.PodcastPage, i int, cur *netease.Track) {
	p := *a.Palette
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
		a.Cover(c, cover, 96, 40, 6)
		ui.Column(c).Grow(1).Shrink(1).MinWidth(0).Gap(3).Children(func() {
			title := ui.Text(c, pr.Name).FontSize(13.5).FontWeight(500).SingleLine()
			if playing {
				title.TextColor(p.Accent)
			}
			ui.Text(c, time.UnixMilli(pr.CreateTime).Format("2006-01-02")).FontSize(12).TextColor(p.Muted)
		})
		if playing {
			a.PlayingBars(c)
		}
		// A liked episode's heart shows; the others' as the pointer is on them.
		ui.Box(c).Size(32, 32).Center().Children(func() {
			if a.Library.LikedPrograms[pr.ID] || row.Hovered() {
				tr := pr.Track(d.Podcast)
				a.LikeButton(c.Key(fmt.Sprintf("like-%d", pr.ID)), &tr, 15, p.Muted)
			}
		})
		ui.Text(c, components.Clock(time.Duration(pr.Duration)*time.Millisecond)).FontSize(12).FontFeatures("tnum").TextColor(p.Muted)
	})
	if row.Clicked() {
		a.PlayProgram(d, i)
	}
}

func (a *Podcast) Grid(c *ui.Context, count int, podcast func(int) *netease.Podcast) {
	const gap, outset = 8, 12
	cols := a.Columns(c, 340)
	width := (page.TrackWidth(c) + 2*outset - gap*float32(cols-1)) / float32(cols)
	ui.Grid(c).Columns(cols).GapX(gap).GapY(4).Margin(0, -outset).Children(func() {
		for i := 0; i < count; i++ {
			pod := podcast(i)
			a.Row(c.Key(pod.ID), pod, width)
		}
	})
}

// podcastRow is a podcast in a list, opening its page.
func (a *Podcast) Row(c *ui.Context, pod *netease.Podcast, width float32) {
	p := *a.Palette
	const pad, gap, cover = 12, 14, 64
	row := ui.ButtonBase(c).Padding(10, pad).Gap(gap).Radius(10).AlignItems(ui.Center).FocusRing(false).Label(pod.Name)
	if row.Hovered() {
		row.Background(p.Hover)
	}
	row.Children(func() {
		a.Cover(c, pod.PicURL, 160, cover, 8)
		ui.Column(c).Grow(1).Shrink(1).MinWidth(0).Gap(4).Children(func() {
			ui.Text(c, pod.Name).FontSize(14).FontWeight(600).SingleLine()
			meta := []string{fmt.Sprintf("%d 期", pod.ProgramCount)}
			if pod.DJ.Nickname != "" {
				meta = append([]string{pod.DJ.Nickname}, meta...)
			}
			ui.Text(c, strings.Join(meta, " · ")).FontSize(12.5).TextColor(p.Muted).SingleLine()
			a.DescriptionSummary(c, pod.Name+" · 简介", pod.Description, width-2*pad-gap-cover, 1)
		})
	})
	if row.Clicked() {
		a.Open(navigation.Route{Page: navigation.Podcast, ID: pod.ID})
	}
}

// ProgramPane is what the episode playing is about, in Now Playing, in
// place of a song's lyrics.
func (a *Podcast) ProgramPane(c *ui.Context, tr *netease.Track, width, height float32) {
	const topPad, rightPad, bottomPad = 4, 12, 48
	column := ui.Scroll(c.Key(tr.ProgramID)).Grow(1).Gap(6).Padding(topPad, rightPad, bottomPad, 0)
	column.Children(func() {
		font := ui.Font{Family: c.Theme().Font, Size: 15, LineHeight: 1.7}
		text := strings.TrimSpace(tr.Description)
		if text == "" {
			text = "这期节目没有简介"
		} else if d := measureDescription(column, text, width-rightPad, font, false); d.height > height-topPad-bottomPad {
			a.DescriptionMore(c, tr.Name+" · 简介", text, true)
		}
		ui.Text(c, text).FontSize(font.Size).LineHeight(font.LineHeight).TextColor(ui.RGB(255, 255, 255).Alpha(0.75))
	})
}

func (a *Podcast) Home(c *ui.Context) {
	t := a.Model.Home
	if !t.HotPage.Loaded && !t.Loading {
		a.Centered(c, t.Error, a.LoadHome)
		return
	}
	a.ScrollState.RestoreScroll(t.HotPage.Loaded)
	top, bottom := a.Insets(c)
	ui.Scroll(c.Key("podcasts")).TrackScroll(&a.ScrollState.PageScroll).Grow(1).Padding(top+shell.PageGap, shell.PagePad, bottom+48, shell.PagePad).ScrollbarInsets(top, 0, bottom, 0).Children(func() {
		a.ScrollState.StartMark(c)
		defer a.ScrollState.EndMark(c)
		a.Heading(c, "播客", "")
		cols := a.Columns(c, 150)
		if !t.HotPage.Loaded {
			a.Section(c, "精选播客").Margin(0, 0, 14, 0)
			a.CardsSkeleton(c, cols, 150, true)
			a.Section(c, "热门播客")
			a.CardsSkeleton(c, cols*2, 150, true)
			return
		}
		// The first list starts right under the heading.
		first := true
		list := func(title string, pods []netease.Podcast) {
			if len(pods) == 0 {
				return
			}
			if s := a.Section(c, title); first {
				s.Margin(0, 0, 14, 0)
			}
			first = false
			ui.Grid(c.Key(title)).Columns(cols).GapX(20).GapY(28).Children(func() {
				for i := range pods {
					a.Card(c.Key(pods[i].ID), &pods[i])
				}
			})
		}
		list("我的订阅", t.Subs)
		list("精选播客", t.Picks)
		list("热门播客", t.Hot)
		a.Foot(c, &t.HotPage, a.LoadHot)
	})
}

// podcastCard is a podcast's cover over its name and, for those the
// listener subscribes to, its newest episode; for the others, why to
// listen.
func (a *Podcast) Card(c *ui.Context, pod *netease.Podcast) {
	sub := pod.LastProgram
	if sub == "" {
		sub = pod.RcmdText
	}
	if sub == "" {
		sub = pod.DJ.Nickname
	}
	id := pod.ID
	if a.CoverCard(c, pod.Name, pod.PicURL, pod.PlayCount, sub, func() { a.PlayPodcast(id) }) {
		a.Open(navigation.Route{Page: navigation.Podcast, ID: id})
	}
}
