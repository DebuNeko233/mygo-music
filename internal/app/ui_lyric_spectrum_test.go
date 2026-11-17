package app

import (
	"image"
	"image/png"
	"math"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/egoist/mygo/ui"

	"netease-music-mygo/internal/lyrics"
	"netease-music-mygo/internal/netease"
	"netease-music-mygo/internal/player"
)

// The spectrum fills to the bottom and strong peaks light the area outside the contour.
func TestLyricSpectrumMaskFillsToBottom(t *testing.T) {
	dst := image.NewRGBA(image.Rect(0, 0, 241, 80))
	var bands [player.DetailBands]float32
	bands[0], bands[player.DetailBands-1] = .85, .35
	tint := ui.RGB(80, 100, 150)
	light := ui.RGB(192, 184, 255)
	const scale = .6
	const height = 100
	glow := make([]spectrumGlow, dst.Bounds().Dx())
	spectrumGlowColumns(glow, bands[:], .85, 400)
	paintLyricSpectrumFill(dst, bands[:], .85, height, scale, glow, tint, light.Alpha(0))

	edgeAt := func(x int) int {
		for y := range dst.Bounds().Dy() {
			if dst.RGBAAt(x, y).A >= 128 {
				return y
			}
		}
		return dst.Bounds().Dy()
	}
	if left, mid, right := edgeAt(0), edgeAt(120), edgeAt(240); !(left < right && right < mid) {
		t.Errorf("edge heights at left, middle and right = %d, %d, %d", left, mid, right)
	}
	for _, x := range []int{0, 120, 240} {
		edge := edgeAt(x)
		if above, middle, below := dst.RGBAAt(x, edge-5).A, dst.RGBAAt(x, edge).A, dst.RGBAAt(x, edge+5).A; above != 0 || middle == 255 || below != 255 {
			t.Errorf("column %d does not feather into a solid fill: %d, %d, %d", x, above, middle, below)
		}
	}
	for x := range dst.Bounds().Dx() {
		if dst.RGBAAt(x, 79).A != 255 {
			t.Fatalf("column %d did not fill to the bottom", x)
		}
	}
	edge := edgeAt(0)
	floor := dst.RGBAAt(0, 79)
	paintLyricSpectrumFill(dst, bands[:], .85, height, scale, glow, tint, light)
	if above, beside, away := dst.RGBAAt(0, edge-14).A, dst.RGBAAt(14, edge-14).A, dst.RGBAAt(100, edge-14).A; above == 0 || beside == 0 || beside >= above || away != 0 {
		t.Errorf("glow outside a strong peak = %d above, %d beside, %d far away", above, beside, away)
	}
	outside := dst.RGBAAt(14, edge-14)
	if dst.RGBAAt(0, 79) != floor {
		t.Error("peak lighting reached the bottom of the spectrum")
	}
	paintLyricSpectrumFill(dst, bands[:], .85, height, scale, glow, tint.Alpha(.22), light)
	if dst.RGBAAt(14, edge-14) != outside {
		t.Error("the outside glow changed with the page's fill opacity")
	}
	clear(bands[:])
	clear(glow)
	paintLyricSpectrumFill(dst, bands[:], 0, height, scale, glow, tint, light)
	if dst.RGBAAt(0, 40).A != 0 {
		t.Error("a previous peak leaked into the next mask")
	}
}

