package app

import (
	"time"

	"github.com/egoist/mygo/ui"
)

const barCoverFade = 180 * time.Millisecond

type barCoverState struct {
	current, previous *artwork
	changedAt         time.Time
}

func (a *app) playerBarArtwork(c *ui.Context, url string) {
	const size, radius = 48, 8
	p := a.pal
	requested := a.playbackArtwork(url, barCoverRequest)
	state := &a.barCover
	in := float32(1)
	now := c.Now()
	if c.Preferences().ReduceMotion || now.Sub(state.changedAt) >= barCoverFade {
		state.previous, state.changedAt = nil, time.Time{}
	}
	// Finish a short fade already on screen, then pick up only the latest
	// selection. While its cover is loading, keep the last visible one.
	if state.changedAt.IsZero() && (url == "" || requested != nil) {
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
		t := float32(now.Sub(state.changedAt)) / float32(barCoverFade)
		in = t * t * (3 - 2*t)
		c.AnimationFrame()
	}
	ui.Box(c.Key("bar-cover")).Size(size, size).Radius(radius).Shadow(0, 2, 8, 0, p.cardShade).Children(func() {
		if in < 1 || state.current == nil {
			if old := bitmap(state.previous); old != nil {
				// The incoming image covers the old one progressively, so
				// the background never shows through between two covers.
				artImage(c, old, radius, 1)
			} else {
				a.barCoverPlaceholder(c, size, radius, 1)
			}
		}
		if current := bitmap(state.current); current != nil {
			artImage(c, current, radius, in)
		} else if state.previous != nil {
			a.barCoverPlaceholder(c, size, radius, in)
		}
	})
}

func (a *app) barCoverPlaceholder(c *ui.Context, size, radius, opacity float32) {
	if opacity <= 0 {
		return
	}
	ui.Box(c).Absolute().Top(0).Left(0).Right(0).Bottom(0).Radius(radius).
		Background(a.pal.hover.Over(a.pal.bg)).Center().Opacity(opacity).Children(func() {
		ui.Icon(c, iconNote).Size(size*0.3, size*0.3).TextColor(a.pal.faint)
	})
}
