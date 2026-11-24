package app

import (
	"fmt"
	"strings"
	"time"

	"github.com/egoist/mygo/ui"

	"netease-music-mygo/internal/desktop/components"
	homeview "netease-music-mygo/internal/desktop/home"
	"netease-music-mygo/internal/desktop/shell"
	"netease-music-mygo/internal/netease"
	"netease-music-mygo/internal/player"
)

const pagePad = shell.PagePad

const trackPagePad = pagePad - 12

func trackPageWidth(c *ui.Context) float32 {
	w, _ := c.Size()
	return w - sidebarWidth - 1 - 2*trackPagePad // the content's left border also takes one DIP
}

func (a *app) columns(c *ui.Context, min float32) int         { return shell.Columns(c, min) }
func (a *app) heading(c *ui.Context, title, sub string)       { components.Heading(c, a.pal, title, sub) }
func (a *app) section(c *ui.Context, title string) ui.Element { return components.Section(c, title) }
func (a *app) showHome(c *ui.Context, charts bool) {
	v := homeview.View{Model: &a.home, Palette: a.pal, Offline: a.network.offline,
		Retry: a.loadHome, DailyCard: a.dailyCard, PlaylistCard: a.playlistCard, ChartCard: a.chartCard,
		HomeSkeleton:      func(c *ui.Context) { a.homeSkeleton(c, a.pageShimmer(c)) },
		ChartsSkeleton:    func(c *ui.Context) { a.chartCardsSkeleton(c, a.pageShimmer(c)) },
		PlaylistsSkeleton: func(c *ui.Context) { a.playlistCardsSkeleton(c, a.pageShimmer(c), shell.Columns(c, 150)*2, 150, true) },
	}
	if a.account.Profile != nil {
		v.Nickname = a.account.Profile.Nickname
	}
	if charts {
		v.Charts(c, &a.shell)
	} else {
		v.Home(c, &a.shell)
	}
}
func (a *app) homePage(c *ui.Context)   { a.showHome(c, false) }
func (a *app) chartsPage(c *ui.Context) { a.showHome(c, true) }
func weekday(t time.Time) string        { return homeview.Weekday(t) }

// playlistCard is a square cover with the playlist's name below: it lifts
// under the pointer, where a play button shows on it.
func (a *app) playlistCard(c *ui.Context, pl *netease.Playlist) {
	if a.coverCard(c, pl.Name, pl.Cover(), pl.PlayCount, pl.UpdateFreq, func() { a.loadPlaylist(pl.ID, true) }) {
		a.open(route{Page: pagePlaylist, ID: pl.ID})
	}
}

// coverCard is a playlist's card, or a podcast's: its cover, with how many
// times it was played on it, over its name and a line under that. The
// button on the cover calls play; it reports whether the rest was clicked.
func (a *app) coverCard(c *ui.Context, name, cover string, playCount float64, sub string, play func()) bool {
	p := a.pal
	// From the top: a grid stretches the cards of a row as tall as the
	// tallest, whose name or line under it takes two lines.
	card := ui.ButtonBase(c).Column().Gap(10).AlignItems(ui.Stretch).Justify(ui.Start).FocusRing(false).Label(name)
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
		art.Children(func() {
			a.coverFill(c, cover, 400, 10)
			if playCount > 0 {
				ui.Row(c).Absolute().Top(8).Right(8).Padding(2, 7).Radius(999).Gap(3).AlignItems(ui.Center).
					Background(ui.RGBA(0, 0, 0, 0.35)).TextColor(ui.RGB(255, 255, 255)).Children(func() {
					ui.Icon(c, components.IconHeadset).Size(10, 10)
					ui.Text(c, components.Plays(playCount)).FontSize(10.5).FontWeight(600)
				})
			}
			a.coverPlayButton(c, hovered, play)
		})
		ui.Text(c, name).FontSize(13).FontWeight(500).MaxLines(2).LineHeight(1.35)
		if sub != "" {
			ui.Text(c, sub).FontSize(12).TextColor(p.Muted).SingleLine().Margin(-6, 0, 0, 0)
		}
	})
	return card.Clicked()
}