// A seek keeps the old contour while the light falls away and blends to the first destination samples.
func TestLyricSpectrumSeekBlend(t *testing.T) {
	start := time.Unix(10, 0)
	st := lyricSpectrum{at: start}
	st.levels[6] = .9
	st.smoothContour()
	st.beginBlend(start, spectrumSeeking)
	st.step(start.Add(40*time.Millisecond), [player.DetailBands]float32{}, false)
	if light := st.blend.light(st.at); light <= 0 || light >= 1 || st.levels[6] <= .6 {
		t.Fatalf("after seeking, light = %g and outgoing level = %g", light, st.levels[6])
	}

	arrival := start.Add(90 * time.Millisecond)
	var incoming [player.DetailBands]float32
	incoming[42] = .85
	before := st.levels
	st.step(arrival, incoming, true)
	if st.levels != before || st.blend.light(arrival) != 0 {
		t.Fatal("the first destination sample jumped the contour or relit the old peak")
	}
	half := arrival.Add(spectrumBlendTime / 2)
	st.step(half, incoming, true)
	if got := st.levels[42]; got <= 0 || got >= incoming[42] || st.blend.light(half) <= 0 || st.blend.light(half) >= 1 {
		t.Fatalf("mid-blend incoming level = %g, light = %g", got, st.blend.light(half))
	}

	// Seeking again restarts from what is visible, even mid-blend.
	before, light := st.levels, st.blend.light(half)
	st.beginBlend(half, spectrumSeeking)
	if st.levels != before || st.blend.light(half) != light {
		t.Fatal("a second seek jumped the displayed spectrum")
	}
	arrival = half.Add(90 * time.Millisecond)
	st.step(arrival, incoming, true)
	st.step(arrival.Add(spectrumBlendTime), incoming, true)
	if st.levels != incoming || st.blend.light(st.at) != 1 || st.blend.phase != spectrumFollowing {
		t.Fatal("the second blend did not settle on the destination samples")
	}
}

// A long pause must not make the first resumed audio frame jump to full height and brightness.
func TestLyricSpectrumResumesWithNewSamples(t *testing.T) {
	start := time.Unix(10, 0)
	track := &netease.Track{ID: 7}
	var incoming, silence [player.DetailBands]float32
	for i := 16; i < 24; i++ {
		incoming[i] = .95
	}
	st := lyricSpectrum{track: track, at: start, wantsPlay: true, levels: incoming}
	st.smoothContour()
	st.updateTransport(start, track, false, 0)
	for elapsed := 20 * time.Millisecond; elapsed <= 2*time.Second; elapsed += 20 * time.Millisecond {
		st.step(start.Add(elapsed), silence, false)
	}
	if st.peak > lyricSpectrumSilent || st.blend.light(st.at) != 0 {
		t.Fatal("the paused spectrum and light did not settle")
	}

	resume := start.Add(10 * time.Second)
	st.updateTransport(resume, track, true, 0)
	st.step(resume, silence, false)
	if st.peak > lyricSpectrumSilent || st.blend.light(resume) != 0 {
		t.Fatal("the spectrum lit up while waiting for resumed audio")
	}
	first := resume.Add(350 * time.Millisecond)
	before := st.levels
	st.step(first, incoming, true)
	if st.levels != before || st.blend.light(first) != 0 {
		t.Fatal("the first resumed audio sample jumped the contour or flashed")
	}
	mid := first.Add(spectrumBlendTime / 2)
	st.step(mid, incoming, true)
	if st.levels[20] <= before[20] || st.levels[20] >= incoming[20] || st.blend.light(mid) <= 0 || st.blend.light(mid) >= 1 {
		t.Fatal("the contour and highlight did not ease back in")
	}
	st.step(first.Add(spectrumBlendTime), incoming, true)
	if st.levels != incoming || st.blend.light(st.at) != 1 || st.blend.phase != spectrumFollowing {
		t.Fatal("the spectrum did not resume normal playback")
	}
}

