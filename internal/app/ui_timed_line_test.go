package app

import (
	"image"
	"math"
	"strings"
	"testing"
	"time"

	"github.com/egoist/mygo/ui"

	"netease-music-mygo/internal/lyrics"
	"netease-music-mygo/internal/netease"
)

// A timed word split across lines must paint both pieces again after seeking backward.
func TestWrappedLyricWordAfterBackwardSeek(t *testing.T) {
	// Latin glyphs keep this test independent of the runner's CJK fonts.
	text := strings.Repeat("m", 8)
	line := lyrics.Line{Text: text, Words: []lyrics.Word{{Text: text, Start: time.Second, End: 2 * time.Second}}}
	a := newTestApp()
	a.pl.Cue([]netease.Track{{ID: 1, Duration: 8000}})
	a.pl.Seek(2 * time.Second)
	tt := ui.NewTester(func(c *ui.Context) {
		c.Root().Background(ui.RGB(0, 0, 0))
		w, _ := c.Size()
		a.paintLyricWords(c, &line, w, linesStyle{size: 30, weight: 800, lineHeight: 1.25})
	}, 400, 200)
	initial := a.paintedLyric.layout.Lines()
	if len(initial) != 1 {
		t.Fatalf("before resize: got %d visual lines, want 1", len(initial))
	}
	// Choose the new width relative to the shaped word so the font cannot
	// decide whether the test actually exercises wrapping.
	tt.SetSize(int(math.Ceil(float64(initial[0].Rect.W)*0.6)), 200)
	wrapped := a.paintedLyric.layout.Lines()
	if len(wrapped) != 2 {
		t.Fatalf("after resize: got %d visual lines, want 2", len(wrapped))
	}
	lit := tt.Image()
	a.pl.Seek(1500 * time.Millisecond)
	tt.Frame()
	half := tt.Image()
	a.pl.Seek(0)
	tt.Frame()
	dim := tt.Image()
	for i, line := range wrapped {
		// Sample each visual line's own box, independent of the highlight clips.
		top := int(math.Ceil(float64(line.Rect.Y)))
		bottom := int(math.Floor(float64(line.Rect.Y + line.Rect.H)))
		if !pixelsLowerInBand(lit, half, top, bottom) || !pixelsLowerInBand(half, dim, top, bottom) {
			t.Errorf("visual line %d did not show a partial highlight after seeking backward", i+1)
		}
	}
}

func pixelsLowerInBand(before, after *image.RGBA, top, bottom int) bool {
	for y := top; y < bottom; y++ {
		for x := 0; x < before.Bounds().Dx(); x++ {
			if before.RGBAAt(x, y).R > after.RGBAAt(x, y).R {
				return true
			}
		}
	}
	return false
}
