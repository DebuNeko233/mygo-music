/*
选择并绘制用户背景图片，提供封面来源和首页背景的呈现接口。
*/

package playback

import (
	"github.com/egoist/mygo/ui"

	artcache "netease-music-mygo/internal/desktop/artwork"
)

// DrawPicture paints pic over r, covering it, as opaque as alpha says.
func DrawPicture(p *ui.Painter, r ui.Rect, pic *artcache.Picture, alpha float32) {
	if pic == nil || alpha <= 0 || r.W <= 0 || r.H <= 0 {
		return
	}
	p.ImageRegion(pic.Bitmap, CoverSource(r, pic.W, pic.H), r, ui.ImageOptions{Tint: White.Alpha(alpha)})
}

// CoverSource returns the part of a w×h picture that lands in r when the
// picture covers r, centered.
func CoverSource(r ui.Rect, w, h float32) ui.Rect {
	if w <= 0 || h <= 0 || r.W <= 0 || r.H <= 0 {
		return ui.Rect{}
	}
	s := max(r.W/w, r.H/h)
	return ui.Rect{X: w/2 - r.W/(2*s), Y: h/2 - r.H/(2*s), W: r.W / s, H: r.H / s}
}

func (a *View) HomeBackdrop(c *ui.Context, pic *artcache.Picture, opacity float32) {
	ui.Box(c.Key("backdrop")).Absolute().Top(0).Left(0).Right(0).Bottom(0).PassThrough().Draw(func(p *ui.Painter, r ui.Rect) { DrawPicture(p, r, pic, opacity) })
}
