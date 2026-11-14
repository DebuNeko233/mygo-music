package app

import (
	"fmt"
	"image/png"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/egoist/mygo/ui"

	"netease-music-mygo/internal/lyrics"
	"netease-music-mygo/internal/netease"
)

// Only a long intro or a gap with a known end time shows the interlude.
func TestLyricInterludeTiming(t *testing.T) {
	lines := []lyrics.Line{
		{At: 5 * time.Second, End: 7 * time.Second, Timed: true},
		{At: 12 * time.Second},
		{At: 20 * time.Second, End: 22 * time.Second, Timed: true},
		{At: 24 * time.Second, End: 28 * time.Second},
	}
	if got := lyricInterludeAt(lines, -1); !got.intro || got.to != lines[0].At {
		t.Fatalf("intro: %+v", got)
	}
	if got := lyricInterludeAt(lines, 0); got.from != 8*time.Second || got.to != lines[1].At {
		t.Fatalf("interlude: %+v", got)
	}
	for _, i := range []int{1, 2, 3} {
		if got := lyricInterludeAt(lines, i); got.to != 0 {
			t.Errorf("line %d unexpectedly has an interlude: %+v", i, got)
		}
	}
	lrc := lyrics.Build("[00:05.00]First\n[00:17.00]Second", "", "")
	if got := lyricInterludeAt(lrc, 0); got.to != 0 {
		t.Fatalf("estimated LRC end produced an interlude: %+v", got)
	}
}

// The intro reserves room above the first lyric without taking over its click target.
func TestLyricIntroKeepsFirstLine(t *testing.T) {
	lines := []lyrics.Line{
		{At: 6 * time.Second, Text: "First lyric", Translation: "Translation"},
		{At: 10 * time.Second, Text: "Second lyric"},
	}
	a, tt := shortLyricsApp(lines, 320, 320)
	without, ok := tt.AccessByRole(ui.RoleButton, lines[0].Text)
	if !ok {
		t.Fatal("first lyric missing")
	}
	a.settings.LyricEdgeFade = true
	tt.Frame()
	with, ok := tt.AccessByRole(ui.RoleButton, lines[0].Text)
	if !ok || with.Bounds.Y <= without.Bounds.Y || a.lyricScroll.MaxY > 1 {
		t.Fatalf("intro should sit above the first lyric in a centered block: before %+v, after %+v", without.Bounds, with.Bounds)
	}
	if err := tt.Click(lines[0].Text); err != nil || a.pl.Position() != lines[0].At {
		t.Fatalf("could not seek to first lyric: %v, position %v", err, a.pl.Position())
	}
	checkShortLyricsCenter(t, tt, a, lines, 320)
}

// Removing the dots used to jump the first line to its final position in one frame.
func TestLyricIntroEasesIntoFirstLine(t *testing.T) {
	const firstAt = 6 * time.Second
	for _, tc := range []struct {
		count, height int
	}{
		{2, 320}, // centered
		{2, 140}, // centered, with limited space for the dots
		{12, 180},
		{timedListThreshold, 180},
	} {
		t.Run(fmt.Sprintf("%d-lines-%d-high", tc.count, tc.height), func(t *testing.T) {
			lines := make([]lyrics.Line, tc.count)
			for i := range lines {
				lines[i] = lyrics.Line{At: firstAt + time.Duration(i)*time.Second, Text: fmt.Sprintf("Lyric %02d", i)}
			}
			a, tt := shortLyricsApp(lines, 200, tc.height)
			a.settings.LyricEdgeFade = true
			tt.SetPreferences(ui.Preferences{})
			firstY := func(left time.Duration) float32 {
				t.Helper()
				a.pl.Hold(firstAt - left)
				tt.Frame()
				row, ok := tt.AccessByRole(ui.RoleButton, lines[0].Text)
				if !ok {
					t.Fatal("first line missing during the transition")
				}
				return row.Bounds.Y
			}
			start := firstY(lyricIntroExit)
			mid := firstY(lyricIntroExit / 2)
			near := firstY(10 * time.Millisecond)
			end := firstY(0)
			if !(start > mid && mid > near) || abs32(near-end) > 1 {
				t.Fatalf("the first row jumped during the intro: start %.2f, middle %.2f, near %.2f, end %.2f", start, mid, near, end)
			}
		})
	}
}

// A wrapped word keeps a sweep on both visual lines after reflow.
func TestLyricSweepReflows(t *testing.T) {
	text := strings.Repeat("m", 8)
	line := lyrics.Line{Text: text, Words: []lyrics.Word{{Text: text, Start: time.Second, End: 3 * time.Second}}}
	a := newTestApp()
	a.pl.Cue([]netease.Track{{ID: 1, Duration: 8000}})
	a.pl.Seek(1700 * time.Millisecond)
	tt := ui.NewTester(func(c *ui.Context) {
		c.Root().Background(ui.Hex("#22384b"))
		w, _ := c.Size()
		a.paintLyricWords(c, &line, w, linesStyle{size: 30, weight: 800, lineHeight: 1.25, immersive: true})
	}, 400, 150)
	initial := a.paintedLyric.layout.Lines()
	if len(initial) != 1 || len(a.paintedLyric.sweep[0].parts) != 1 {
		t.Fatal("expected a sweep on the original line")
	}
	tt.SetSize(int(initial[0].Rect.W*0.6), 150)
	if got := a.paintedLyric.sweep[0].parts; len(got) != 2 || got[0].rect.Y >= got[1].rect.Y {
		t.Fatalf("sweep did not follow visual line order after wrapping: %+v", got)
	}
	if dir := os.Getenv("NM_FRAMES"); dir != "" {
		f, err := os.Create(filepath.Join(dir, "lyric-word-sweep.png"))
		if err != nil {
			t.Fatal(err)
		}
		defer f.Close()
		if err := png.Encode(f, tt.Image()); err != nil {
			t.Fatal(err)
		}
	}
}

// Color glyphs fall back to opacity so an emoji at the end cannot bypass the sweep.
func TestLyricSweepColorGlyphFallback(t *testing.T) {
	for _, text := range []string{"word🎵", "1️⃣", "🇨🇳"} {
		if !lyricMayHaveColorGlyph(text) {
			t.Errorf("%q should keep the opacity highlight", text)
		}
	}
	if lyricMayHaveColorGlyph("一行 lyrics") {
		t.Fatal("ordinary lyric text lost the sweep")
	}
}
