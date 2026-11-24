package shell

import (
	"github.com/egoist/mygo/ui"

	"netease-music-mygo/internal/desktop/theme"
)

const SidebarWidth = 232

// Layout contains this build's feature views, not persistent UI elements.
// BindRoot runs before children so global playback shortcuts precede focused buttons.
type Layout struct {
	Palette                           theme.Palette
	HasBackdrop, Covered              bool
	Navigation                        Navigation
	BindRoot                          func(*ui.Context, ui.Element)
	Backdrop                          func(*ui.Context)
	Sidebar, Page, Player, NowPlaying func(*ui.Context)
}

func (s *State) Build(c *ui.Context, v Layout) {
	p := v.Palette
	if c.Vibrancy() {
		c.Root().Background(ui.Transparent)
	}
	root := ui.Box(c).Fill()
	// The keys the listener gave the actions, Space for play and pause by
	// default, work wherever the focus is: on the container around
	// everything they come before a button the pointer focused, which would
	// take Space as a click, and on the window where nothing has the focus,
	// as once Now Playing covers the button that had it.
	v.BindRoot(c, root)
	root.Children(func() {
		if v.Backdrop != nil {
			v.Backdrop(c)
		}
		page := ui.Row(c).Fill().AlignItems(ui.Stretch)
		page.Children(func() {
			if v.Covered {
				// Covered by Now Playing: not built, which would cost each
				// of its frames as much again as the stage itself. Where
				// it is scrolled lives outside the build, so it shows again
				// as it was.
				return
			}
			side := ui.Column(c).Width(SidebarWidth)
			if !c.Vibrancy() && !v.HasBackdrop {
				side.Background(p.Sidebar)
			}
			side.Children(func() { v.Sidebar(c) })
			// Keyed, so that a popover of the sidebar opening or closing
			// keeps what the page and its bars hold, their edges' fades.
			content := ui.Box(c.Key("main")).Grow(1).BorderWidth(0, 0, 0, 1).BorderColor(p.Hairline)
			if !v.HasBackdrop {
				content.Background(p.Background)
			}
			content.Children(func() {
				v.Page(c)
				// The bars float over the page, which scrolls under them:
				// positioned, they paint over it.
				s.Top(c, p, v.Navigation)
				v.Player(c)
			})
		})
		v.NowPlaying(c)
	})
}