// coverFill fills its parent with a cover, its corners rounded by radius.
// The cover rounds itself rather than its parent clipping it: a clip moved
// by a transform, as a card lifting under the pointer, is drawn on the CPU.
func (a *app) coverFill(c *ui.Context, url string, px int, radius float32) {
	box := ui.Box(c).Absolute().Top(0).Left(0).Right(0).Bottom(0).Radius(radius)
	bmp := bitmap(a.art.getInView(netease.Sized(url, px), false, box))
	in := components.ArtIn(box, bmp)
	if in < 1 {
		box.Background(a.pal.Hover)
	}
	box.Children(func() { components.ArtImage(c, bmp, radius, in) })
}

// coverPlayButton plays a playlist or a podcast from its card, without
// opening it.
func (a *app) coverPlayButton(c *ui.Context, shown bool, play func()) {
	b := ui.ButtonBase(c.Key("play")).Absolute().Bottom(10).Right(10).Size(38, 38).Radius(999).Center().
		Background(a.pal.Accent).TextColor(ui.RGB(255, 255, 255)).Label("播放").FocusRing(false).
		Shadow(0, 4, 12, 0, ui.RGBA(0, 0, 0, 0.3))
	pose := ui.Pose{Fade: 1, Scale: 0.8, Y: 6}
	if shown || b.Hovered() {
		pose = ui.Pose{Scale: 1}
		if b.Hovered() {
			pose.Scale = 1.08
		}
	}
	b.AnimatePose(pose, ui.Snappy)
	b.Children(func() { ui.Icon(c, components.IconPlay).Size(15, 15).Margin(0, 0, 0, 2) })
	if b.Clicked() {
		play()
	}
}

// chartCard shows a chart with its top three songs beside its cover.
func (a *app) chartCard(c *ui.Context, pl *netease.Playlist) {
	p := a.pal
	card := ui.ButtonBase(c).Gap(14).Padding(12).Radius(14).AlignItems(ui.Center).FocusRing(false).Label(pl.Name)
	hovered := card.Hovered()
	bg := p.Hover
	if hovered {
		bg = p.Press
	}
	card.Background(card.AnimateColor("bg", bg, ui.Animation{Duration: 140 * time.Millisecond}))
	card.Children(func() {
		ui.Box(c).Size(84, 84).Radius(9).Shadow(0, 3, 10, -2, p.CardShade).Children(func() {
			a.coverFill(c, pl.Cover(), 200, 9)
		})
		ui.Column(c).Grow(1).Shrink(1).MinWidth(0).Gap(5).Children(func() {
			ui.Row(c).Gap(6).AlignItems(ui.Baseline).Children(func() {
				ui.Text(c, pl.Name).FontSize(14).FontWeight(700).SingleLine()
				ui.Text(c, pl.UpdateFreq).FontSize(11).TextColor(p.Faint).SingleLine()
			})
			for i, line := range pl.Preview {
				ui.Row(c).Gap(7).Children(func() {
					ui.Textf(c, "%d", i+1).FontSize(12).FontWeight(700).Width(10).FontFeatures("tnum").TextColor(p.Muted)
					ui.RichText(c,
						ui.Span{Text: line.First, Weight: 500},
						ui.Span{Text: "  " + line.Second, Color: p.Muted},
					).FontSize(12).SingleLine().Shrink(1).MinWidth(0)
				})
			}
		})
	})
	if card.Clicked() {
		a.open(route{Page: pagePlaylist, ID: pl.ID})
	}
}

func (a *app) playlistPage(c *ui.Context) {
	id := a.nav.Current.ID
	pl := a.library.Playlists[id]
	if pl == nil {
		if !a.library.Loading[id] || a.library.Errors[id] != "" {
			a.centered(c, a.library.Errors[id], func() { a.loadPlaylist(id, false) })
			return
		}
		s := a.pageShimmer(c)
		a.trackSkeleton(c, s, true, func(c *ui.Context) { a.playlistHeaderSkeleton(c, s) })
		return
	}
	at := a.currentTrackIndex(pl.Tracks)
	a.syncPlaylistViewport(c, pl, at)
	visible := a.trackRows(c, pl.Tracks, true, func(c *ui.Context) { a.playlistHeader(c, pl) })
	a.trackLocateButton(c, at, visible)
}

