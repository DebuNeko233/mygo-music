package app

import (
	"time"

	"github.com/egoist/mygo/ui"

	"netease-music-mygo/internal/desktop/components"
)

const coverFadeDuration = 180 * time.Millisecond

type coverFadeState struct {
	current, previous *artwork
	changedAt         time.Time
}

// draw holds the visible cover until the latest selection is ready, then
// blends it over the previous image without exposing the background.
func (state *coverFadeState) draw(c *ui.Context, requested *artwork, hasCover bool, size, radius float32, bg, fg ui.Color) {
	in := float32(1)
	now := c.Now()
	if c.Preferences().ReduceMotion || now.Sub(state.changedAt) >= coverFadeDuration {
		state.previous, state.changedAt = nil, time.Time{}
	}
	// Finish a short fade already on screen, then pick up only the latest
	// selection. While its cover is loading, keep the last visible one.
	if state.changedAt.IsZero() && (!hasCover || requested != nil) {
		if bitmap(requested) == nil {
			requested = nil
		}
		if state.current != requested {
			state.previous, state.current = state.current, requested
			if c.Preferences().ReduceMotion {
				state.previous = nil
			} else {
				state.changedAt = now
			}
		}
	}
	if !state.changedAt.IsZero() {
		t := float32(now.Sub(state.changedAt)) / float32(coverFadeDuration)
		in = t * t * (3 - 2*t)
		c.AnimationFrame()
	}
	if in < 1 || state.current == nil {
		if old := bitmap(state.previous); old != nil {
			// The incoming image covers the old one progressively, so
			// the background never shows through between two covers.
			components.ArtImage(c, old, radius, 1)
		} else {
			components.CoverPlaceholder(c, size, radius, 1, bg, fg)
		}
	}
	if current := bitmap(state.current); current != nil {
		components.ArtImage(c, current, radius, in)
	} else if state.previous != nil {
		components.CoverPlaceholder(c, size, radius, in, bg, fg)
	}
}
