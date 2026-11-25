package app

import (
	"github.com/egoist/mygo/ui"
	"netease-music-mygo/internal/desktop/view/components"
	"time"
)

func (a *app) localFoldersView(c *ui.Context) {
	t := a.settings.uiTheme(c.Theme())
	a.views.local.Folders(c, t, paletteFor(&t))
}

func (a *app) coverImage(c *ui.Context, size, radius float32, load func(ui.Element) *artwork) ui.Element {
	return a.views.artwork.Image(c, size, radius, func(el ui.Element) *ui.Bitmap { return bitmap(load(el)) })
}

func (a *app) recordKey(kind keyKind, name string, event ui.InputEvent) bool {
	return a.views.settings.RecordKey(string(kind), name, event)
}

func count(n int) string                              { return components.Count(n) }
func commentTime(t, now time.Time) string             { return components.CommentTime(t, now) }
func hsv(r, g, b float64) (float64, float64, float64) { return components.HSV(r, g, b) }
