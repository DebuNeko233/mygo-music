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
	if got := lyricInterludeAt(lines, -1, 0); !got.intro || got.to != lines[0].At {
		t.Fatalf("intro: %+v", got)
	}
	if got := lyricInterludeAt(lines, 0, 1); got.from != 8*time.Second || got.to != lines[1].At {
		t.Fatalf("interlude: %+v", got)
	}
	for _, i := range []int{1, 2, 3} {
		next := i + 1
		if next >= len(lines) {
			next = -1
		}
		if got := lyricInterludeAt(lines, i, next); got.to != 0 {
			t.Errorf("line %d unexpectedly has an interlude: %+v", i, got)
		}
	}
	lrc := lyrics.Build("[00:05.00]First\n[00:17.00]Second", "", "")
	if got := lyricInterludeAt(lrc, 0, 1); got.to != 0 {
		t.Fatalf("estimated LRC end produced an interlude: %+v", got)
	}
}

// A lyric block that fits stays centered before and after the first timestamp.
func TestLyricIntroSkipsFittingLyrics(t *testing.T) {
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
	if !ok || abs32(with.Bounds.Y-without.Bounds.Y) > 1 {
		t.Fatalf("the intro moved the first lyric: before %+v, after %+v", without.Bounds, with.Bounds)
	}
	checkShortLyricsCenter(t, tt, a, lines, 320)
	if err := tt.Click(lines[0].Text); err != nil || a.pl.Position() != lines[0].At {
		t.Fatalf("could not seek to first lyric: %v, position %v", err, a.pl.Position())
	}
	checkShortLyricsCenter(t, tt, a, lines, 320)
}

// Wrapped lyrics regain the intro when they overflow and lose it when they fit again.
func TestLyricIntroFollowsReflow(t *testing.T) {
	const height = 280
	lines := []lyrics.Line{
		{At: 6 * time.Second, Text: "Composed and arranged", Translation: strings.Repeat("Production credits with wrapping. ", 11)},
		{At: 10 * time.Second, Text: "Produced and mixed"},
	}
	a, tt := shortLyricsApp(lines, 720, height)
	a.settings.LyricEdgeFade = true
	tt.Frame()
	checkShortLyricsCenter(t, tt, a, lines, height)
	tt.SetSize(180, height)
	first, ok := tt.AccessByRole(ui.RoleButton, lines[0].Text)
	if !ok || a.lyricScroll.MaxY <= 0 || first.Bounds.Y < height*0.3+lyricIntroHeight {
		t.Fatalf("wrapped credits should scroll and show the intro: first row %+v, scroll %+v", first.Bounds, a.lyricScroll)
	}
	tt.SetSize(720, height)
	checkShortLyricsCenter(t, tt, a, lines, height)
}

// A lyric near the close button ignores clicks and becomes clickable again when it moves away.
func TestLyricNearCloseDoesNotSeek(t *testing.T) {
	a := newTestApp()
	a.pl.Cue([]netease.Track{{ID: 1, Duration: 30000}})
	line := lyrics.Line{At: 5 * time.Second, Text: "Lyric"}
	y := float32(35)
	tt := ui.NewTester(func(c *ui.Context) {
		st := linesStyle{size: 20, immersive: true, closeBounds: ui.Rect{X: 60, Y: 0, W: 38, H: 38}}
		ui.Column(c).Absolute().Top(y).Left(75).Width(180).Children(func() {
			a.timedLine(c, &line, 0, 156, st)
		})
	}, 280, 180)
	tt.Frame()
	clickLyric := func() {
		t.Helper()
		row, ok := tt.AccessByRole(ui.RoleButton, line.Text)
		if !ok {
			t.Fatal("lyric row is missing")
		}
		tt.ClickAt(row.Bounds.X+45, row.Bounds.Y+row.Bounds.H/2)
	}
	clickLyric()
	if got := a.pl.Position(); got != 0 {
		t.Fatalf("clicking the protected row sought to %v", got)
	}
	y = 100
	tt.Frame()
	tt.Frame()
	clickLyric()
	if got := a.pl.Position(); got != line.At {
		t.Fatalf("clicking below the protected area sought to %v, want %v", got, line.At)
	}
}

