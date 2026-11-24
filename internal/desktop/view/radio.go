package view

import (
	"strings"
	"time"

	"github.com/egoist/mygo/ui"

	"netease-music-mygo/internal/desktop/view/components"
	"netease-music-mygo/internal/desktop/view/page"
	"netease-music-mygo/internal/desktop/view/shell"
	"netease-music-mygo/internal/netease"
	"netease-music-mygo/internal/player"
	accountvm "netease-music-mygo/internal/viewmodel/account"
	radiovm "netease-music-mygo/internal/viewmodel/radio"
)

type Radio struct {
	*page.Page
	Model                           *radiovm.Radio
	Account                         *accountvm.Account
	Player                          func() *player.Player
	PlayFM, Trash, Exit, StartHeart func()
	Like                            func(*ui.Context, *netease.Track, float32, ui.Color) ui.Element
	ArtistLinks                     func(*ui.Context, *netease.Track, ui.Color, ui.Color, bool) ui.Element
}

// fmPage is the personal FM: what it plays, large, over its cover's
// colors, with the FM's own controls, and heart mode below.
func (a *Radio) Build(c *ui.Context) {
	p := *a.Palette
	top, bottom := a.Insets(c)
	ui.Scroll(c.Key("fm")).TrackScroll(&a.ScrollState.PageScroll).Grow(1).Padding(top+shell.PageGap, shell.PagePad, bottom+48, shell.PagePad).ScrollbarInsets(top, 0, bottom, 0).Children(func() {
		a.ScrollState.StartMark(c)
		defer a.ScrollState.EndMark(c)
		// The mode the FM plays in, if it plays, taken from the radio when the
		// radio changes: taken on every build, it put back the mode the
		// listener had just chosen, since the radio goes on playing the last
		// one until the first songs of the one asked for arrive.
		a.Model.Sync(a.Player().Radio())
		ui.Row(c).AlignItems(ui.End).Children(func() {
			ui.Box(c).Grow(1).Children(func() { a.Heading(c, "私人 FM", "无尽推荐") })
			mode := a.Model.Mode
			ui.Box(c).Margin(0, 0, 22, 0).Children(func() {
				labels := make([]string, len(radiovm.FMModes))
				for i, m := range radiovm.FMModes {
					labels[i] = m.Label
				}
				if components.Segmented(c.Key("fm-mode"), &mode, labels...) {
					a.Model.Mode = mode
					// Playing the FM, it changes to the mode at once.
					if strings.HasPrefix(a.Player().Radio(), "fm:") {
						a.PlayFM()
					}
				}
			})
		})
		a.Card(c)
		a.Section(c, "心动模式")
		a.HeartCard(c, p)
	})
}

