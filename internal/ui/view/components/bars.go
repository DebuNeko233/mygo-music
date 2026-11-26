/*
平滑频段电平并绘制均衡器条形，供不同播放视图复用。
*/

package components

import (
	"time"

	"github.com/egoist/mygo/ui"

	"netease-music-mygo/internal/features/player"
)

// BarState keeps the smoothed levels of one view's equalizer between frames.
type BarState struct {
	h    [player.Bands]float32
	last time.Time
}

func (st *BarState) Step(now time.Time, target [player.Bands]float32, live bool) {
	dt := float32(now.Sub(st.last).Seconds())
	if st.last.IsZero() || dt > 0.2 {
		dt = 0.016
	}
	st.last = now
	for i := range st.h {
		tg := target[i]
		if !live {
			tg = 0
		}
		k := float32(18) // rises fast
		if tg < st.h[i] {
			k = 6 // falls slowly
		}
		st.h[i] += (tg - st.h[i]) * min(1, k*dt)
	}
}

// Group returns the mean of bands [from, to).
func (st *BarState) Group(from, to int) float32 {
	var s float32
	for i := from; i < to; i++ {
		s += st.h[i]
	}
	return s / float32(to-from)
}

func DrawBars(p *ui.Painter, r ui.Rect, st *BarState, n int, color ui.Color, minH float32) {
	EachBarRect(r, st, n, minH, func(rect ui.Rect) { p.Fill(rect, color, rect.W/2) })
}

// EachBarRect shares the exact response, spacing and capsule geometry between renderers.
func EachBarRect(r ui.Rect, st *BarState, n int, minH float32, emit func(ui.Rect)) {
	gap := r.W / float32(n) * 0.38
	w := (r.W - gap*float32(n-1)) / float32(n)
	per := player.Bands / n
	for i := range n {
		// Shape the response: low bands are loud already.
		v := st.Group(i*per, (i+1)*per)
		v = v * v * 1.15
		h := max(minH, min(r.H, r.H*v))
		x := r.X + float32(i)*(w+gap)
		emit(ui.Rect{X: x, Y: r.Y + (r.H-h)/2, W: w, H: h})
	}
}
