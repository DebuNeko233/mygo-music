package app

import (
	"strings"
	"time"

	"github.com/egoist/mygo/ui"

	"netease-music-mygo/internal/netease"
	"netease-music-mygo/internal/viewmodel"
)

const heartRadio = viewmodel.HeartRadio

var fmModes = viewmodel.FMModes

func fmRadio(mode string) string    { return viewmodel.FMRadio(mode) }
func radioLabel(name string) string { return viewmodel.RadioLabel(name) }

var personalFM = netease.PersonalFM
var fmTrash = netease.FMTrash

func (a *app) radioServices() viewmodel.RadioServices {
	return viewmodel.RadioServices{
		Player: a.pl, Library: &a.library, Post: a.detailPost(),
		Notify: func(message string) { a.toast = message }, NowPlaying: func() { a.playbackState.Open = true },
		PersonalFM: personalFM, Heart: netease.HeartMode, Playlist: netease.PlaylistDetail, Dislike: fmTrash,
	}
}
func (a *app) radioFeed(name string) func(func([]netease.Track)) {
	return a.radio.Feed(name, a.radioServices())
}
func (a *app) playFM()    { a.radio.StartFM(fmRadio(fmModes[a.radio.Mode].Mode), a.radioServices()) }
func (a *app) exitRadio() { a.radio.Exit(a.radioServices()) }
func (a *app) trashFM()   { a.radio.Trash(a.radioServices()) }
func (a *app) startHeartMode() {
	if a.requireLibraryLogin() {
		a.radio.StartHeart(a.radioServices())
	}
}

// fmPage is the personal FM: what it plays, large, over its cover's
// colors, with the FM's own controls, and heart mode below.
func (a *app) fmPage(c *ui.Context) {
	p := a.pal
	top, bottom := a.insets(c)
	ui.Scroll(c.Key("fm")).TrackScroll(&a.pageScroll).Grow(1).Padding(top+pageGap, pagePad, bottom+48, pagePad).ScrollbarInsets(top, 0, bottom, 0).Children(func() {
		a.startMark(c)
		defer a.endMark(c)
		// The mode the FM plays in, if it plays, taken from the radio when the
		// radio changes: taken on every build, it put back the mode the
		// listener had just chosen, since the radio goes on playing the last
		// one until the first songs of the one asked for arrive.
		a.radio.Sync(a.pl.Radio())
		ui.Row(c).AlignItems(ui.End).Children(func() {
			ui.Box(c).Grow(1).Children(func() { a.heading(c, "私人 FM", "无尽推荐") })
			mode := a.radio.Mode
			ui.Box(c).Margin(0, 0, 22, 0).Children(func() {
				labels := make([]string, len(fmModes))
				for i, m := range fmModes {
					labels[i] = m.Label
				}
				if segmented(c.Key("fm-mode"), &mode, labels...) {
					a.radio.Mode = mode
					// Playing the FM, it changes to the mode at once.
					if strings.HasPrefix(a.pl.Radio(), "fm:") {
						a.playFM()
					}
				}
			})
		})
		a.fmCard(c)
		a.section(c, "心动模式")
		a.heartCard(c, p)
	})
}

// fmCard shows the song the FM plays, or how to start it.
func (a *app) fmCard(c *ui.Context) {
	pl := a.pl
	onFM := strings.HasPrefix(pl.Radio(), "fm:")
	var tr *netease.Track
	if onFM {
		tr = pl.Current()
	}
	var art *artwork
	if tr != nil {
		art = a.art.get(netease.Sized(tr.Album.PicURL, 600), true)
	}
	card := ui.Box(c.Key("fm-card")).Height(320).Radius(24).Background(ui.Hex("#232328")).TextColor(white).
		Shadow(0, 18, 44, -16, a.pal.cardShade)
	in := artIn(card, backdrop(art))
	card.Children(func() {
		if in < 1 {
			ui.Box(c).Absolute().Top(0).Left(0).Right(0).Bottom(0).Radius(24).
				LinearGradient(ui.LinearGradient{Angle: 135, Oklab: true, From: ui.Oklch(0.42, 0.14, 20), To: ui.Oklch(0.3, 0.1, 300)})
		}
		artImage(c, backdrop(art), 24, in)
		ui.Box(c).Absolute().Top(0).Left(0).Right(0).Bottom(0).Radius(24).
			LinearGradient(ui.LinearGradient{Angle: 90, Stops: []ui.GradientStop{
				{Color: ui.RGBA(0, 0, 0, 0.35), At: 0},
				{Color: ui.RGBA(0, 0, 0, 0.1), At: 1},
			}})
		ui.Row(c).Absolute().Top(0).Left(0).Right(0).Bottom(0).Padding(36, 40).Gap(40).AlignItems(ui.Center).Children(func() {
			a.fmCovers(c, tr)
			ui.Column(c).Grow(1).Shrink(1).MinWidth(0).Gap(8).Children(func() {
				label := "私人 FM · " + fmModes[a.radio.Mode].Label
				if onFM {
					label = radioLabel(pl.Radio())
				}
				ui.Row(c).Gap(6).AlignItems(ui.Center).Children(func() {
					ui.Icon(c, iconRadio).Size(14, 14).TextColor(white.Alpha(0.75))
					ui.Text(c, label).FontSize(12).FontWeight(600).TextColor(white.Alpha(0.75))
				})
				if tr == nil {
					ui.Text(c, "听见更多好音乐").FontSize(30).FontWeight(800).LetterSpacing(-0.6)
					ui.Text(c, "根据你的口味，一首接一首，不停推荐").FontSize(13.5).TextColor(white.Alpha(0.75))
					ui.Row(c).Margin(18, 0, 0, 0).Children(func() {
						label := "开始收听"
						if a.radio.Starting != "" && strings.HasPrefix(a.radio.Starting, "fm:") {
							label = "正在调频…"
						}
						if pillButton(c.Key("start"), iconPlay, label, white, ui.Hex("#1d1d1f")).Clicked() {
							a.playFM()
						}
					})
					return
				}
				ui.Text(c, tr.Name).FontSize(30).FontWeight(800).LetterSpacing(-0.6).MaxLines(2).LineHeight(1.2)
				switch {
				case pl.Buffering():
					ui.Text(c, "正在缓冲…").FontSize(14).TextColor(white.Alpha(0.7)).SingleLine()
				case pl.Err() != "":
					ui.Text(c, pl.Err()).FontSize(14).TextColor(white).SingleLine()
				default:
					a.artistLinks(c, tr, white.Alpha(0.7), white, true).FontSize(14).SingleLine()
				}
				ui.Row(c).Gap(14).AlignItems(ui.Center).Margin(20, 0, 0, -8).Children(func() {
					if a.account.Profile != nil {
						if iconButton(c.Key("trash"), iconTrash, 20, white.Alpha(0.8), "不喜欢").Clicked() {
							a.trashFM()
						}
					}
					a.likeButton(c.Key("like"), tr, 20, white.Alpha(0.8))
					play := ui.ButtonBase(c.Key("play")).Size(56, 56).Center().Radius(999).Background(white).TextColor(ui.Hex("#1d1d1f")).Label("播放/暂停")
					pose := ui.Pose{}
					if play.Pressed() {
						pose.Scale = 0.92
					} else if play.Hovered() {
						pose.Scale = 1.05
					}
					play.AnimatePose(pose, ui.Snappy)
					play.Children(func() { playPause(c, play, pl.Playing(), 22) })
					if play.Clicked() {
						pl.TogglePlay()
					}
					if iconButton(c.Key("next"), iconNext, 20, white.Alpha(0.8), "下一首").Clicked() {
						pl.Next()
					}
				})
			})
		})
	})
}

