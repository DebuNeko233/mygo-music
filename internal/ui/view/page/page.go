/*
提供功能页面共用的边距、列布局、标题、空状态与滚动容器。
*/

package page

import (
	"github.com/egoist/mygo/ui"

	"netease-music-mygo/internal/features/navigation"
	"netease-music-mygo/internal/ui/view/components"
	"netease-music-mygo/internal/ui/view/shell"
)

// Page borrows the main window's colors, route and scroll state. It owns no feature data.
type Page struct {
	Renderers
	Palette     *components.Palette
	Nav         *navigation.Navigator
	ScrollState *shell.State
	Offline     func() bool
}

const DetailHeaderGap = 28
const ArtistPhotoSize = 212

func TrackWidth(c *ui.Context) float32 {
	w, _ := c.Size()
	return w - shell.SidebarWidth - 1 - 2*(shell.PagePad-12)
}
func (p *Page) Insets(c *ui.Context) (float32, float32) { return shell.Insets(c) }
func (p *Page) Columns(c *ui.Context, min float32) int  { return shell.Columns(c, min) }
func (p *Page) Heading(c *ui.Context, title, sub string) {
	components.Heading(c, *p.Palette, title, sub)
}
func (p *Page) Section(c *ui.Context, title string) ui.Element { return components.Section(c, title) }
func (p *Page) Centered(c *ui.Context, text string, retry func()) {
	shell.Centered(c, *p.Palette, p.Offline(), text, retry)
}
func (p *Page) Empty(c *ui.Context, text string) {
	ui.Text(c, text).FontSize(13).TextColor(p.Palette.Muted).Padding(40, 0).AlignSelf(ui.Center)
}
func (p *Page) Scroll(c *ui.Context, content func()) {
	p.ScrollReady(c, true, content)
}

// ScrollReady keeps a saved position pending while only placeholders build.
func (p *Page) ScrollReady(c *ui.Context, ready bool, content func()) {
	p.ScrollState.RestoreScroll(ready)
	top, bottom := p.Insets(c)
	ui.Scroll(c.Key(p.Nav.Current)).TrackScroll(&p.ScrollState.PageScroll).Grow(1).Padding(top+shell.PageGap, shell.PagePad-12, bottom+32, shell.PagePad-12).ScrollbarInsets(top, 0, bottom, 0).Children(func() { p.ScrollState.StartMark(c); defer p.ScrollState.EndMark(c); content() })
}