// fmCard shows the song the FM plays, or how to start it.
func (a *Radio) Card(c *ui.Context) {
	pl := a.Player()
	onFM := strings.HasPrefix(pl.Radio(), "fm:")
	var tr *netease.Track
	if onFM {
		tr = pl.Current()
	}
	var art components.Image
	if tr != nil {
		art = a.Image(netease.Sized(tr.Album.PicURL, 600), true)
	}
	card := ui.Box(c.Key("fm-card")).Height(320).Radius(24).Background(ui.Hex("#232328")).TextColor(white).
		Shadow(0, 18, 44, -16, a.Palette.CardShade)
	in := components.ArtIn(card, components.Blurred(art))
	card.Children(func() {
		if in < 1 {
			ui.Box(c).Absolute().Top(0).Left(0).Right(0).Bottom(0).Radius(24).
				LinearGradient(ui.LinearGradient{Angle: 135, Oklab: true, From: ui.Oklch(0.42, 0.14, 20), To: ui.Oklch(0.3, 0.1, 300)})
		}
		components.ArtImage(c, components.Blurred(art), 24, in)
		ui.Box(c).Absolute().Top(0).Left(0).Right(0).Bottom(0).Radius(24).
			LinearGradient(ui.LinearGradient{Angle: 90, Stops: []ui.GradientStop{
				{Color: ui.RGBA(0, 0, 0, 0.35), At: 0},
				{Color: ui.RGBA(0, 0, 0, 0.1), At: 1},
			}})
		ui.Row(c).Absolute().Top(0).Left(0).Right(0).Bottom(0).Padding(36, 40).Gap(40).AlignItems(ui.Center).Children(func() {
			a.Covers(c, tr)
			ui.Column(c).Grow(1).Shrink(1).MinWidth(0).Gap(8).Children(func() {
				label := "私人 FM · " + radiovm.FMModes[a.Model.Mode].Label
				if onFM {
					label = radiovm.RadioLabel(pl.Radio())
				}
				ui.Row(c).Gap(6).AlignItems(ui.Center).Children(func() {
					ui.Icon(c, components.IconRadio).Size(14, 14).TextColor(white.Alpha(0.75))
					ui.Text(c, label).FontSize(12).FontWeight(600).TextColor(white.Alpha(0.75))
				})
				if tr == nil {
					ui.Text(c, "听见更多好音乐").FontSize(30).FontWeight(800).LetterSpacing(-0.6)
					ui.Text(c, "根据你的口味，一首接一首，不停推荐").FontSize(13.5).TextColor(white.Alpha(0.75))
					ui.Row(c).Margin(18, 0, 0, 0).Children(func() {
						label := "开始收听"
						if a.Model.Starting != "" && strings.HasPrefix(a.Model.Starting, "fm:") {
							label = "正在调频…"
						}
						if components.PillButton(c.Key("start"), components.IconPlay, label, white, ui.Hex("#1d1d1f")).Clicked() {
							a.PlayFM()
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
					a.ArtistLinks(c, tr, white.Alpha(0.7), white, true).FontSize(14).SingleLine()
				}
				ui.Row(c).Gap(14).AlignItems(ui.Center).Margin(20, 0, 0, -8).Children(func() {
					if a.Account.Profile != nil {
						if components.IconButton(c.Key("trash"), components.IconTrash, 20, white.Alpha(0.8), "不喜欢").Clicked() {
							a.Trash()
						}
					}
					a.Like(c.Key("like"), tr, 20, white.Alpha(0.8))
					play := ui.ButtonBase(c.Key("play")).Size(56, 56).Center().Radius(999).Background(white).TextColor(ui.Hex("#1d1d1f")).Label("播放/暂停")
					pose := ui.Pose{}
					if play.Pressed() {
						pose.Scale = 0.92
					} else if play.Hovered() {
						pose.Scale = 1.05
					}
					play.AnimatePose(pose, ui.Snappy)
					play.Children(func() { components.PlayPause(c, play, pl.Playing(), 22) })
					if play.Clicked() {
						pl.TogglePlay()
					}
					if components.IconButton(c.Key("next"), components.IconNext, 20, white.Alpha(0.8), "下一首").Clicked() {
						pl.Next()
					}
				})
			})
		})
	})
}

// fmCovers stacks the cover playing over the next one's, which peeks out
// behind it, tilted, as the FM's own player shows them.
func (a *Radio) Covers(c *ui.Context, tr *netease.Track) {
	const size = 232
	ui.Box(c).Size(size+36, size).Shrink(0).Children(func() {
		var next *netease.Track
		if tr != nil {
			if order, at := a.Player().PlayOrder(); at >= 0 && at+1 < len(order) {
				next = &a.Player().Queue()[order[at+1]]
			}
		}
		if next != nil {
			back := ui.Box(c.Key(next.ID)).Absolute().Top(16).Left(36).Size(size-32, size-32).Radius(14).Opacity(0.55).Background(white.Alpha(0.12)).
				Shadow(0, 10, 24, -8, ui.RGBA(0, 0, 0, 0.4))
			back.AnimatePose(ui.Pose{Rotate: 6, X: 14}, ui.Animation{Spring: ui.SpringOptions{Duration: 500 * time.Millisecond, Bounce: 0.2}})
			back.Children(func() { a.CoverFill(c, next.Album.PicURL, 400, 14) })
		}
		front := ui.Box(c.Key("front")).Absolute().Top(0).Left(0).Size(size, size).Radius(16).
			Background(white.Alpha(0.12)).Shadow(0, 20, 44, -10, ui.RGBA(0, 0, 0, 0.55))
		if tr == nil {
			front.Center().Children(func() {
				ui.Icon(c, components.IconRadio).Size(64, 64).TextColor(white.Alpha(0.85))
			})
			return
		}
		scale := float32(1)
		if !a.Player().Playing() {
			scale = 0.94
		}
		front.AnimatePose(ui.Pose{Scale: scale}, ui.Animation{Spring: ui.SpringOptions{Duration: 600 * time.Millisecond, Bounce: 0.25}})
		front.Children(func() {
			ui.Box(c.Key(tr.ID)).Fill().Transition(ui.ElementTransition{Animation: &ui.Snappy, Enter: &ui.Motion{Scale: 0.92}}).
				Children(func() { a.CoverFill(c, tr.Album.PicURL, 600, 16) })
		})
	})
}

// HeartCard starts heart mode: songs like those the listener likes.
func (a *Radio) HeartCard(c *ui.Context, p components.Palette) {
	on := a.Player().Radio() == radiovm.HeartRadio
	ui.Row(c).Padding(22, 26).Gap(20).Radius(18).AlignItems(ui.Center).Background(p.Hover).Children(func() {
		ui.Box(c).Size(56, 56).Radius(16).Center().Shrink(0).
			LinearGradient(ui.LinearGradient{Angle: 135, Oklab: true, From: ui.Oklch(0.72, 0.17, 10), To: ui.Oklch(0.58, 0.22, 350)}).
			TextColor(white).Children(func() { ui.Icon(c, components.IconHeartFill).Size(26, 26) })
		ui.Column(c).Grow(1).Shrink(1).MinWidth(0).Gap(4).Children(func() {
			ui.Text(c, "从你喜欢的音乐出发").FontSize(15).FontWeight(700)
			ui.Text(c, "根据你红心过的歌曲，智能推荐风格相似的音乐，与你的喜欢穿插播放").FontSize(12.5).TextColor(p.Muted)
		})
		if on {
			if components.PillButton(c.Key("exit-heart"), components.IconX, "退出心动模式", p.Press.Over(p.Background), p.Text).Clicked() {
				a.Exit()
			}
		}
		label := "开启心动模式"
		switch {
		case a.Account.Profile == nil:
			label = "登录后开启"
		case on:
			label = "正在播放"
		case a.Model.Starting == radiovm.HeartRadio:
			label = "正在开启…"
		}
		if components.PillButton(c.Key("heart"), components.IconHeartFill, label, p.Accent, p.OnAccent).Clicked() {
			a.StartHeart()
		}
	})
}
