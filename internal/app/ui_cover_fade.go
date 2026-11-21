package app

import (
	"time"

	"github.com/egoist/mygo/ui"
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
			artImage(c, old, radius, 1)
		} else {
			coverPlaceholder(c, size, radius, 1, bg, fg)
		}
	}
	if current := bitmap(state.current); current != nil {
		artImage(c, current, radius, in)
	} else if state.previous != nil {
		coverPlaceholder(c, size, radius, in, bg, fg)
	}
}

func coverPlaceholder(c *ui.Context, size, radius, opacity float32, bg, fg ui.Color) {
	if opacity <= 0 {
		return
	}
	ui.Box(c).Absolute().Top(0).Left(0).Right(0).Bottom(0).Radius(radius).
		Background(bg).Center().Opacity(opacity).Children(func() {
		ui.Icon(c, iconNote).Size(size*0.3, size*0.3).TextColor(fg)
	})
}
