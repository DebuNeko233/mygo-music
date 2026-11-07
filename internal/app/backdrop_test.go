package app

import (
	"image"
	"image/draw"
	"image/png"
	"math"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/egoist/mygo/ui"

	"netease-music-mygo/internal/netease"
	"netease-music-mygo/internal/player"
)

// The drift eases in as the music plays and glides to a stop as it
// pauses; the bass swells and settles.
func TestBackdropDrifts(t *testing.T) {
	var bd backdropCache
	now := time.Unix(0, 0)
	var quiet [player.Bands]float32
	for range 120 {
		now = now.Add(backdropFrame)
		bd.step(now, true, quiet, true)
	}
	if bd.speed != 1 || bd.drift <= 0 {
		t.Fatalf("playing: speed %v, drift %v", bd.speed, bd.drift)
	}
	var loud [player.Bands]float32
	for i := range loud {
		loud[i] = 1
	}
	now = now.Add(backdropFrame)
	bd.step(now, true, loud, true)
	if bd.level.group(0, 6) <= 0.2 {
		t.Errorf("bass %v on a beat", bd.level.group(0, 6))
	}
	moving := true
	for range 120 {
		now = now.Add(backdropFrame)
		moving = bd.step(now, false, quiet, false)
	}
	if moving || bd.speed != 0 {
		t.Errorf("paused: still moving, speed %v", bd.speed)
	}
	at := bd.drift
	now = now.Add(time.Second)
	bd.step(now, false, quiet, false)
	if bd.drift != at {
		t.Error("drifted while paused")
	}
}

// The software-rendering fallback keeps one sample per display pixel,
// including after a Retina-scale change or resize: a thumbnail banded.
func TestBackdropPixelResolution(t *testing.T) {
	a := newTestApp()
	a.settings = defaultSettings()
	tt := ui.NewTester(func(c *ui.Context) { a.backdrop(c, nil, ui.RGB(50, 80, 140)) }, 240, 160)
	check := func() {
		t.Helper()
		w, h := a.backdropCache.canvas.Size()
		want := tt.Image().Bounds().Size()
		if w != want.X || h != want.Y {
			t.Fatalf("background %dx%d for a %dx%d display", w, h, want.X, want.Y)
		}
	}
	check()
	first := a.backdropCache.canvas
	tt.Frame()
	if a.backdropCache.canvas != first {
		t.Fatal("unchanged background replaced its canvas")
	}
	if tt.FramePending() {
		t.Fatal("still background requested another frame")
	}
	tt.SetScale(2)
	check()
	tt.SetSize(280, 180)
	check()
}

// Dark gradient samples keep distinctions below one 8-bit color step,
// so the final pixel dither can reproduce their intermediate averages.
func TestBackdropRampPrecision(t *testing.T) {
	for _, tc := range []struct {
		name string
		tint ui.Color
	}{
		{"gray", ui.RGB(100, 100, 100)},
		{"blue", ui.RGB(50, 80, 140)},
		{"red", ui.RGB(180, 50, 45)},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ramp := backdropRamp(tc.tint)
			decode := func(x, channel int) uint16 {
				return uint16(ramp.Pix[x*4+channel])<<8 | uint16(ramp.Pix[ramp.Stride+x*4+channel])
			}
			top, foot := backdropColors(tc.tint)
			for i, colors := range [][3]uint8{{top.R, top.G, top.B}, {foot.R, foot.G, foot.B}} {
				x := i * (backdropRampSize - 1)
				for channel, want := range colors {
					if math.Abs(float64(decode(x, channel))/65535-float64(want)/255) > 1.0/65535 {
						t.Fatalf("endpoint %d channel %d lost precision", i, channel)
					}
				}
			}
			distinct := map[[3]uint16]bool{}
			last := decode(0, 0)
			for x := range backdropRampSize {
				v := [3]uint16{decode(x, 0), decode(x, 1), decode(x, 2)}
				if tc.name == "gray" && v[0] > last {
					t.Fatal("dark gradient reversed its direction")
				}
				// The packed bytes are sampled as opaque RGBA. Translucent
				// rows would be premultiplied and decode to different values.
				if ramp.Pix[x*4+3] != 255 || ramp.Pix[ramp.Stride+x*4+3] != 255 {
					t.Fatal("packed gradient bytes are not opaque")
				}
				distinct[v] = true
				last = v[0]
			}
			if len(distinct) <= backdropRampSize/2 {
				t.Fatalf("only %d distinct colors in a dark gradient", len(distinct))
			}
		})
	}
}

// Resizing and changing display scale reuse the small palette instead
// of allocating another screen-sized background texture.
func TestBackdropRampSurvivesResize(t *testing.T) {
	a := newTestApp()
	a.settings = defaultSettings()
	tt := ui.NewTester(func(c *ui.Context) { a.backdrop(c, nil, ui.RGB(50, 80, 140)) }, 240, 160)
	palette := a.backdropCache.still
	if palette == nil {
		t.Fatal("no palette")
	}
	ramp := palette.ramp
	tt.SetScale(2)
	tt.SetSize(280, 180)
	if a.backdropCache.still != palette || a.backdropCache.still.ramp != ramp {
		t.Fatal("resize replaced the palette")
	}
}

