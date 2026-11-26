/*
验证前奏与间奏边界、歌词进入动画，以及缩放和点击后的跟随行为。
*/

package playback

import (
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/egoist/mygo/ui"

	"netease-music-mygo/internal/features/lyrics"
	"netease-music-mygo/internal/features/netease"
	"netease-music-mygo/internal/features/player"
)

// Only a long intro or a gap with a known end time shows the interlude.
func TestLyricInterludeTiming(t *testing.T) {
	lines := []lyrics.Line{
		{At: 5 * time.Second, End: 7 * time.Second, Timed: true},
		{At: 12 * time.Second},
		{At: 20 * time.Second, End: 22 * time.Second, Timed: true},
		{At: 24 * time.Second, End: 28 * time.Second},
	}
	if got := LyricInterludeAt(lines, -1, 0); !got.Intro || got.To != lines[0].At {
		t.Fatalf("intro: %+v", got)
	}
	if got := LyricInterludeAt(lines, 0, 1); got.From != 8*time.Second || got.To != lines[1].At {
		t.Fatalf("interlude: %+v", got)
	}
	for _, i := range []int{1, 2, 3} {
		next := i + 1
		if next >= len(lines) {
			next = -1
		}
		if got := LyricInterludeAt(lines, i, next); got.To != 0 {
			t.Errorf("line %d unexpectedly has an interlude: %+v", i, got)
		}
	}
	lrc := lyrics.Build("[00:05.00]First\n[00:17.00]Second", "", "")
	if got := LyricInterludeAt(lrc, 0, 1); got.To != 0 {
		t.Fatalf("estimated LRC end produced an interlude: %+v", got)
	}
}

// A YRC row's declared end keeps the interlude from starting during the tail of the line.
func TestLyricInterludeUsesYRCLineEnd(t *testing.T) {
	const yrc = "[5000,3000](5000,1000,0)First\n[15000,1000](15000,1000,0)Second"
	_, display := lyrics.BuildDisplay("[00:10]作曲：甲", "", yrc)
	cur, next := display.Position(8 * time.Second)
	if got := LyricInterludeAt(display.Lines, cur, next); got.From != 9*time.Second || got.To != 15*time.Second {
		t.Fatalf("gap used the last word or an intervening credit instead of the sung line cues: %+v", got)
	}
}

