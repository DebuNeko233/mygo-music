package app

import (
	"slices"
	"testing"
	"time"

	"github.com/egoist/mygo/ui"

	"netease-music-mygo/internal/netease"
)

// Dragging the progress bar seeks once, where it is let go: not as the
// pointer moves, even off the thin bar, and not to the end, which would
// play the next song, before it is let go. The clock previews the drag.
func TestScrubSeeksOnRelease(t *testing.T) {
	a := newTestApp()
	a.settings = defaultSettings()
	tr := netease.Track{ID: 7, Name: "宝船", Duration: 200000}
	a.pl.Cue([]netease.Track{tr, {ID: 8, Name: "下一首"}})
	a.pl.SetLyrics(nil)
	// Keep playback at a fixed position regardless of how long CI takes
	// to draw the window under the race detector.
	const start = 3 * time.Second
	a.pl.Hold(start)
	a.playbackState.Open = true
	tt := ui.NewTester(a.view, 1200, 800)
	tt.Advance(time.Second)
	tt.Frame()
	bar, ok := tt.Find("播放进度")
	if !ok {
		t.Fatal("no progress bar")
	}
	checkTimes := func(elapsed, remaining string) {
		t.Helper()
		texts := tt.Texts()
		if !slices.Contains(texts, elapsed) || !slices.Contains(texts, remaining) {
			t.Fatalf("progress labels should show %q and %q, got %q", elapsed, remaining, texts)
		}
	}
	checkTimes("0:03", "−3:17")
	y := bar.Y + bar.H/2
	at := func(f float32) float32 { return bar.X + bar.W*f }

	tt.Press(at(0.25), y)
	tt.Frame()
	tt.Move(at(0.6), y-40) // off the bar, above it
	tt.Frame()
	tt.Move(at(1)+30, y+30) // past its end, below it
	tt.Frame()
	checkTimes("3:20", "0:00")
	if pos := a.pl.Position(); pos != start {
		t.Fatalf("seeked to %v while dragging", pos)
	}
	if a.pl.Current().ID != 7 {
		t.Fatal("went to the next song while dragging")
	}
	tt.Move(at(0.6), y-40)
	tt.Frame()
	tt.Release(at(0.6), y-40)
	tt.Frame()
	tt.Frame()
	if pos := a.pl.Position(); pos < 110*time.Second || pos > 130*time.Second {
		t.Errorf("let go at 60%%, at %v", pos)
	}
}

// Long tracks show hours, while unknown lengths leave the remaining time blank.
func TestNowPlayingTimeForLongAndUnknownDuration(t *testing.T) {
	a := newTestApp()
	for _, tc := range []struct {
		name               string
		duration           int64
		elapsed, remaining string
	}{
		{"long", 7_322_000, "1:01:01", "−1:01:01"},
		{"unknown", 0, "1:01:01", "—"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			a.pl.Cue([]netease.Track{{ID: 7, Duration: tc.duration}})
			a.pl.Hold(time.Hour + time.Minute + time.Second + 500*time.Millisecond)
			tt := ui.NewTester(a.nowPlayingTime, 300, 24)
			tt.Frame()
			texts := tt.Texts()
			if !slices.Contains(texts, tc.elapsed) || !slices.Contains(texts, tc.remaining) {
				t.Fatalf("progress labels should show %q and %q, got %q", tc.elapsed, tc.remaining, texts)
			}
		})
	}
}

// The visible clocks refresh at playback boundaries even if the scrubber does not rebuild the view.
func TestNowPlayingTimeRefreshesWithoutScrubber(t *testing.T) {
	a := newTestApp()
	a.pl.Cue([]netease.Track{{ID: 7, Duration: 10_500}})
	a.pl.Hold(2*time.Second + 250*time.Millisecond)
	tt := ui.NewTester(a.nowPlayingTime, 300, 24)
	check := func(elapsed, remaining string) {
		t.Helper()
		texts := tt.Texts()
		if !slices.Contains(texts, elapsed) || !slices.Contains(texts, remaining) {
			t.Fatalf("progress labels should show %q and %q, got %q", elapsed, remaining, texts)
		}
	}
	check("0:02", "−0:09")
	a.pl.Hold(2*time.Second + 500*time.Millisecond)
	tt.Advance(250 * time.Millisecond)
	check("0:02", "−0:08")
	a.pl.Hold(3 * time.Second)
	tt.Advance(500 * time.Millisecond)
	check("0:03", "−0:08")
}