// Rapid pause and play reversals continue from the last displayed contour and light.
func TestLyricSpectrumRapidPauseResume(t *testing.T) {
	start := time.Unix(10, 0)
	track := &netease.Track{ID: 7}
	var incoming, silence [player.DetailBands]float32
	for i := 16; i < 24; i++ {
		incoming[i] = .95
	}
	st := lyricSpectrum{track: track, at: start, wantsPlay: true, levels: incoming}
	st.smoothContour()
	st.updateTransport(start, track, false, 0)
	resume := start.Add(50 * time.Millisecond)
	st.step(resume, silence, false)
	before, light := st.levels, st.blend.light(resume)
	st.updateTransport(resume, track, true, 0)
	st.step(resume, incoming, true)
	if st.levels != before || light <= 0 || light >= 1 || st.blend.light(resume) != light {
		t.Fatal("resuming during the fade jumped the displayed spectrum")
	}

	pause := resume.Add(spectrumBlendTime / 2)
	st.step(pause, incoming, true)
	before, light = st.levels, st.blend.light(pause)
	st.updateTransport(pause, track, false, 0)
	if st.levels != before || st.blend.light(pause) != light {
		t.Fatal("pausing during the blend jumped the displayed spectrum")
	}
	st.step(pause.Add(30*time.Millisecond), silence, false)
	if st.levels[20] >= before[20] || st.blend.light(st.at) >= light {
		t.Fatal("the interrupted resume kept raising the contour or its highlight")
	}
}

// The spectrum falls away smoothly during pause, seek, and track loading, and stops when hidden.
func TestLyricSpectrumStops(t *testing.T) {
	a := spectrumTestApp()
	tt := ui.NewTester(a.view, 880, 580)
	tt.SetScale(.5)
	tt.Advance(time.Second)

	seedLyricSpectrum(a, tt.Now())
	tt.Frame()
	if a.npSpectrum.renderer.mask == nil {
		t.Fatal("the lyrics page did not draw the spectrum")
	}
	initial := a.npSpectrum.peak
	// Its motion follows display frames rather than skipping every other one.
	updates := 0
	for range 12 {
		before := a.npSpectrum.at
		tt.Advance(time.Second / 60)
		if a.npSpectrum.at.After(before) {
			updates++
		}
	}
	if updates < 10 {
		t.Fatalf("spectrum updated on only %d of 12 display frames", updates)
	}
	if peak := a.npSpectrum.peak; peak <= initial*0.40 || peak >= initial {
		t.Errorf("the paused spectrum should fall gradually, from %g to %g", initial, peak)
	}
	tt.Advance(2 * time.Second)
	if a.npSpectrum.peak > lyricSpectrumSilent {
		t.Fatal("the paused spectrum did not settle")
	}
	at := a.npSpectrum.at
	tt.Advance(time.Second)
	if !at.Equal(a.npSpectrum.at) {
		t.Error("an idle spectrum kept asking for frames")
	}

	seedLyricSpectrum(a, tt.Now())
	peak := a.npSpectrum.peak
	a.pl.Seek(12 * time.Second)
	tt.Frame()
	if a.npSpectrum.peak != peak {
		t.Error("the spectrum jumped on the first frame after seeking")
	}
	tt.Advance(250 * time.Millisecond)
	if next := a.npSpectrum.peak; next <= 0 || next >= peak {
		t.Errorf("the spectrum did not ease out while seeking: %g → %g", peak, next)
	}
	peak = a.npSpectrum.peak
	a.pl.Cue([]netease.Track{{ID: 8, Name: "下一首", Duration: 200000}})
	tt.Frame()
	if a.npSpectrum.peak != peak {
		t.Error("the spectrum jumped on the first frame of the next song")
	}
	tt.Advance(250 * time.Millisecond)
	if next := a.npSpectrum.peak; next <= 0 || next >= peak {
		t.Errorf("the outgoing spectrum did not fall while waiting for audio: %g → %g", peak, next)
	}
	seedLyricSpectrum(a, tt.Now())
	peak, mask := a.npSpectrum.peak, a.npSpectrum.renderer.mask
	a.npPane = paneStage
	tt.Frame()
	if a.npSpectrum.peak != peak || a.npSpectrum.renderer.mask != mask {
		t.Error("switching to the stage discarded the current spectrum")
	}
	a.npPane = paneQueue
	tt.Frame()
	if a.npSpectrum != (lyricSpectrum{}) {
		t.Error("the queue kept the lyrics page's spectrum")
	}
	a.npPane = paneLyrics
	tt.Frame()
	seedLyricSpectrum(a, tt.Now())
	tt.SetPreferences(ui.Preferences{ReduceMotion: true})
	if a.npSpectrum != (lyricSpectrum{}) {
		t.Error("Reduce Motion kept the animated spectrum")
	}
}

