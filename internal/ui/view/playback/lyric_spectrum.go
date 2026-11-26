/*
平滑歌词频谱轮廓，协调播放、暂停、跳转及封面色调过渡。
*/

package playback

import (
	"math"
	"time"

	"github.com/egoist/mygo/ui"

	"netease-music-mygo/internal/features/netease"
	"netease-music-mygo/internal/features/player"
	"netease-music-mygo/internal/ui/view/components"
)

const (
	LyricSpectrumSilent = 0.006
	spectrumSeekFade    = 70 * time.Millisecond
	spectrumPauseFade   = 180 * time.Millisecond
	SpectrumBlendTime   = 200 * time.Millisecond
)

type spectrumPhase uint8

const (
	SpectrumFollowing spectrumPhase = iota
	spectrumPausing
	spectrumResuming
	SpectrumSeeking
)

type spectrumBlend struct {
	Phase            spectrumPhase
	since, samplesAt time.Time
	lightFrom        float32
	from, target     [player.DetailBands]float32
}

type LyricSpectrum struct {
	Levels, Contour      [player.DetailBands]float32
	At                   time.Time
	Peak                 float32
	Track                *netease.Track
	WantsPlay            bool
	seekRevision         uint64
	Blend                spectrumBlend
	tintFrom, tintTarget ui.Color
	tintSince            time.Time
	Renderer             spectrumRenderer
}

func (a *View) lyricSpectrumLayer(c *ui.Context, tr *netease.Track, tint ui.Color, height float32) {
	st := &a.NPSpectrum
	if !a.Playback.Open || (a.Playback.Pane != PaneLyrics && a.Playback.Pane != PaneStage) || tr.ProgramID != 0 ||
		!a.Config.LyricSpectrum || c.Preferences().ReduceMotion || c.WindowHidden() {
		*st = LyricSpectrum{}
		return
	}
	layerHeight, opacity, peakLight := min(height*0.216, 208), float32(0.16), float32(0.75)
	if a.Playback.Pane == PaneStage {
		layerHeight, opacity, peakLight = min(height*0.35, 340), 0.31, 1
	}
	tint = tint.Mix(ui.RGB(112, 112, 200), 0.45).Mix(White, 0.07)
	ui.Box(c.Key("lyric-spectrum")).Absolute().Left(0).Right(0).Bottom(0).
		Height(layerHeight + lyricSpectrumGlowY).PassThrough().Draw(func(p *ui.Painter, r ui.Rect) {
		pl := a.Player()
		now := p.Now()
		changed := now.After(st.At)
		wantsPlay := pl.Playing()
		st.UpdateTransport(now, tr, wantsPlay, pl.SeekRevision())
		animating := wantsPlay && pl.Ready()
		if changed && (animating || st.Peak > LyricSpectrumSilent) {
			sample, live := pl.DetailedLevels()
			st.Step(now, sample, live)
		}
		color := st.BlendTint(now, tint)
		if st.Peak > LyricSpectrumSilent {
			fade := spectrumEase((st.Peak - LyricSpectrumSilent) / (0.10 - LyricSpectrumSilent))
			light := color.Mix(ui.RGB(211, 202, 255), 0.72).Alpha(peakLight * fade * st.Blend.Light(now))
			st.Renderer.draw(p, r, layerHeight, st.Contour[:], st.Peak, color.Alpha(opacity*fade), light, changed)
		}
		if animating || st.Peak > LyricSpectrumSilent {
			p.AnimationFrame()
		} else if !wantsPlay {
			st.Blend.Phase = SpectrumFollowing
		}
	})
}

func (st *LyricSpectrum) UpdateTransport(now time.Time, track *netease.Track, wantsPlay bool, revision uint64) {
	switch {
	case revision != st.seekRevision && !st.At.IsZero() && (wantsPlay || st.Peak > LyricSpectrumSilent):
		st.BeginBlend(now, SpectrumSeeking)
	case track != st.Track && st.Blend.Phase != SpectrumFollowing:
		// A new track can overtake a pause or resume while its audio loads.
		st.BeginBlend(now, SpectrumSeeking)
	case track == st.Track && wantsPlay != st.WantsPlay:
		if !wantsPlay {
			st.BeginBlend(now, spectrumPausing)
		} else if st.Blend.Phase != SpectrumSeeking {
			st.BeginBlend(now, spectrumResuming)
		}
	}
	st.Track, st.WantsPlay, st.seekRevision = track, wantsPlay, revision
}

