package app

import (
	"math"
	"time"

	"github.com/egoist/mygo/ui"

	"netease-music-mygo/internal/netease"
	"netease-music-mygo/internal/player"
)

const (
	lyricSpectrumSilent = 0.006
	spectrumSeekFade    = 70 * time.Millisecond
	spectrumSeekBlend   = 200 * time.Millisecond
)

type lyricSpectrum struct {
	levels, contour      [player.DetailBands]float32
	at                   time.Time
	peak                 float32
	seekRevision         uint64
	seekAt, seekBlendAt  time.Time
	seekLightFrom        float32
	seekFrom, seekTarget [player.DetailBands]float32
	tintFrom, tintTarget ui.Color
	tintSince            time.Time
	renderer             spectrumRenderer
}

func (a *app) lyricSpectrumLayer(c *ui.Context, tr *netease.Track, tint ui.Color, height float32) {
	st := &a.npSpectrum
	if !a.nowPlaying || (a.npPane != paneLyrics && a.npPane != paneStage) || tr.ProgramID != 0 ||
		!a.settings.LyricSpectrum || c.Preferences().ReduceMotion || c.WindowHidden() {
		*st = lyricSpectrum{}
		return
	}
	layerHeight, opacity, peakLight := min(height*0.216, 208), float32(0.16), float32(0.75)
	if a.npPane == paneStage {
		layerHeight, opacity, peakLight = min(height*0.35, 340), 0.31, 1
	}
	tint = tint.Mix(ui.RGB(112, 112, 200), 0.45).Mix(white, 0.07)
	ui.Box(c.Key("lyric-spectrum")).Absolute().Left(0).Right(0).Bottom(0).
		Height(layerHeight + lyricSpectrumGlowY).PassThrough().Draw(func(p *ui.Painter, r ui.Rect) {
		pl := a.pl
		now := p.Now()
		changed := now.After(st.at)
		playing := pl.Playing() && pl.Ready()
		revision := pl.SeekRevision()
		if revision != st.seekRevision {
			st.seekRevision = revision
			if !st.at.IsZero() && (playing || st.peak > lyricSpectrumSilent) {
				st.beginSeek(now)
			}
		}
		if changed && (playing || st.peak > lyricSpectrumSilent) {
			sample, live := pl.DetailedLevels()
			st.step(now, sample, live)
		}
		color := st.blendTint(now, tint)
		if st.peak > lyricSpectrumSilent {
			fade := spectrumEase((st.peak - lyricSpectrumSilent) / (0.10 - lyricSpectrumSilent))
			light := color.Mix(ui.RGB(211, 202, 255), 0.72).Alpha(peakLight * fade * st.seekLight(now))
			st.renderer.draw(p, r, layerHeight, st.contour[:], st.peak, color.Alpha(opacity*fade), light, changed)
		}
		if playing || st.peak > lyricSpectrumSilent {
			p.AnimationFrame()
		} else {
			st.seekAt, st.seekBlendAt = time.Time{}, time.Time{}
		}
	})
}

func (st *lyricSpectrum) beginSeek(now time.Time) {
	st.seekLightFrom = st.seekLight(now)
	st.seekAt, st.seekBlendAt = now, time.Time{}
}

// The old halo fades even if the destination is still buffering. The
// new halo returns in the latter part of the blend to the new samples.
func (st *lyricSpectrum) seekLight(now time.Time) float32 {
	if st.seekAt.IsZero() {
		return 1
	}
	out := st.seekLightFrom * (1 - spectrumEase(float32(now.Sub(st.seekAt))/float32(spectrumSeekFade)))
	if st.seekBlendAt.IsZero() {
		return out
	}
	in := spectrumEase(float32(now.Sub(st.seekBlendAt)-spectrumSeekFade) / float32(spectrumSeekBlend-spectrumSeekFade))
	return out + (1-out)*in
}

// blendTint follows the backdrop's crossfade. A new cover can arrive
// during a fade, so start from the color already on screen.
func (st *lyricSpectrum) blendTint(now time.Time, target ui.Color) ui.Color {
	color := st.tintTarget
	if st.tintSince.IsZero() {
		color = target
	} else if t := float32(now.Sub(st.tintSince)) / float32(backdropFade); t < 1 {
		color = st.tintFrom.Mix(st.tintTarget, easeOutCubic(max(0, t)))
	}
	if target != st.tintTarget {
		st.tintFrom, st.tintTarget, st.tintSince = color, target, now
	}
	return color
}

func (st *lyricSpectrum) step(now time.Time, sample [player.DetailBands]float32, live bool) {
	// Keep the attack and release in seconds as the display changes rate.
	dt := 1.0 / 120
	if !st.at.IsZero() {
		dt = min(0.2, now.Sub(st.at).Seconds())
	}
	st.at = now
	if !st.seekAt.IsZero() {
		if st.seekBlendAt.IsZero() && live {
			st.seekBlendAt = now
			st.seekFrom, st.seekTarget = st.levels, sample
		}
		if !st.seekBlendAt.IsZero() {
			followSpectrum(&st.seekTarget, &sample, dt, live)
			mix := spectrumEase(float32(now.Sub(st.seekBlendAt)) / float32(spectrumSeekBlend))
			if mix == 1 {
				st.levels = st.seekTarget
				st.seekAt, st.seekBlendAt = time.Time{}, time.Time{}
			} else {
				for i, from := range st.seekFrom {
					st.levels[i] = from + (st.seekTarget[i]-from)*mix
				}
			}
			st.smoothContour()
			return
		}
	}
	followSpectrum(&st.levels, &sample, dt, live)
	st.smoothContour()
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
func (st *lyricSpectrum) smoothContour() {
	var scratch [player.DetailBands]float32
	for i := range scratch {
		scratch[i] = spectrumAverage(&st.levels, i)
	}
	st.peak = 0
	for i, v := range st.levels {
		avg := spectrumAverage(&scratch, i)
		st.contour[i] = avg + 0.18*(v-avg)
		st.peak = max(st.peak, st.contour[i])
	}
}

func spectrumAverage(src *[player.DetailBands]float32, i int) float32 {
	last := len(src) - 1
	return (src[max(0, i-1)] + src[i] + src[min(last, i+1)]) / 3
}

func spectrumEase(t float32) float32 {
	t = clamp01(t)
	return t * t * (3 - 2*t)
}