// Spatial smoothing restrains a single spike without flattening a sustained band of energy.
func TestLyricSpectrumRoundsSpikes(t *testing.T) {
	const n = player.DetailBands
	var st lyricSpectrum
	st.levels[n/4] = 1
	for i := n / 2; i < 3*n/4; i++ {
		st.levels[i] = .75
	}
	st.smoothContour()
	if spike, hill := st.contour[n/4], st.contour[5*n/8]; spike >= .5 || hill < .74 {
		t.Errorf("isolated spike %g and broad hill %g", spike, hill)
	}
}

// A late or rapidly replaced cover restarts the spectrum's fade from its displayed color.
func TestLyricSpectrumTintTransition(t *testing.T) {
	start := time.Unix(10, 0)
	red, blue, green := ui.RGB(180, 80, 80), ui.RGB(80, 80, 180), ui.RGB(80, 180, 80)
	var st lyricSpectrum
	if got := st.blendTint(start, red); got != red {
		t.Fatalf("first draw should use its cover's color immediately: %v", got)
	}
	firstChange := start.Add(time.Second)
	if got := st.blendTint(firstChange, blue); got != red {
		t.Errorf("a new cover should begin at the previous color: %v", got)
	}
	secondChange := firstChange.Add(backdropFade / 4)
	visible := st.blendTint(secondChange, blue)
	if visible == red || visible == blue {
		t.Errorf("the first transition did not blend: %v", visible)
	}
	if got := st.blendTint(secondChange, green); got != visible {
		t.Errorf("a quick second change jumped from %v to %v", visible, got)
	}
	if got := st.blendTint(secondChange.Add(backdropFade), green); got != green {
		t.Errorf("the transition did not settle on the final cover: %v", got)
	}
}

// With NM_FRAMES set, save the spectrum behind the real lyrics view at two window sizes.
func TestLyricSpectrumFrames(t *testing.T) {
	dir := os.Getenv("NM_FRAMES")
	if dir == "" {
		t.Skip()
	}
	for _, size := range []struct {
		name          string
		width, height int
		pane          int
	}{{"desktop", 1200, 800, paneLyrics}, {"compact", 750, 580, paneLyrics}, {"stage", 1200, 800, paneStage}} {
		a := spectrumTestApp()
		a.npPane = size.pane
		tt := ui.NewTester(a.view, size.width, size.height)
		tt.Advance(time.Second)
		seedLyricSpectrum(a, tt.Now())
		tt.Frame()
		f, err := os.Create(filepath.Join(dir, "lyric-spectrum-"+size.name+".png"))
		if err != nil {
			t.Fatal(err)
		}
		err = png.Encode(f, tt.Image())
		f.Close()
		if err != nil {
			t.Fatal(err)
		}
	}
}

func spectrumTestApp() *app {
	a := newTestApp()
	a.settings = defaultSettings()
	a.settings.LyricSpectrum = true
	a.nowPlaying, a.npPane = true, paneLyrics
	a.pl.Cue([]netease.Track{{ID: 7, Name: "夜航", Artists: []netease.Artist{{Name: "测试音轨"}}, Duration: 200000}})
	a.pl.SetLyrics(lyrics.Build("[00:00.00]夜色落在海面\n[00:04.00]星光照亮归途\n[00:08.00]远处的灯缓缓亮起\n[00:12.00]风从耳边经过\n[00:16.00]潮水轻轻退去\n[00:20.00]月亮停在天边\n", "", ""))
	a.pl.Seek(8 * time.Second)
	return a
}

func seedLyricSpectrum(a *app, now time.Time) {
	st := &a.npSpectrum
	st.at = now
	for i := range st.levels {
		x := float64(i) / float64(len(st.levels)-1)
		st.levels[i] = float32((.48 + .22*math.Sin(16*x) + .19*math.Sin(35*x) + .04*math.Cos(150*x)) * (1 - .50*x))
	}
	st.smoothContour()
}
