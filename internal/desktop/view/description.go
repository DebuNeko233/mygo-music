/*
测量并呈现简介摘要，提供随宽度变化的展开入口与全文对话框。
*/

package view

import (
	"strings"

	"github.com/egoist/mygo/ui"

	"netease-music-mygo/internal/desktop/view/components"
	"netease-music-mygo/internal/desktop/view/page"
)

const descriptionSize = 12.5
const descriptionLineHeight = 1.45

type descriptionLayout struct {
	source, text  string
	font          ui.Font
	width, height float32
	lines         int
}

type Description struct {
	*page.Page
	Open        bool
	Title, Text string
	Scroll      ui.ScrollState
}

// measureDescription retains the layout for each preview, including rows that
// appear beside other descriptions in the same view.
func measureDescription(element ui.Element, source string, width float32, font ui.Font, flatten bool) *descriptionLayout {
	d := ui.Local(element, "description-layout", func() descriptionLayout { return descriptionLayout{} })
	width = max(1, width)
	if d.source != source || d.width != width || d.font != font {
		text := source
		if flatten {
			text = strings.Join(strings.Fields(source), " ")
		}
		shape := ui.ShapeText(text, font, width)
		_, height := shape.Size()
		*d = descriptionLayout{source: source, text: text, font: font, width: width, height: height, lines: len(shape.Lines())}
	}
	return d
}

func (a *Description) Header(c *ui.Context, title, text string, artworkSize float32) {
	a.Summary(c, title, text, page.TrackWidth(c)-artworkSize-page.DetailHeaderGap, 2).Margin(2, 0, 0, 0)
}

func (a *Description) Summary(c *ui.Context, title, text string, width float32, maxLines int) ui.Element {
	if text = strings.TrimSpace(text); text == "" {
		return ui.Element{}
	}
	column := ui.Column(c.Key("description")).Gap(3)
	font := ui.Font{Family: c.Theme().Font, Size: descriptionSize, LineHeight: descriptionLineHeight}
	d := measureDescription(column, text, width, font, true)
	return column.Children(func() {
		ui.Text(c, d.text).FontSize(font.Size).TextColor(a.Palette.Muted).MaxLines(maxLines).LineHeight(font.LineHeight)
		if d.lines > maxLines {
			a.More(c, title, text, a.Palette.Dark)
		}
	})
}

func (a *Description) More(c *ui.Context, title, text string, dark bool) {
	blue := ui.Hex("#005eb8")
	if dark {
		blue = ui.Hex("#78b8ff")
	}
	button := ui.ButtonBase(c.Key("more-description")).AlignSelf(ui.Start).Padding(2, 0).Cursor(ui.CursorPointer).Label("更多：" + title)
	button.Children(func() {
		label := ui.Text(c, "更多").FontSize(descriptionSize).FontWeight(600).TextColor(blue)
		if button.Hovered() {
			label.Underline()
		}
	})
	button.OnClick(func() {
		a.Open, a.Title, a.Text, a.Scroll = true, title, text, ui.ScrollState{}
	})
}

func (a *Description) Dialog(c *ui.Context) {
	d, p := a, *a.Palette
	ui.DialogBase(c.Key("page-description"), &d.Open, func(backdrop, panel ui.Element) {
		w, h := c.Size()
		backdrop.Background(ui.RGBA(0, 0, 0, 0.35))
		panel.Width(min(540, w-48)).Padding(16, 24, 24, 24).Radius(18).Gap(12).
			Background(p.Background).Border(1, p.Hairline).Shadow(0, 24, 60, -12, ui.RGBA(0, 0, 0, 0.4)).
			Transition(ui.ElementTransition{Animation: &ui.Snappy, Enter: &ui.Motion{Scale: 0.94}, Exit: &ui.Motion{Scale: 0.96}})
		ui.Row(c).AlignItems(ui.Center).Gap(12).Children(func() {
			ui.Text(c, d.Title).Grow(1).MinWidth(0).MaxLines(2).FontSize(17).FontWeight(700).TextColor(p.Text)
			components.IconButton(c.Key("close"), components.IconX, 18, p.Muted, "关闭").Shrink(0).OnClick(func() { d.Open = false })
		})
		ui.Scroll(c.Key("text")).TrackScroll(&d.Scroll).MaxHeight(min(420, h-140)).Padding(0, 10, 0, 0).Children(func() {
			ui.Text(c, d.Text).FontSize(14).LineHeight(1.65).TextColor(p.Text).Selectable()
		})
	})
}
