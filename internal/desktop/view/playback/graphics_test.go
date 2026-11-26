/*
验证播放页释放闲置图层时保留当前封面和阅读位置，恢复后仍可直接显示。
*/

package playback

import (
	"image"
	"testing"
	"time"

	"github.com/egoist/mygo/ui"

	artcache "netease-music-mygo/internal/desktop/artwork"
)

// Releasing hidden layers must drop their scene graphs and previous-song
// crossfades, while keeping current-cover fallbacks and the lyric scroll.
func TestReleaseGraphicsKeepsCurrentCovers(t *testing.T) {
	a := newTestView(t)
	current := &artcache.Image{Bitmap: ui.NewBitmap(image.NewRGBA(image.Rect(0, 0, 48, 48)))}
	old := &artcache.Image{}
	a.NPArt, a.NPCoverState.Art, a.NPCoverState.Previous = current, current, old
	a.NPCoverState.Switching = true
	a.StageCover = coverFadeState{Current: current, Previous: old, ChangedAt: time.Unix(1, 0)}
	a.WallCover = a.StageCover
	a.BackdropCache = BackdropCache{Canvas: ui.NewCanvas(640, 480), Before: ui.NewCanvas(640, 480), art: old}
	a.BackdropCache.PrepareStill(old, ui.RGB(50, 80, 140), time.Unix(1, 0))
	a.LyricBlurPass = ui.NewCanvas(800, 600)
	a.LyricScroll.Y = 123
	for range 2 {
		a.releaseGraphics()
		if a.BackdropCache != (BackdropCache{}) || a.LyricBlurPass != nil || a.NPCoverState.Previous != nil || a.NPCoverState.Switching || a.StageCover.Previous != nil || a.WallCover.Previous != nil {
			t.Fatal("offscreen layers or previous covers remain retained")
		}
		if a.NPArt != current || a.NPCoverState.Art != current || a.StageCover.Current != current || a.WallCover.Current != current || a.LyricScroll.Y != 123 {
			t.Fatal("release discarded a current cover or reading position")
		}
	}
}
