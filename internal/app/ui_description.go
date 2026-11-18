package app

import (
	"strings"

	"github.com/egoist/mygo/ui"
)

const (
	detailHeaderGap       = 28
	descriptionSize       = 12.5
	descriptionLineHeight = 1.45
	descriptionLines      = 2
)

type descriptionPreview struct {
	source, text, font string
	width              float32
	truncated          bool
}

type descriptionDialog struct {
	open        bool
	title, text string
	scroll      ui.ScrollState
}

func (a *app) descriptionSummary(c *ui.Context, title, text string, artworkSize float32) {
	if text = strings.TrimSpace(text); text == "" {
		return
	}
	w, _ := c.Size()
	// The header shares the track page's insets and leaves room for its cover.
	width := max(1, w-sidebarWidth-1-2*trackPagePad-artworkSize-detailHeaderGap)
	font := c.Theme().Font
	d := &a.introPreview
	if d.source != text || d.width != width || d.font != font {
		preview := strings.Join(strings.Fields(text), " ")
		shape := ui.ShapeText(preview, ui.Font{Family: font, Size: descriptionSize, LineHeight: descriptionLineHeight}, width)
		*d = descriptionPreview{source: text, text: preview, font: font, width: width, truncated: len(shape.Lines()) > descriptionLines}
	}
	ui.Column(c.Key("description")).Gap(3).Margin(2, 0, 0, 0).Children(func() {
		ui.Text(c, d.text).FontSize(descriptionSize).TextColor(a.pal.muted).MaxLines(descriptionLines).LineHeight(descriptionLineHeight)
		if !d.truncated {
			return
		}
		blue := ui.Hex("#005eb8")
		if a.pal.dark {
			blue = ui.Hex("#78b8ff")
		}
		button := ui.ButtonBase(c.Key("more")).AlignSelf(ui.Start).Padding(2, 0).Cursor(ui.CursorPointer).Label("更多")
		button.Children(func() {
			label := ui.Text(c, "更多").FontSize(descriptionSize).FontWeight(600).TextColor(blue)
			if button.Hovered() {
				label.Underline()
			}
		})
		button.OnClick(func() {
			a.intro = descriptionDialog{open: true, title: title, text: text}
		})
	})
}

func (a *app) descriptionDialog(c *ui.Context) {
	d, p := &a.intro, a.pal
	ui.DialogBase(c.Key("page-description"), &d.open, func(backdrop, panel ui.Element) {
		w, h := c.Size()
		backdrop.Background(ui.RGBA(0, 0, 0, 0.35))
		panel.Width(min(540, w-48)).Padding(16, 24, 24, 24).Radius(18).Gap(12).
			Background(p.bg).Border(1, p.hairline).Shadow(0, 24, 60, -12, ui.RGBA(0, 0, 0, 0.4)).
			Transition(ui.ElementTransition{Animation: &ui.Snappy, Enter: &ui.Motion{Scale: 0.94}, Exit: &ui.Motion{Scale: 0.96}})
		ui.Row(c).AlignItems(ui.Center).Gap(12).Children(func() {
			ui.Text(c, d.title+" · 简介").Grow(1).MinWidth(0).MaxLines(2).FontSize(17).FontWeight(700).TextColor(p.text)
			iconButton(c.Key("close"), iconX, 18, p.muted, "关闭").Shrink(0).OnClick(func() { d.open = false })
		})
		ui.Scroll(c.Key("text")).TrackScroll(&d.scroll).MaxHeight(min(420, h-140)).Padding(0, 10, 0, 0).Children(func() {
			ui.Text(c, d.text).FontSize(14).LineHeight(1.65).TextColor(p.text).Selectable()
		})
	})
}
