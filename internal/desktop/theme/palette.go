package theme

import (
	"github.com/egoist/mygo/ui"
)

// Palette holds the app's colors, for the light and the dark appearance.
type Palette struct {
	Dark       bool
	Background ui.Color // the content's background
	Sidebar    ui.Color // under the sidebar where no material shows
	Text       ui.Color
	Muted      ui.Color
	Faint      ui.Color
	Hairline   ui.Color
	Hover      ui.Color
	Press      ui.Color
	Accent     ui.Color
	OnAccent   ui.Color
	Warn       ui.Color // what went wrong, and what the app refused to do
	CardShade  ui.Color
}

// For derives window colors from the appearance and an optional accent color.
func For(t *ui.Theme, selected ui.Color) Palette {
	p := Palette{
		Background: ui.Hex("#ffffff"),
		Sidebar:    ui.Hex("#f4f4f6"),
		Text:       ui.Hex("#1d1d1f"),
		Muted:      ui.RGBA(60, 60, 67, 0.62),
		Faint:      ui.RGBA(60, 60, 67, 0.32),
		Hairline:   ui.RGBA(0, 0, 0, 0.07),
		Hover:      ui.RGBA(0, 0, 0, 0.04),
		Press:      ui.RGBA(0, 0, 0, 0.08),
		Accent:     ui.Oklch(0.6, 0.21, 24),
		OnAccent:   ui.Hex("#ffffff"),
		Warn:       ui.Hex("#d70015"),
		CardShade:  ui.RGBA(0, 0, 0, 0.16),
	}
	if t.Dark {
		p = Palette{
			Dark:       true,
			Background: ui.Hex("#141416"),
			Sidebar:    ui.Hex("#1c1c1f"),
			Text:       ui.Hex("#f5f5f7"),
			Muted:      ui.RGBA(235, 235, 245, 0.56),
			Faint:      ui.RGBA(235, 235, 245, 0.3),
			Hairline:   ui.RGBA(255, 255, 255, 0.07),
			Hover:      ui.RGBA(255, 255, 255, 0.06),
			Press:      ui.RGBA(255, 255, 255, 0.1),
			Accent:     ui.Oklch(0.66, 0.2, 22),
			OnAccent:   ui.Hex("#ffffff"),
			Warn:       ui.Hex("#ff6961"),
			CardShade:  ui.RGBA(0, 0, 0, 0.45),
		}
	}
	// The accent the listener chose, in the lightness of this appearance.
	if c := selected; c != (ui.Color{}) {
		p.Accent = c
	}
	return p
}

func (p Palette) ApplyAccent(t *ui.Theme) {
	accent := t.WithAccent(p.Accent)
	t.Accent, t.AccentText = p.Accent, p.OnAccent
	t.AccentHover, t.AccentPressed = accent.AccentHover, accent.AccentPressed
}