// fmCovers stacks the cover playing over the next one's, which peeks out
// behind it, tilted, as the FM's own player shows them.
func (a *app) fmCovers(c *ui.Context, tr *netease.Track) {
	const size = 232
	ui.Box(c).Size(size+36, size).Shrink(0).Children(func() {
		var next *netease.Track
		if tr != nil {
			if order, at := a.pl.PlayOrder(); at >= 0 && at+1 < len(order) {
				next = &a.pl.Queue()[order[at+1]]
			}
		}
		if next != nil {
			back := ui.Box(c.Key(next.ID)).Absolute().Top(16).Left(36).Size(size-32, size-32).Radius(14).Opacity(0.55).Background(white.Alpha(0.12)).
				Shadow(0, 10, 24, -8, ui.RGBA(0, 0, 0, 0.4))
			back.AnimatePose(ui.Pose{Rotate: 6, X: 14}, ui.Animation{Spring: ui.SpringOptions{Duration: 500 * time.Millisecond, Bounce: 0.2}})
			back.Children(func() { a.coverFill(c, next.Album.PicURL, 400, 14) })
		}
		front := ui.Box(c.Key("front")).Absolute().Top(0).Left(0).Size(size, size).Radius(16).
			Background(white.Alpha(0.12)).Shadow(0, 20, 44, -10, ui.RGBA(0, 0, 0, 0.55))
		if tr == nil {
			front.Center().Children(func() {
				ui.Icon(c, iconRadio).Size(64, 64).TextColor(white.Alpha(0.85))
			})
			return
		}
		scale := float32(1)
		if !a.pl.Playing() {
			scale = 0.94
		}
		front.AnimatePose(ui.Pose{Scale: scale}, ui.Animation{Spring: ui.SpringOptions{Duration: 600 * time.Millisecond, Bounce: 0.25}})
		front.Children(func() {
			ui.Box(c.Key(tr.ID)).Fill().Transition(ui.ElementTransition{Animation: &ui.Snappy, Enter: &ui.Motion{Scale: 0.92}}).
				Children(func() { a.coverFill(c, tr.Album.PicURL, 600, 16) })
		})
	})
}

// heartCard starts heart mode: songs like those the listener likes.
func (a *app) heartCard(c *ui.Context, p palette) {
	on := a.pl.Radio() == heartRadio
	ui.Row(c).Padding(22, 26).Gap(20).Radius(18).AlignItems(ui.Center).Background(p.hover).Children(func() {
		ui.Box(c).Size(56, 56).Radius(16).Center().Shrink(0).
			LinearGradient(ui.LinearGradient{Angle: 135, Oklab: true, From: ui.Oklch(0.72, 0.17, 10), To: ui.Oklch(0.58, 0.22, 350)}).
			TextColor(white).Children(func() { ui.Icon(c, iconHeartFill).Size(26, 26) })
		ui.Column(c).Grow(1).Shrink(1).MinWidth(0).Gap(4).Children(func() {
			ui.Text(c, "从你喜欢的音乐出发").FontSize(15).FontWeight(700)
			ui.Text(c, "根据你红心过的歌曲，智能推荐风格相似的音乐，与你的喜欢穿插播放").FontSize(12.5).TextColor(p.muted)
		})
		if on {
			if pillButton(c.Key("exit-heart"), iconX, "退出心动模式", p.press.Over(p.bg), p.text).Clicked() {
				a.exitRadio()
			}
		}
		label := "开启心动模式"
		switch {
		case a.account.Profile == nil:
			label = "登录后开启"
		case on:
			label = "正在播放"
		case a.radio.Starting == heartRadio:
			label = "正在开启…"
		}
		if pillButton(c.Key("heart"), iconHeartFill, label, p.accent, p.onAccent).Clicked() {
			a.startHeartMode()
		}
	})
}