func (a *app) playlistHeader(c *ui.Context, pl *netease.Playlist) {
	p := a.pal
	const coverSize = 212
	ui.Row(c).Gap(detailHeaderGap).AlignItems(ui.End).Padding(0, 0, 26, 0).Children(func() {
		ui.Box(c).Size(coverSize, coverSize).Radius(14).Shadow(0, 18, 40, -10, p.CardShade).Children(func() {
			a.coverFill(c, pl.Cover(), 600, 14)
		})
		ui.Column(c).Grow(1).Shrink(1).MinWidth(0).Gap(8).Children(func() {
			kind := "歌单"
			if pl.UpdateFreq != "" {
				kind = "榜单 · " + pl.UpdateFreq
			}
			ui.Text(c, kind).FontSize(12).FontWeight(600).TextColor(p.Accent)
			ui.Text(c, pl.Name).FontSize(30).FontWeight(800).LetterSpacing(-0.6).MaxLines(2).LineHeight(1.2)
			var meta []string
			if pl.Creator.Nickname != "" {
				meta = append(meta, pl.Creator.Nickname)
			}
			meta = append(meta, fmt.Sprintf("%d 首", len(pl.Tracks)))
			if pl.PlayCount > 0 {
				meta = append(meta, components.Plays(pl.PlayCount)+" 次播放")
			}
			ui.Text(c, strings.Join(meta, " · ")).FontSize(13).TextColor(p.Muted)
			a.headerDescription(c, pl.Name+" · 简介", pl.Description, coverSize)
			ui.Row(c).Gap(10).Margin(10, 0, 0, 0).Children(func() {
				if components.PillButton(c.Key("play"), components.IconPlay, "播放", p.Accent, p.OnAccent).Clicked() {
					if a.pl.Shuffle() {
						a.pl.ToggleShuffle()
					}
					a.pl.PlayQueueFrom(pl.Tracks, firstPlayable(pl.Tracks), a.playSource())
				}
				if components.PillButton(c.Key("shuffle"), components.IconShuffle, "随机播放", p.Hover.Over(p.Background), p.Text).Clicked() {
					a.pl.PlayShuffledFrom(pl.Tracks, a.playSource())
				}
				if a.savable(pl) {
					icon, label := components.IconPlus, "收藏"
					if a.isSaved(pl) {
						icon, label = components.IconCheck, "已收藏"
					}
					if components.PillButton(c.Key("save"), icon, label, p.Hover.Over(p.Background), p.Text).Clicked() {
						a.toggleSaved(*pl)
					}
				}
				switch {
				case pl.SpecialType != 5:
				case a.pl.Radio() == heartRadio:
					if components.PillButton(c.Key("heart"), components.IconX, "退出心动模式", p.Hover.Over(p.Background), p.Text).Clicked() {
						a.exitRadio()
					}
				default:
					label := "心动模式"
					if a.radio.Starting == heartRadio {
						label = "正在开启…"
					}
					if components.PillButton(c.Key("heart"), components.IconHeartFill, label, p.Hover.Over(p.Background), p.Text).Clicked() {
						a.startHeartMode()
					}
				}
				{
					mine := a.editable(pl.ID)
					more := components.IconButton(c.Key("more"), components.IconMore, 18, p.Muted, "更多")
					more.Menu(func(m *ui.Menu) {
						if m.Item("下载全部").Disabled(len(pl.Tracks) == 0).Chosen() {
							a.enqueueDownloads(pl.Tracks)
						}
						if mine == nil {
							return
						}
						m.Separator()
						if m.Item("重命名…").Chosen() {
							a.startRename(mine)
						}
						m.Separator()
						if m.Item("删除歌单…").Chosen() {
							a.confirmDelete(mine)
						}
					})
				}
			})
		})
	})
}