func (st *LyricSpectrum) BeginBlend(now time.Time, phase spectrumPhase) {
	light := float32(0)
	if st.Peak > LyricSpectrumSilent {
		light = st.Blend.Light(now)
	}
	st.Blend.Phase, st.Blend.since = phase, now
	st.Blend.samplesAt, st.Blend.lightFrom = time.Time{}, light
}

// Let the new contour take shape before restoring its halo.
func (b *spectrumBlend) Light(now time.Time) float32 {
	switch b.Phase {
	case SpectrumFollowing:
		return 1
	case spectrumPausing:
		return b.fadeLight(now, spectrumPauseFade)
	case spectrumResuming:
		if b.samplesAt.IsZero() {
			return b.fadeLight(now, spectrumPauseFade)
		}
		return b.lightFrom + (1-b.lightFrom)*b.incomingLight(now)
	default: // Seeking.
		out := b.fadeLight(now, spectrumSeekFade)
		if b.samplesAt.IsZero() {
			return out
		}
		return out + (1-out)*b.incomingLight(now)
	}
}

func (b *spectrumBlend) fadeLight(now time.Time, duration time.Duration) float32 {
	return b.lightFrom * (1 - spectrumEase(float32(now.Sub(b.since))/float32(duration)))
}

func (b *spectrumBlend) incomingLight(now time.Time) float32 {
	return spectrumEase(float32(now.Sub(b.samplesAt)-spectrumSeekFade) / float32(SpectrumBlendTime-spectrumSeekFade))
}

// BlendTint follows the backdrop's crossfade. A new cover can arrive
// during a fade, so start from the color already on screen.
func (st *LyricSpectrum) BlendTint(now time.Time, target ui.Color) ui.Color {
	color := st.tintTarget
	if st.tintSince.IsZero() {
		color = target
	} else if t := float32(now.Sub(st.tintSince)) / float32(BackdropFade); t < 1 {
		color = st.tintFrom.Mix(st.tintTarget, easeOutCubic(max(0, t)))
	}
	if target != st.tintTarget {
		st.tintFrom, st.tintTarget, st.tintSince = color, target, now
	}
	return color
}

func (st *LyricSpectrum) Step(now time.Time, sample [player.DetailBands]float32, live bool) {
	// Keep the attack and release in seconds as the display changes rate.
	dt := 1.0 / 120
	if !st.At.IsZero() {
		dt = min(0.2, now.Sub(st.At).Seconds())
	}
	st.At = now
	b := &st.Blend
	if b.Phase == SpectrumSeeking || b.Phase == spectrumResuming {
		if b.samplesAt.IsZero() && live {
			if b.Phase == spectrumResuming {
				b.lightFrom = b.Light(now)
			}
			b.samplesAt = now
			b.from, b.target = st.Levels, sample
		}
		if !b.samplesAt.IsZero() {
			followSpectrum(&b.target, &sample, dt, live)
			mix := spectrumEase(float32(now.Sub(b.samplesAt)) / float32(SpectrumBlendTime))
			if mix == 1 {
				st.Levels = b.target
				b.Phase = SpectrumFollowing
			} else {
				for i, from := range b.from {
					st.Levels[i] = from + (b.target[i]-from)*mix
				}
			}
			st.SmoothContour()
			return
		}
	}
	followSpectrum(&st.Levels, &sample, dt, live)
	st.SmoothContour()
}

func followSpectrum(levels, sample *[player.DetailBands]float32, dt float64, live bool) {
	release := 4.5
	if !live {
		release = 3.8
	}
	rise, fall := float32(1-math.Exp(-18*dt)), float32(1-math.Exp(-release*dt))
	for i, value := range levels {
		speed := rise
		if sample[i] < value {
			speed = fall
		}
		levels[i] += (sample[i] - value) * speed
	}
}

// Two passes over neighboring bands round isolated spikes. Keep a little
// of the measured contour so their position is still visible.
func (st *LyricSpectrum) SmoothContour() {
	var scratch [player.DetailBands]float32
	for i := range scratch {
		scratch[i] = spectrumAverage(&st.Levels, i)
	}
	st.Peak = 0
	for i, v := range st.Levels {
		avg := spectrumAverage(&scratch, i)
		st.Contour[i] = avg + 0.18*(v-avg)
		st.Peak = max(st.Peak, st.Contour[i])
	}
}

func spectrumAverage(src *[player.DetailBands]float32, i int) float32 {
	last := len(src) - 1
	return (src[max(0, i-1)] + src[i] + src[min(last, i+1)]) / 3
}

func spectrumEase(t float32) float32 {
	t = components.Clamp01(t)
	return t * t * (3 - 2*t)
}
