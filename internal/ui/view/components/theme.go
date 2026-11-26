/*
定义应用配色，并根据明暗外观和用户强调色生成主题颜色。
*/

package components

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

// PaletteFor derives window colors from the appearance and an optional accent color.
func PaletteFor(t *ui.Theme, selected ui.Color) Palette {
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

// Accent is a colour the app can take, named for the settings, in the
// lightness each appearance needs of it.
type Accent struct {
	Name        string
	Dark, Light ui.Color
}

// Accents are the colours the app can take, the first being the one its
// appearance gives.
var Accents = []Accent{
	{"胭脂", ui.Oklch(0.66, 0.2, 22), ui.Oklch(0.6, 0.21, 24)},
	{"柑橘", ui.Oklch(0.68, 0.18, 55), ui.Oklch(0.62, 0.19, 52)},
	{"琥珀", ui.Oklch(0.72, 0.17, 90), ui.Oklch(0.64, 0.17, 88)},
	{"青苔", ui.Oklch(0.68, 0.17, 140), ui.Oklch(0.6, 0.16, 142)},
	{"松石", ui.Oklch(0.7, 0.14, 190), ui.Oklch(0.6, 0.13, 192)},
	{"湖蓝", ui.Oklch(0.68, 0.16, 230), ui.Oklch(0.6, 0.15, 232)},
	{"靛青", ui.Oklch(0.64, 0.19, 265), ui.Oklch(0.56, 0.19, 267)},
	{"紫罗兰", ui.Oklch(0.66, 0.19, 300), ui.Oklch(0.58, 0.18, 302)},
	{"玫瑰", ui.Oklch(0.68, 0.19, 340), ui.Oklch(0.6, 0.18, 342)},
	{"石墨", ui.Oklch(0.6, 0.02, 260), ui.Oklch(0.5, 0.02, 260)},
}

// AccentColor returns the colour called name, or zero for one that names
// none of them.
func AccentColor(name string, dark bool) ui.Color {
	for _, a := range Accents {
		if a.Name != name {
			continue
		}
		if dark {
			return a.Dark
		}
		return a.Light
	}
	return ui.Color{}
}