// A lyric block that fits stays centered before and after the first timestamp.
func TestLyricIntroSkipsFittingLyrics(t *testing.T) {
	lines := []lyrics.Line{
		{At: 6 * time.Second, Text: "First lyric", Translation: "Translation"},
		{At: 10 * time.Second, Text: "Second lyric"},
	}
	a, tt := lyricPaneView(t, lines, 320, 320)
	without, ok := tt.AccessByRole(ui.RoleButton, lines[0].Text)
	if !ok {
		t.Fatal("first lyric missing")
	}
	a.Config.LyricEdgeFade = true
	tt.Frame()
	with, ok := tt.AccessByRole(ui.RoleButton, lines[0].Text)
	if !ok || Abs32(with.Bounds.Y-without.Bounds.Y) > 1 {
		t.Fatalf("the intro moved the first lyric: before %+v, after %+v", without.Bounds, with.Bounds)
	}
	checkShortLyricsCenter(t, tt, a, lines, 320)
	if err := tt.Click(lines[0].Text); err != nil || a.Player().Position() != lines[0].At {
		t.Fatalf("could not seek to first lyric: %v, position %v", err, a.Player().Position())
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
	a, tt := lyricPaneView(t, lines, 720, height)
	a.Config.LyricEdgeFade = true
	tt.Frame()
	checkShortLyricsCenter(t, tt, a, lines, height)
	tt.SetSize(180, height)
	first, ok := tt.AccessByRole(ui.RoleButton, lines[0].Text)
	if !ok || a.LyricScroll.MaxY <= 0 || first.Bounds.Y < height*0.3+LyricIntroHeight {
		t.Fatalf("wrapped credits should scroll and show the intro: first row %+v, scroll %+v", first.Bounds, a.LyricScroll)
	}
	tt.SetSize(720, height)
	checkShortLyricsCenter(t, tt, a, lines, height)
}

// A lyric near the close button ignores clicks and highlight hover until it moves away.
func TestLyricNearCloseDoesNotSeek(t *testing.T) {
	a := newTestView(t)
	player.Cue(a.Player(), []netease.Track{{ID: 1, Duration: 30000}})
	line := lyrics.Line{At: 5 * time.Second, Text: "Lyric"}
	y := float32(35)
	var hovered bool
	tt := ui.NewTester(func(c *ui.Context) {
		st := LinesStyle{Size: 20, Immersive: true, CloseBounds: ui.Rect{X: 60, Y: 0, W: 38, H: 38}}
		ui.Column(c).Absolute().Top(y).Left(75).Width(180).Children(func() {
			_, hovered = a.TimedLine(c, &line, 0, 156, st)
		})
	}, 280, 180)
	tt.Frame()
	clickLyric := func(wantHover bool) {
		t.Helper()
		row, ok := tt.AccessByRole(ui.RoleButton, line.Text)
		if !ok {
			t.Fatal("lyric row is missing")
		}
		x, y := row.Bounds.X+45, row.Bounds.Y+row.Bounds.H/2
		tt.Move(x, y)
		tt.Frame()
		if hovered != wantHover {
			t.Fatalf("hover available = %v, want %v", hovered, wantHover)
		}
		tt.ClickAt(x, y)
	}
	clickLyric(false)
	if got := a.Player().Position(); got != 0 {
		t.Fatalf("clicking the protected row sought to %v", got)
	}
	y = 100
	tt.Frame()
	tt.Frame()
	clickLyric(true)
	if got := a.Player().Position(); got != line.At {
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
		{TimedListThreshold, 180},
	} {
		t.Run(fmt.Sprintf("%d-lines-%d-high", tc.count, tc.height), func(t *testing.T) {
			lines := make([]lyrics.Line, tc.count)
			lines[0] = lyrics.Line{Text: "作词：A"}
			lines[1] = lyrics.Line{At: time.Second, Text: "作曲：B"}
			const first = 2
			for i := first; i < len(lines); i++ {
				lines[i] = lyrics.Line{At: firstAt + time.Duration(i-first)*time.Second, Text: fmt.Sprintf("Lyric %02d", i)}
			}
			a, tt := lyricPaneView(t, lines, 200, tc.height)
			tt.SetScale(0.25) // only row geometry is observed
			a.Config.LyricEdgeFade = true
			tt.SetPreferences(ui.Preferences{})
			firstY := func(left time.Duration) float32 {
				t.Helper()
				player.Hold(a.Player(), firstAt-left)
				tt.Frame()
				row, ok := tt.AccessByRole(ui.RoleButton, lines[first].Text)
				if !ok {
					t.Fatal("first line missing during the transition")
				}
				return row.Bounds.Y
			}
			start := firstY(LyricIntroExit)
			tt.Advance(LyricIntroExit / 2)
			mid := firstY(LyricIntroExit / 2)
			tt.Advance(LyricIntroExit/2 - 10*time.Millisecond)
			near := firstY(10 * time.Millisecond)
			tt.Advance(10 * time.Millisecond)
			end := firstY(0)
			if !(start > mid && mid > near) || Abs32(near-end) > 1 || Abs32(end-float32(tc.height)*0.3) > 2 {
				t.Fatalf("the first row jumped during the intro: start %.2f, middle %.2f, near %.2f, end %.2f", start, mid, near, end)
			}
		})
	}
}

// The intro exit must move the first row between refreshes without rebuilding its layout.
func TestLyricIntroExitReusesLayout(t *testing.T) {
	const firstAt = time.Second
	a := newTestView(t)
	var start time.Time
	var builds int
	tt := ui.NewTester(func(c *ui.Context) {
		if start.IsZero() {
			start = c.Now()
		}
		builds++
		gap := LyricInterlude{To: firstAt, Intro: c.Now().Sub(start) < firstAt}
		ui.Column(c).Fill().Background(ui.Hex("#000000")).Children(func() {
			box := ui.Column(c.Key("lyrics"))
			motion := PrepareLyricIntro(c, box, gap, c.Now().Sub(start), true)
			if motion.Gap.Intro {
				motion.Move(box, LyricIntroSpace)
			}
			box.Children(func() {
				if motion.Gap.Intro {
					slot, dots := LyricIntroDots(c)
					a.IntroDots(c, slot, dots, motion)
				}
				ui.Button(c.Key("first"), "First lyric").Margin(0, 0, 0, 80)
			})
		})
	}, 180, 120)
	y := func() float32 {
		t.Helper()
		row, ok := tt.AccessByRole(ui.RoleButton, "First lyric")
		if !ok {
			t.Fatal("the moving row is missing")
		}
		return row.Bounds.Y
	}
	dotLight := func() int {
		img := tt.Image()
		light := 0
		for y := range LyricIntroHeight {
			for x := range 60 {
				light += int(img.RGBAAt(x, y).R)
			}
		}
		return light
	}
	tt.Advance(firstAt - LyricIntroExit)
	before, top := builds, y()
	startDots := dotLight()
	tt.Advance(LyricIntroExit / 2)
	if middle := y(); middle <= 5 || middle >= top-5 {
		t.Fatalf("first row did not move through the exit: start %.2f, mid %.2f", top, middle)
	}
	if builds != before {
		t.Fatalf("the exit rebuilt the view %d times before the first cue", builds-before)
	}
	midDots := dotLight()
	tt.Advance(LyricIntroExit / 2)
	if end := y(); end > 1 {
		t.Fatalf("first row did not settle at its layout position: %.2f", end)
	}
	if endDots := dotLight(); !(startDots > midDots && midDots > endDots) {
		t.Fatalf("the dots did not fade with the row: start %d, mid %d, end %d", startDots, midDots, endDots)
	}
}

// Resizing during an automatic glide retargets the motion without snapping to the new anchor.
func TestImmersiveLyricGlideContinuesAfterResize(t *testing.T) {
	for _, count := range []int{12, TimedListThreshold} {
		t.Run(fmt.Sprint(count), func(t *testing.T) {
			lines := make([]lyrics.Line, count)
			for i := range lines {
				lines[i] = lyrics.Line{At: time.Duration(i) * time.Second, Text: fmt.Sprintf("Line %d", i)}
			}
			a, tt := lyricPaneView(t, lines, 120, 200)
			tt.SetScale(0.25)
			a.Config.LyricEdgeFade = true
			player.Hold(a.Player(), lines[4].At)
			tt.Frame()
			tt.SetPreferences(ui.Preferences{})
			player.Hold(a.Player(), lines[5].At)
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
			if start-middle < 2 || middle-end < 2 || Abs32(end-target) > 2 {
				t.Fatalf("resize interrupted the glide: %.1f → %.1f → %.1f (target %.1f)", start, middle, end, target)
			}
		})
	}
}

// Clicking a visible lyric resumes following with a glide, even after scrolling manually.
func TestLyricClickGlidesToReadingPosition(t *testing.T) {
	for _, count := range []int{20, TimedListThreshold} {
		for _, immersive := range []bool{false, true} {
			for _, browse := range []bool{false, true} {
				t.Run(fmt.Sprintf("rows=%d/immersive=%t/browse=%t", count, immersive, browse), func(t *testing.T) {
					lines := make([]lyrics.Line, count)
					for i := range lines {
						lines[i] = lyrics.Line{At: time.Duration(i) * time.Second, Text: fmt.Sprintf("Line %02d", i)}
					}
					a, tt := lyricPaneView(t, lines, 240, 300)
					tt.SetScale(0.25)
					a.Config.LyricEdgeFade = immersive
					player.Hold(a.Player(), lines[6].At)
					tt.Frame()
					target := lines[8]
					if browse {
						tt.Scroll(120, 120, 0, 70)
						target = lines[9]
					}
					if immersive && browse && a.Reading.BrowseUntil.IsZero() {
						t.Fatal("manual scrolling did not enter browsing mode")
					}
					tt.SetPreferences(ui.Preferences{})
					rowY := func() float32 {
						t.Helper()
						row, ok := tt.AccessByRole(ui.RoleButton, target.Text)
						if !ok || row.Offscreen {
							t.Fatalf("clicked lyric left the viewport: %+v", row)
						}
						return row.Bounds.Y
					}
					before := rowY()
					if err := tt.Click(target.Text); err != nil {
						t.Fatal(err)
					}
					if a.Player().Position() != target.At || !a.Reading.BrowseUntil.IsZero() {
						t.Fatalf("click did not seek and resume following: position %v", a.Player().Position())
					}
					start := rowY()
					tt.Advance(90 * time.Millisecond)
					middle := rowY()
					tt.Advance(1100 * time.Millisecond)
					end := rowY()
					if Abs32(start-before) > 2 || start-middle < 2 || middle-end < 2 || Abs32(end-90) > 2 {
						t.Fatalf("want a glide to y=90, got %.1f before click, then %.1f → %.1f → %.1f", before, start, middle, end)
					}
				})
			}
		}
	}
}

// Immersive lyrics light LRC as a whole line and reserve progressive highlights for real word timestamps.
func TestImmersiveLyricsUseActualWordTiming(t *testing.T) {
	a := newTestView(t)
	player.Cue(a.Player(), []netease.Track{{ID: 1, Duration: 10000}})
	player.Hold(a.Player(), 1500*time.Millisecond)
	line := lyrics.Build("[00:01.00]Hello world\n[00:05.00]Next line", "", "")[0]
	if len(line.Words) == 0 {
		t.Fatal("the LRC fixture needs estimated words")
	}
	st := LinesStyle{Size: 24, Weight: 800, LineHeight: 1.25, Immersive: true}
	tt := ui.NewTester(func(c *ui.Context) {
		a.TimedLine(c, &line, 0, 256, st)
	}, 280, 80)
	if !tt.HasText(line.Text) || a.PaintedLyric.Line != nil {
		t.Fatal("LRC should render as one line without scheduling word highlights")
	}
	line.Timed = true
	tt.Frame()
	if a.PaintedLyric.Line != &line || len(a.PaintedLyric.Clips) != len(line.Words) {
		t.Fatal("word-timed lyrics should paint individual words")
	}
}
