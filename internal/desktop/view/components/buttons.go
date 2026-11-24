package components

import (
	"time"

	"github.com/egoist/mygo/ui"
)

// IconButton is a borderless button showing an icon, which a soft circle
// marks under the pointer and which gives a little as it is pressed.
func IconButton(c *ui.Context, icon *ui.SVG, size float32, color ui.Color, label string) ui.Element {
	b := RoundButton(c, size, color, label, false)
	b.Children(func() { ui.Icon(c, icon).Size(size, size) })
	return b
}

// PlayButton is the icon button that plays and pauses, its icon turning
// from one into the other.
func PlayButton(c *ui.Context, playing bool, size float32, color ui.Color) ui.Element {
	b := RoundButton(c, size, color, "播放/暂停", false)
	b.Children(func() { PlayPause(c, b, playing, size) })
	return b
}

// PlayPause draws the icon of b, a button that plays and pauses: as it
// goes from one to the other, the icon going shrinks and fades as the one
// coming grows in, with a little spring.
func PlayPause(c *ui.Context, b ui.Element, playing bool, size float32) {
	to := float32(0)
	if playing {
		to = 1
	}
	v := b.AnimateTo("playing", to, ui.Animation{Spring: ui.SpringOptions{Duration: 340 * time.Millisecond, Bounce: 0.3}})
	ui.Box(c).Size(size, size).Children(func() {
		for _, icon := range []struct {
			svg   *ui.SVG
			shown float32
		}{{IconPlay, 1 - v}, {IconPause, v}} {
			if icon.shown <= 0.01 {
				continue
			}
			s := 0.4 + 0.6*icon.shown
			ui.Icon(c, icon.svg).Absolute().Top(0).Left(0).Size(size, size).Scale(s, s).Opacity(min(icon.shown, 1))
		}
	})
}

// RoundButton is the button of iconButton, without its icon; selected
// controls keep a translucent background even away from the pointer.
func RoundButton(c *ui.Context, size float32, color ui.Color, label string, selected bool) ui.Element {
	b := ui.ButtonBase(c).Size(size+16, size+16).Center().Radius(999).Label(label).TextColor(color)
	bg := ui.Transparent
	if selected {
		bg = color.Alpha(0.18)
	}
	if b.Hovered() {
		bg = ui.RGBA(128, 128, 128, 0.14)
		if selected {
			bg = color.Alpha(0.26)
		}
	}
	b.Background(b.AnimateColor("bg", bg, ui.Animation{Duration: 120 * time.Millisecond}))
	return AnimateButtonPress(b, 0.88)
}

// PillButton is a rounded button with an icon and a label.
func PillButton(c *ui.Context, icon *ui.SVG, label string, bg, fg ui.Color) ui.Element {
	b := ui.ButtonBase(c).Height(36).PaddingX(18).Gap(8).Radius(999).AlignItems(ui.Center).TextColor(fg)
	fill := bg
	if b.Hovered() {
		fill = bg.Mix(ui.RGB(255, 255, 255), 0.12)
	}
	b.Background(b.AnimateColor("bg", fill, ui.Animation{Duration: 120 * time.Millisecond}))
	b.Children(func() {
		if icon != nil {
			ui.Icon(c, icon).Size(14, 14)
		}
		ui.Text(c, label).FontSize(14).FontWeight(600).SingleLine()
	})
	return AnimateButtonPress(b, 0.96)
}

func AnimateButtonPress(b ui.Element, scale float32) ui.Element {
	pose := ui.Pose{}
	if b.Pressed() {
		pose.Scale = scale
	}
	b.AnimatePose(pose, ui.Snappy)
	return b
}