// A crossfade keeps at most the previous palette and drops it once the
// transition finishes; an unchanged color shares the same ramp.
func TestStillBackdropTransition(t *testing.T) {
	var bd backdropCache
	now := time.Unix(0, 0)
	blue := ui.RGB(50, 80, 140)
	bd.prepareStill(nil, blue, now)
	first := bd.still
	bd.prepareStill(nil, blue, now.Add(backdropFade/4))
	if bd.still != first || bd.previousStill != nil || bd.stillAt != now {
		t.Fatal("unchanged background restarted its transition")
	}
	bd.prepareStill(&artwork{}, blue, now)
	if bd.still.ramp != first.ramp || bd.previousStill != first {
		t.Fatal("unchanged color failed to reuse its ramp")
	}
	second := bd.still
	bd.prepareStill(nil, ui.RGB(180, 50, 45), now)
	if bd.previousStill != second || bd.still.ramp == second.ramp {
		t.Fatal("rapid track change kept more than the previous palette")
	}
	if got := bd.stillWeight(now.Add(backdropFade / 2)); math.Abs(float64(got-0.125)) > 0.001 {
		t.Fatalf("midpoint previous weight %v", got)
	}
	if bd.stillWeight(now.Add(backdropFade)) != 0 || bd.previousStill != nil {
		t.Fatal("transition retained the old palette")
	}
}

// Switching to fog drops both static palettes, including a song whose
// crossfade was interrupted, so unused artwork is no longer retained.
func TestFogDropsStillPalettes(t *testing.T) {
	a := newTestApp()
	a.settings = defaultSettings()
	tint := ui.RGB(50, 80, 140)
	tt := ui.NewTester(func(c *ui.Context) { a.backdrop(c, nil, tint) }, 240, 160)
	tint = ui.RGB(180, 50, 45)
	tt.Frame()
	if a.backdropCache.still == nil || a.backdropCache.previousStill == nil {
		t.Fatal("song change did not retain two palettes for its transition")
	}
	a.settings.Fog = true
	tt.Frame()
	if a.backdropCache.still != nil || a.backdropCache.previousStill != nil {
		t.Fatal("fog retained static palette states")
	}
}

// With NM_FRAMES set, saves still gradients at normal and Retina scale
// for inspecting dark color transitions without network or playback.
func TestBackdropGradientFrames(t *testing.T) {
	dir := os.Getenv("NM_FRAMES")
	if dir == "" {
		t.Skip()
	}
	for _, tc := range []struct {
		name string
		tint ui.Color
	}{
		{"blue", ui.RGB(50, 80, 140)},
		{"red", ui.RGB(180, 50, 45)},
		{"gray", ui.RGB(100, 100, 100)},
	} {
		for _, scale := range []float32{1, 2} {
			var bd backdropCache
			bd.prepareStill(nil, tc.tint, time.Unix(0, 0))
			tt := ui.NewTester(func(c *ui.Context) {
				ui.Box(c).Fill().Draw(func(p *ui.Painter, r ui.Rect) { bd.drawStill(p, r, r, 0, 1) })
			}, 800, 600)
			tt.SetScale(scale)
			name := tc.name + "-1x.png"
			if scale == 2 {
				name = tc.name + "-2x.png"
			}
			f, err := os.Create(filepath.Join(dir, name))
			if err != nil {
				t.Fatal(err)
			}
			err = png.Encode(f, tt.Image())
			closeErr := f.Close()
			if err != nil {
				t.Fatal(err)
			}
			if closeErr != nil {
				t.Fatal(closeErr)
			}
		}
	}
}

// With NM_FRAMES set, saves the fog of a few covers as it drifts,
// and on a beat, one row a cover.
func TestBackdropDriftFrames(t *testing.T) {
	dir := os.Getenv("NM_FRAMES")
	if dir == "" {
		t.Skip()
	}
	const w, h = 300, 420
	var rows []*image.RGBA
	for _, q := range []string{"春日影 MyGO", "晴天 周杰伦", "夜に駆ける"} {
		hits, err := netease.Search(q, 1)
		if err != nil || len(hits) == 0 {
			t.Fatal(q, err)
		}
		img, err := fetchImage(netease.Sized(hits[0].Album.PicURL, 600))
		if err != nil {
			t.Fatal(err)
		}
		art := &artwork{tint: dominantColor(img)}
		shots := []struct{ drift, bass float32 }{{0, 0}, {2, 0}, {4, 0}, {4, 0.9}}
		row := image.NewRGBA(image.Rect(0, 0, w*len(shots), h))
		for i, s := range shots {
			tt := ui.NewTester(func(c *ui.Context) {
				ui.Box(c).Fill().Draw(func(p *ui.Painter, r ui.Rect) { drawFog(p, r, art.tint, s.drift, s.bass) })
			}, w, h)
			tt.Frame()
			shot := tt.Image()
			draw.Draw(row, image.Rect(i*w, 0, (i+1)*w, h), shot, shot.Bounds().Min, draw.Src)
		}
		rows = append(rows, row)
	}
	out := image.NewRGBA(image.Rect(0, 0, rows[0].Bounds().Dx(), h*len(rows)))
	for i, r := range rows {
		draw.Draw(out, image.Rect(0, i*h, r.Bounds().Dx(), (i+1)*h), r, image.Point{}, draw.Src)
	}
	f, _ := os.Create(filepath.Join(dir, "drift.png"))
	defer f.Close()
	png.Encode(f, out)
}
