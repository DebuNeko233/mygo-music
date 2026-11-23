package app

import (
	"math"
	"time"

	"github.com/egoist/mygo/ui"

	"netease-music-mygo/internal/desktop/components"
	"netease-music-mygo/internal/netease"
)

// likeCurrent likes the song or episode playing, or unlikes it, from the
// keyboard (⌘D), which shows no heart: a toast says which it did.
func (a *app) likeCurrent() {
	tr := a.pl.Current()
	if tr == nil || tr.IsLocal() {
		return
	}
	a.toggleLike(tr)
	switch {
	case a.account.Profile == nil:
		return // asked to sign in
	case !a.isLiked(tr):
		a.toast = "已取消喜欢"
	case tr.ProgramID != 0:
		a.toast = "已喜欢这期节目"
	default:
		a.toast = "已添加到我喜欢的音乐"
	}
}

// setProgramLiked says the listener likes an episode, or doesn't, over
// what NetEase may still answer of it.
// askProgramLikes asks NetEase whether the listener likes the episodes
// ids, those not asked about yet: NetEase lists no episodes they like.
// An episode it doesn't answer of shows not liked, and isn't asked again.
// programLikesAnswered takes NetEase's answer of whether uid likes the
// episodes asked about.
// likeButton is a heart that likes tr: filled in red once liked. Liking
// pops it in on a spring while a ring swells around it and confetti flies
// out; unliking lets it give a little.
func (a *app) likeButton(c *ui.Context, tr *netease.Track, size float32, color ui.Color) ui.Element {
	if tr.IsLocal() || tr.Cloud {
		return ui.Element{}
	}
	if tr.ProgramID != 0 {
		a.askProgramLikes([]int64{tr.ProgramID})
	}
	liked := a.isLiked(tr)
	icon, label := components.IconHeart, "喜欢"
	if liked {
		icon, color, label = components.IconHeartFill, a.pal.Accent, "取消喜欢"
	}
	// The label names the song or the episode too: a page shows several
	// hearts, and which one is which has to be said.
	if tr.Name != "" {
		label += " " + tr.Name
	}
	// The burst goes on a wrapper, which the heart's pop does not scale.
	wrap := ui.Box(c).Center()
	var b ui.Element
	wrap.Children(func() { b = components.IconButton(c, icon, size, color, label) })
	scale := b.AnimateTo("pop", 1, ui.Animation{Spring: ui.SpringOptions{Duration: 550 * time.Millisecond, Bounce: 0.55}})
	b.Scale(scale, scale)
	burst := ui.Local(wrap, "burst", func() time.Time { return time.Time{} })
	if b.Clicked() {
		switch {
		case a.account.Profile == nil:
		case liked:
			b.Jump("pop", 0.8)
		default:
			b.Jump("pop", 0.2)
			*burst = c.Now()
		}
		a.toggleLike(tr)
	}
	if !burst.IsZero() {
		start, accent := *burst, a.pal.Accent
		wrap.DrawOver(func(p *ui.Painter, r ui.Rect) {
			if drawBurst(p, r, size, p.Now().Sub(start), accent) {
				p.AnimationFrame()
			}
		})
	}
	return wrap
}

// burstColors are the confetti's colors, around the heart's red.
var burstColors = []ui.Color{
	ui.Hex("#ff4d6d"), ui.Hex("#ff9f1c"), ui.Hex("#ffd166"),
	ui.Hex("#c77dff"), ui.Hex("#4cc9f0"), ui.Hex("#ff70a6"),
}

const burstTime = 750 * time.Millisecond

// drawBurst draws the burst of a heart of size in r, at elapsed since the
// like, and reports whether it goes on.
func drawBurst(p *ui.Painter, r ui.Rect, size float32, elapsed time.Duration, accent ui.Color) bool {
	t := float32(elapsed) / float32(burstTime)
	if t >= 1 || t < 0 {
		return false
	}
	cx, cy := r.X+r.W/2, r.Y+r.H/2
	out := func(x float32) float32 { return 1 - (1-x)*(1-x)*(1-x) } // ease out

	// The ring swells from the heart and thins away, in the first half.
	if rt := t * 2; rt < 1 {
		radius := size * (0.35 + 0.65*out(rt))
		width := size * 0.22 * (1 - rt)
		ring := (&ui.Path{}).Circle(cx, cy, radius)
		p.StrokePath(ring, width, accent.Alpha(0.9*(1-rt)))
	}

	// Two rings of confetti fly out, slowing, shrinking and fading; the
	// inner one a little behind and turned, as if thrown at once.
	for ring := range 2 {
		lag := float32(ring) * 0.08
		pt := (t - 0.12 - lag) / (0.88 - lag)
		if pt <= 0 {
			continue
		}
		pt = min(pt, 1)
		const n = 7
		dist := size * (0.55 + (1.15-0.25*float32(ring))*out(pt))
		dot := size * (0.16 - 0.04*float32(ring)) * (1 - pt*pt)
		alpha := 1 - pt*pt
		for i := range n {
			angle := (float64(i)/n + float64(ring)*0.5/n - 0.25) * 2 * math.Pi
			x := cx + dist*float32(math.Cos(angle))
			y := cy + dist*float32(math.Sin(angle))
			color := burstColors[(i+ring*3)%len(burstColors)].Alpha(alpha)
			p.Fill(ui.Rect{X: x - dot, Y: y - dot, W: 2 * dot, H: 2 * dot}, color, dot)
		}
	}
	return true
}