// Intro dots reserve space directly above the first sung line, even after opening credits.
func TestLyricIntroEasesIntoFirstLine(t *testing.T) {
	const firstAt = 6 * time.Second
	for _, tc := range []struct {
		count, height int
	}{
		{12, 180},
		{timedListThreshold, 180},
	} {
		t.Run(fmt.Sprintf("%d-lines-%d-high", tc.count, tc.height), func(t *testing.T) {
			lines := make([]lyrics.Line, tc.count)
			lines[0] = lyrics.Line{Text: "作词：A"}
			lines[1] = lyrics.Line{At: time.Second, Text: "作曲：B"}
			const first = 2
			for i := first; i < len(lines); i++ {
				lines[i] = lyrics.Line{At: firstAt + time.Duration(i-first)*time.Second, Text: fmt.Sprintf("Lyric %02d", i)}
			}
			a, tt := shortLyricsApp(lines, 200, tc.height)
			a.settings.LyricEdgeFade = true
			tt.SetPreferences(ui.Preferences{})
			firstY := func(left time.Duration) float32 {
				t.Helper()
				a.pl.Hold(firstAt - left)
				tt.Frame()
				row, ok := tt.AccessByRole(ui.RoleButton, lines[first].Text)
				if !ok {
					t.Fatal("first line missing during the transition")
				}
				return row.Bounds.Y
			}
			start := firstY(lyricIntroExit)
			mid := firstY(lyricIntroExit / 2)
			near := firstY(10 * time.Millisecond)
			end := firstY(0)
			if !(start > mid && mid > near) || abs32(near-end) > 1 || abs32(end-float32(tc.height)*0.3) > 2 {
				t.Fatalf("the first row jumped during the intro: start %.2f, middle %.2f, near %.2f, end %.2f", start, mid, near, end)
			}
		})
	}
}

// Resizing during an automatic glide retargets the motion without snapping to the new anchor.
func TestImmersiveLyricGlideContinuesAfterResize(t *testing.T) {
	for _, count := range []int{12, timedListThreshold} {
		t.Run(fmt.Sprint(count), func(t *testing.T) {
			lines := make([]lyrics.Line, count)
			for i := range lines {
				lines[i] = lyrics.Line{At: time.Duration(i) * time.Second, Text: fmt.Sprintf("Line %d", i)}
			}
			a, tt := shortLyricsApp(lines, 120, 200)
			a.settings.LyricEdgeFade = true
			a.pl.Hold(lines[4].At)
			tt.Frame()
			tt.SetPreferences(ui.Preferences{})
			a.pl.Hold(lines[5].At)
			tt.Frame()
			tt.Advance(80 * time.Millisecond)
			tt.SetSize(120, 170)
			const target = 170 * 0.3
			row := func() float32 {
				t.Helper()
				got, ok := tt.AccessByRole(ui.RoleButton, lines[5].Text)
				if !ok || got.Offscreen {
					t.Fatalf("active line left the viewport: %+v", got)
				}
				return got.Bounds.Y
			}
			start := row()
			tt.Advance(90 * time.Millisecond)
			middle := row()
			tt.Advance(1100 * time.Millisecond)
			end := row()
			if start-middle < 2 || middle-end < 2 || abs32(end-target) > 2 {
				t.Fatalf("resize interrupted the glide: %.1f → %.1f → %.1f (target %.1f)", start, middle, end, target)
			}
		})
	}
}

// Immersive lyrics light LRC as a whole line and reserve the sweep for real word timestamps.
func TestImmersiveLyricsUseActualWordTiming(t *testing.T) {
	a := newTestApp()
	a.pl.Cue([]netease.Track{{ID: 1, Duration: 10000}})
	a.pl.Hold(1500 * time.Millisecond)
	line := lyrics.Build("[00:01.00]Hello world\n[00:05.00]Next line", "", "")[0]
	if len(line.Words) == 0 {
		t.Fatal("the LRC fixture needs estimated words")
	}
	st := linesStyle{size: 24, weight: 800, lineHeight: 1.25, immersive: true}
	tt := ui.NewTester(func(c *ui.Context) {
		a.timedLine(c, &line, 0, 256, st)
	}, 280, 80)
	if !tt.HasText(line.Text) || a.paintedLyric.line != nil {
		t.Fatal("LRC should render as one line without scheduling word highlights")
	}
	line.Timed = true
	tt.Frame()
	if a.paintedLyric.line != &line || len(a.paintedLyric.sweep) != len(line.Words) {
		t.Fatal("word-timed lyrics should use the sweep")
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
