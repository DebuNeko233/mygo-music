package app

import (
	"image"
	"strings"
	"testing"
	"time"

	"github.com/egoist/mygo/ui"

	"netease-music-mygo/internal/lyrics"
	"netease-music-mygo/internal/netease"
)

// A timed word split across lines must paint both pieces again after seeking backward.
func TestWrappedLyricWordAfterBackwardSeek(t *testing.T) {
	text := strings.Repeat("唱", 7)
	line := lyrics.Line{Text: text, Words: []lyrics.Word{{Text: text, Start: time.Second, End: 2 * time.Second}}}
	a := newTestApp()
	a.pl.Cue([]netease.Track{{ID: 1, Duration: 8000}})
	a.pl.Seek(2 * time.Second)
	tt := ui.NewTester(func(c *ui.Context) {
		c.Root().Background(ui.RGB(0, 0, 0))
		w, _ := c.Size()
		a.paintLyricWords(c, &line, w, linesStyle{size: 30, weight: 800, lineHeight: 1.25})
	}, 125, 90)
	// The first pass exercises an already-shaped line; narrowing forces a
	// fresh layout and puts the same timed word on two visual lines.
	tt.SetSize(110, 90)
	lit := tt.Image()
	a.pl.Seek(1500 * time.Millisecond)
	tt.Frame()
	half := tt.Image()
	a.pl.Seek(0)
	tt.Frame()
	dim := tt.Image()
	for _, rows := range [][2]int{{0, 35}, {40, 70}} {
		if !pixelsLowerInBand(lit, half, rows[0], rows[1]) || !pixelsLowerInBand(half, dim, rows[0], rows[1]) {
			t.Fatalf("wrapped text did not show partial highlight in rows %v", rows)
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
