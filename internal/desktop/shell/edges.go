package shell

import (
	"runtime"
	"time"

	"github.com/egoist/mygo/plugins/glass"
	"github.com/egoist/mygo/ui"

	"netease-music-mygo/internal/desktop/components"
	"netease-music-mygo/internal/desktop/theme"
)

// PlayerBarHeight is the height of the player along the foot of the pages.
const PlayerBarHeight = 84

// PageGap is the room between the bar over a page and its content.
const PageGap = 12

// Insets returns the room the bars over a page take at its top and its
// bottom: pages pad their content by it, and scroll under the bars.
func Insets(c *ui.Context) (top, bottom float32) {
	top = TopBar(c)
	if runtime.GOOS == "darwin" {
		// The edge reaches a little below the traffic lights' strip.
		top += topEdgePad
	}
	return top, PlayerBarHeight
}

// topEdgePad is the room the edge over a page leaves below the strip, on
// macOS.
const topEdgePad = 6

// ButtonsHeight is the height of the window buttons off macOS, which fill
// the edge over a page.
const ButtonsHeight = TopBarHeight + topEdgePad

// TopBar returns the height of the strip along the top of the window, as
// tall as the window buttons and the room above and below them, which
// what sits in it centers on.
func TopBar(c *ui.Context) float32 {
	if runtime.GOOS != "darwin" {
		// The whole edge, which the buttons fill: what sits in the strip
		// centers on their glyphs.
		return ButtonsHeight
	}
	return max(c.TitleBar().Height, TopBarHeight)
}

// TopBarHeight is the least height of the strip along the top.
const TopBarHeight = 40

// StartMark marks where a page's content starts, first in what scrolls:
// once it has moved up, content is under the bar at the top.
func (s *State) StartMark(c *ui.Context) {
	b := ui.Box(c.Key("start-mark")).Height(0).Bounds()
	if b.W > 0 {
		top, _ := Insets(c)
		s.UnderTop = b.Y < top+PageGap-0.5
	}
}

// EndMark marks where a page's content ends, last in what scrolls: while it
// is below the top of the player, content is under the player.
func (s *State) EndMark(c *ui.Context) {
	b := ui.Box(c.Key("end-mark")).Height(0).Bounds()
	if b.W > 0 {
		_, h := c.Size()
		_, bottom := Insets(c)
		s.UnderBottom = b.Y > h-bottom+0.5
	}
}

// EdgeShown returns how much of a scroll edge shows over el: fully while
// content is under its bar, and none at rest, as AppKit shows its edges.
func EdgeShown(el ui.Element, under bool) float32 {
	target := float32(0)
	if under {
		target = 1
	}
	return el.AnimateTo("shown", target, ui.Animation{Duration: 180 * time.Millisecond})
}

// Top is the strip over the top of a page, frosting what scrolls under
// it as macOS 26's hard scroll edge does: it drags the window, and holds
// the back button once there is a page to go back to.
func (s *State) Top(c *ui.Context, p theme.Palette, n Navigation) {
	top, _ := Insets(c)
	edge := ui.Box(c.Key("top-edge")).Absolute().Top(0).Left(0).Right(0).Height(top).DragWindow().
		Material(glass.ScrollEdge{Hard: true, Background: p.Background})
	edge.Opacity(EdgeShown(edge, s.UnderTop))
	if n.CanBack || n.CanForward {
		ui.Row(c).Absolute().Top(0).Left(20).Height(TopBar(c)).AlignItems(ui.Center).Children(func() {
			back := components.IconButton(c.Key("back"), components.IconBack, 18, p.Muted, "返回").Disabled(!n.CanBack)
			if !n.CanBack {
				back.Opacity(0.35)
			}
			if back.Clicked() {
				n.Back()
			}
			if n.CanForward && components.IconButton(c.Key("forward"), components.IconForward, 18, p.Muted, "前进").Clicked() {
				n.Forward()
			}
		})
	}
}