// trackRows lists tracks under a header, which scrolls with them; a click
// plays the list from the track clicked.
func (a *app) trackRows(c *ui.Context, tracks []netease.Track, covers bool, header func(c *ui.Context), footer ...func(c *ui.Context)) (playingVisible bool) {
	return a.trackRowsMeta(c, tracks, covers, nil, header, footer...)
}

// trackRowsMeta lists tracks as trackRows does, saying meta(i) of the track
// at i where it would say how long it plays, as the recent plays say when and
// where the listener played it. meta is asked only for the rows in view.
func (a *app) trackRowsMeta(c *ui.Context, tracks []netease.Track, covers bool, meta func(i int) string, header func(c *ui.Context), footer ...func(c *ui.Context)) (playingVisible bool) {
	p := a.pal
	cur := a.pl.Current()
	// A song's menu can take it out of the playlist it is listed in.
	var pid int64
	if a.nav.Current.Page == pagePlaylist {
		pid = a.nav.Current.ID
	}
	w, _ := c.Size()
	// An album's page has no column of the album its songs are on.
	wide := w-sidebarWidth > 820 && a.nav.Current.Page != pageAlbum
	top, bottom := a.insets(c)
	// Rows out of view are not built, their marks with them: a list
	// scrolled away from its start or end has content under the bars.
	a.shell.UnderTop, a.shell.UnderBottom = true, true
	rows := len(tracks) + 1 + len(footer)
	list := ui.List(c.Key(a.nav.Current), &a.trackList, rows)
	if pid != 0 || a.nav.Current.Page == pageCloud || a.nav.Current.Page == pageLocal {
		list.ItemKey(a.trackRowKeys(tracks))
	}
	list.Rows(func(item ui.ListRow) {
		i := item.Index
		if i == 0 {
			ui.Column(c).Children(func() {
				a.startMark(c)
				header(c)
			})
			return
		}
		if i == rows-1 {
			defer a.endMark(c)
		}
		if i > len(tracks) {
			footer[i-len(tracks)-1](c)
			return
		}
		i--
		tr := &tracks[i]
		missing := tr.IsLocal() && a.local.Missing[tr.LocalID]
		playable := tr.Playable()
		playing := cur != nil && cur.Key() == tr.Key()
		row := ui.ButtonBase(c.Key(tr.Key())).Height(54).PaddingX(12).Gap(14).Radius(10).AlignItems(ui.Center).FocusRing(false).Label(tr.Name)
		if a.nav.Current.Page == pageLocal {
			row.ContextMenu(a.localSongItems(tr, true))
		} else {
			row.ContextMenu(a.songItems(tr, pid))
		}
		if playing {
			// The bars overlay the list. Even a partially visible row counts,
			// but rows wholly covered by either bar do not.
			playingVisible = playingVisible || row.InViewMargin(-top, 0, -bottom, 0)
		}
		hovered := row.Hovered()
		bg := ui.Transparent
		switch {
		case playing:
			bg = p.Accent.Alpha(0.09)
		case hovered:
			bg = p.Hover
		}
		rowBG := row.AnimateColor("bg", bg, ui.Animation{Duration: 100 * time.Millisecond})
		row.Background(rowBG)
		if !playable {
			row.Opacity(0.42)
		}
		row.Children(func() {
			ui.Box(c).Width(26).Center().Children(func() {
				switch {
				case playing && !missing:
					a.nowPlayingBars(c, rowBG.Over(p.Background))
				case hovered && playable && !missing:
					ui.Icon(c, components.IconPlay).Size(13, 13).TextColor(p.Text)
				default:
					ui.Textf(c, "%d", i+1).FontSize(13).FontFeatures("tnum").TextColor(p.Faint)
				}
			})
			if covers {
				a.trackCover(c, tr, 96, 38, 6)
			}
			ui.Column(c).Grow(3).Basis(0).MinWidth(0).Gap(3).Children(func() {
				name := p.Text
				if playing {
					name = p.Accent
				}
				spans := []ui.Span{{Text: tr.Name, Weight: 500, Color: name}}
				if len(tr.Alias) > 0 {
					spans = append(spans, ui.Span{Text: "（" + tr.Alias[0] + "）", Color: p.Faint})
				}
				ui.RichText(c, spans...).FontSize(14).SingleLine()
				ui.Row(c).Gap(6).AlignItems(ui.Center).Children(func() {
					if missing {
						ui.Text(c, "文件已移动或不可访问").FontSize(12.5).TextColor(p.Muted).SingleLine()
						return
					}
					if !playable {
						ui.Text(c, "VIP").FontSize(9).FontWeight(800).TextColor(p.Accent).Padding(0, 4).Radius(3).Border(1, p.Accent.Alpha(0.6))
					}
					a.artistLinks(c, tr, p.Muted, p.Text).FontSize(12.5).SingleLine().Shrink(1).MinWidth(0)
					if tr.Reason != "" {
						ui.Text(c, tr.Reason).FontSize(10.5).FontWeight(600).TextColor(p.Accent).Padding(1, 6).Radius(999).Background(p.Accent.Alpha(0.1)).SingleLine()
					}
				})
			})
			if wide {
				album := ui.RichText(c).Grow(2).Basis(0).MinWidth(0).FontSize(12.5).TextColor(p.Muted).SingleLine()
				album.Children(func() {
					if tr.Album.ID == 0 {
						ui.Text(c, tr.Album.Name)
					} else {
						a.link(c.Key("album"), tr.Album.Name, p.Muted, p.Text, route{Page: pageAlbum, ID: tr.Album.ID})
					}
				})
			}
			// A liked song's heart shows; the others' as the pointer is on them.
			if missing {
				ui.Button(c.Key("relink"), "重新定位").Disabled(a.local.Busy).OnClick(func() { a.chooseLocalFile(tr.LocalID) })
			} else {
				ui.Box(c).Size(32, 32).Center().Children(func() {
					if !tr.IsLocal() && (a.library.Liked[tr.ID] || hovered) {
						a.likeButton(c.Key("like"), tr, 15, p.Muted)
					}
				})
			}
			if meta != nil {
				ui.Text(c, meta(i)).FontSize(12.5).FontFeatures("tnum").TextColor(p.Faint).MinWidth(88).TextAlign(ui.End)
			} else {
				ui.Text(c, components.Clock(tr.Length())).FontSize(12.5).FontFeatures("tnum").TextColor(p.Faint).Width(44).TextAlign(ui.End)
			}
		})
		if row.Clicked() {
			if missing {
				a.chooseLocalFile(tr.LocalID)
			} else if playable {
				a.playTrackQueue(tracks, i)
			}
		}
	}).Grow(1).Padding(top+pageGap, trackPagePad, bottom+32, trackPagePad).ScrollbarInsets(top, 0, bottom, 0)
	if a.nav.Current.Page == pagePlaylist || a.nav.Current.Page == pageCloud || a.nav.Current.Page == pageLocal {
		list.TrackScroll(&a.shell.PageScroll)
	}
	return playingVisible
}

// macOS draws row meters in a native view; other platforms use the painter.
var setupNativeRowBars func(*app)

var nativeRowBars func(*app, *ui.Context, ui.Color) bool

// nowPlayingBars dance in the row of the track playing.
func (a *app) nowPlayingBars(c *ui.Context, background ...ui.Color) {
	if nativeRowBars != nil && len(background) == 1 && nativeRowBars(a, c, background[0]) {
		return
	}
	color := a.pal.Accent
	ui.Box(c).Size(16, 14).Draw(func(p *ui.Painter, r ui.Rect) {
		lv, live := a.pl.Levels()
		a.rowBars.Step(p.Now(), lv, live)
		components.DrawBars(p, r, &a.rowBars, 4, color, 2.5)
		if live || a.rowBars.Group(0, player.Bands) > 0.01 {
			p.AnimationFrame()
		}
	})
}
