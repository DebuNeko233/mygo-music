/*
呈现歌单页面与标题，连接共享资料库状态和歌曲列表。
*/

package library

import (
	"fmt"
	"strings"

	"github.com/egoist/mygo/ui"

	"netease-music-mygo/internal/features/navigation"
	"netease-music-mygo/internal/features/netease"
	"netease-music-mygo/internal/ui/view/components"
	"netease-music-mygo/internal/ui/view/page"
	accountvm "netease-music-mygo/internal/ui/viewmodel/account"
	libraryvm "netease-music-mygo/internal/ui/viewmodel/library"
	playbackvm "netease-music-mygo/internal/ui/viewmodel/playback"
	radio "netease-music-mygo/internal/ui/viewmodel/radio"
)

type View struct {
	*page.Page
	Model                                           *libraryvm.Library
	Account                                         *accountvm.Account
	Playback                                        *playbackvm.Playback
	RadioState                                      *radio.Radio
	Load                                            func(int64, bool)
	CurrentIndex                                    func([]netease.Track) int
	SyncViewport                                    func(*ui.Context, *netease.Playlist, int)
	Locate                                          func(*ui.Context, int, bool)
	Play, Shuffle, Download                         func([]netease.Track)
	Savable, Saved                                  func(*netease.Playlist) bool
	Editable                                        func(int64) *netease.Playlist
	ToggleSaved                                     func(netease.Playlist)
	ToggleAlbumSaved                                func(netease.AlbumProfile)
	Radio                                           func() string
	ExitRadio, StartHeart, SubmitName, DeleteChosen func()
	Rename, ConfirmDelete                           func(*netease.Playlist)
	NewPlaylist                                     func(*netease.Track)
	AddTo, RemoveFrom                               func(int64, *netease.Track)
	Open                                            func(navigation.Route)
}

func (a *View) Build(c *ui.Context) {
	id := a.Nav.Current.ID
	pl := a.Model.Playlists[id]
	if pl == nil {
		if !a.Model.Loading[id] || a.Model.Errors[id] != "" {
			a.Centered(c, a.Model.Errors[id], func() { a.Load(id, false) })
			return
		}
		a.Skeleton(c, true, func(c *ui.Context) { a.HeaderSkeleton(c) })
		return
	}
	at := a.CurrentIndex(pl.Tracks)
	a.SyncViewport(c, pl, at)
	visible := a.TrackRows(c, pl.Tracks, true, func(c *ui.Context) { a.Header(c, pl) })
	a.Locate(c, at, visible)
}

func (a *View) Header(c *ui.Context, pl *netease.Playlist) {
	p := *a.Palette
	const coverSize = 212
	ui.Row(c).Gap(page.DetailHeaderGap).AlignItems(ui.End).Padding(0, 0, 26, 0).Children(func() {
		ui.Box(c).Size(coverSize, coverSize).Radius(14).Shadow(0, 18, 40, -10, p.CardShade).Children(func() {
			a.CoverFill(c, pl.Cover(), 600, 14)
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
			a.HeaderDescription(c, pl.Name+" · 简介", pl.Description, coverSize)
			ui.Row(c).Gap(10).Margin(10, 0, 0, 0).Children(func() {
				if components.PillButton(c.Key("play"), components.IconPlay, "播放", p.Accent, p.OnAccent).Clicked() {
					a.Play(pl.Tracks)
				}
				if components.PillButton(c.Key("shuffle"), components.IconShuffle, "随机播放", p.Hover.Over(p.Background), p.Text).Clicked() {
					a.Shuffle(pl.Tracks)
				}
				if a.Savable(pl) {
					icon, label := components.IconPlus, "收藏"
					if a.Saved(pl) {
						icon, label = components.IconCheck, "已收藏"
					}
					if components.PillButton(c.Key("save"), icon, label, p.Hover.Over(p.Background), p.Text).Clicked() {
						a.ToggleSaved(*pl)
					}
				}
				switch {
				case pl.SpecialType != 5:
				case a.Radio() == radio.HeartRadio:
					if components.PillButton(c.Key("heart"), components.IconX, "退出心动模式", p.Hover.Over(p.Background), p.Text).Clicked() {
						a.ExitRadio()
					}
				default:
					label := "心动模式"
					if a.RadioState.Starting == radio.HeartRadio {
						label = "正在开启…"
					}
					if components.PillButton(c.Key("heart"), components.IconHeartFill, label, p.Hover.Over(p.Background), p.Text).Clicked() {
						a.StartHeart()
					}
				}
				{
					mine := a.Editable(pl.ID)
					more := components.IconButton(c.Key("more"), components.IconMore, 18, p.Muted, "更多")
					more.Menu(func(m *ui.Menu) {
						if m.Item("下载全部").Disabled(len(pl.Tracks) == 0).Chosen() {
							a.Download(pl.Tracks)
						}
						if mine == nil {
							return
						}
						m.Separator()
						if m.Item("重命名…").Chosen() {
							a.Rename(mine)
						}
						m.Separator()
						if m.Item("删除歌单…").Chosen() {
							a.ConfirmDelete(mine)
						}
					})
				}
			})
		})
	})
}
