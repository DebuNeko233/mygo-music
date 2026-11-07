package app

import (
	"image"
	"image/draw"
	"image/png"
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

// The still background keeps one sample per display pixel, including
// after a Retina-scale change or resize: an enlarged thumbnail banded.
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
			a := newTestApp()
			a.settings = defaultSettings()
			tt := ui.NewTester(func(c *ui.Context) { a.backdrop(c, nil, tc.tint) }, 800, 600)
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
