/*
保留当前可见封面，待最新图片就绪后平滑混合，避免露出背景。
*/

package playback

import (
	"time"

	"github.com/egoist/mygo/ui"

	"netease-music-mygo/internal/ui/view/components"
)

const CoverFadeDuration = 180 * time.Millisecond

type coverFadeState struct {
	Current, Previous components.Image
	ChangedAt         time.Time
}

// draw holds the visible cover until the latest selection is ready, then
// blends it over the previous image without exposing the background.
func (state *coverFadeState) draw(c *ui.Context, requested components.Image, hasCover bool, size, radius float32, bg, fg ui.Color) {
	in := float32(1)
	now := c.Now()
	if c.Preferences().ReduceMotion || now.Sub(state.ChangedAt) >= CoverFadeDuration {
		state.Previous, state.ChangedAt = nil, time.Time{}
	}
	// Finish a short fade already on screen, then pick up only the latest
	// selection. While its cover is loading, keep the last visible one.
	if state.ChangedAt.IsZero() && (!hasCover || requested != nil) {
		if components.Bitmap(requested) == nil {
			requested = nil
		}
		if state.Current != requested {
			state.Previous, state.Current = state.Current, requested
			if c.Preferences().ReduceMotion {
				state.Previous = nil
			} else {
				state.ChangedAt = now
			}
		}
	}
	if !state.ChangedAt.IsZero() {
		t := float32(now.Sub(state.ChangedAt)) / float32(CoverFadeDuration)
		in = t * t * (3 - 2*t)
		c.AnimationFrame()
	}
	if in < 1 || state.Current == nil {
		if old := components.Bitmap(state.Previous); old != nil {
			// The incoming image covers the old one progressively, so
			// the background never shows through between two covers.
			components.ArtImage(c, old, radius, 1)
		} else {
			components.CoverPlaceholder(c, size, radius, 1, bg, fg)
		}
	}
	if current := components.Bitmap(state.Current); current != nil {
		components.ArtImage(c, current, radius, in)
	} else if state.Previous != nil {
		components.CoverPlaceholder(c, size, radius, in, bg, fg)
	}
}
