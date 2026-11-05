package app

import (
	"testing"
	"time"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"

	"netease-music-mygo/internal/lyrics"
	"netease-music-mygo/internal/netease"
)

// The window shows the line being sung and the ones after it, in either
// arrangement, and none of them lit before the song starts.
func TestLyricWindowLinesFollowTheLineSung(t *testing.T) {
	lines := []lyrics.Line{
		{At: 0, Text: "one"},
		{At: 2 * time.Second, Text: "two"},
		{At: 4 * time.Second, Text: "three"},
		{At: 6 * time.Second, Text: "four"},
		{At: 8 * time.Second, Text: "five"},
	}

	// Set across: the line sung and the next.
	shown, start := lyricWindowLines(lines, 1, false)
	if start != 1 || len(shown) != 2 || shown[0].Text != "two" || shown[1].Text != "three" {
		t.Fatalf("set across, at two: %d from %d", len(shown), start)
	}
	// Stacked: it and the three after it.
	shown, start = lyricWindowLines(lines, 1, true)
	if start != 1 || len(shown) != 4 || shown[3].Text != "five" {
		t.Fatalf("stacked, at two: %d from %d", len(shown), start)
	}
	// The last line shows alone, not past the song's.
	shown, start = lyricWindowLines(lines, 4, true)
	if start != 4 || len(shown) != 1 || shown[0].Text != "five" {
		t.Fatalf("stacked, at five: %d from %d", len(shown), start)
	}
	// Before the first, the first ones show, with none of them lit.
	shown, start = lyricWindowLines(lines, -1, false)
	if start != 0 || len(shown) != 2 || shown[0].Text != "one" {
		t.Fatalf("before the first: %d from %d", len(shown), start)
	}
	if shown, _ := lyricWindowLines(nil, 0, true); shown != nil {
		t.Fatalf("a song without lines: %d", len(shown))
	}
}

// The window stays on the screen, wherever it was left and whatever the
// screen has become since; never placed, it stands over the foot of it.
func TestLyricWindowStaysOnScreen(t *testing.T) {
	area := mygo.Rectangle{X: 0, Y: 25, Width: 1440, Height: 875}

	if x, y := lyricWindowPlace(0, 0, 840, 172, area); x != 300 || y+172 > area.Y+area.Height || y < area.Y {
		t.Errorf("never placed: %d, %d", x, y)
	}
	// Past the right of it, as a screen went away from under the window.
	if x, y := lyricWindowPlace(1600, 400, 840, 172, area); x != area.Width-840 || y != 400 {
		t.Errorf("past the right: %d, %d", x, y)
	}
	// Above it.
	if x, y := lyricWindowPlace(100, -200, 840, 172, area); x != 100 || y != area.Y {
		t.Errorf("past the top: %d, %d", x, y)
	}
	// Larger than the screen: its top left corner.
	if x, y := lyricWindowPlace(300, 300, 2000, 1000, area); x != area.X || y != area.Y {
		t.Errorf("larger than the screen: %d, %d", x, y)
	}
}

// Its own button locks the window, and locked it keeps only the lines: the
// buttons go, so that a click meant for the desktop lands on the desktop.
func TestLockingTheDeskLyricsLeavesOnlyItsLines(t *testing.T) {
	isolateUserData(t)
	a := newTestApp()
	a.settings = defaultSettings()
	a.settings.LyricWindow = true
	a.pl.Cue([]netease.Track{{ID: 1, Name: "时过境迁"}})
	a.pl.SetLyrics([]lyrics.Line{{At: 0, Text: "第一句"}, {At: 2 * time.Second, Text: "第二句"}})
	a.pl.Hold(0)
	d := &deskLyrics{a: a}
	a.lyrics = d

	w, h := lyricWindowSize(false, a.settings.LyricSize)
	tt := ui.NewTester(d.view, int(w), int(h))
	if _, ok := tt.Find("锁定"); !ok {
		t.Fatal("no button to lock it, which it shows while it may be moved")
	}
	if err := tt.Click("锁定"); err != nil {
		t.Fatalf("locking it: %v", err)
	}
	if !a.settings.LyricLocked {
		t.Error("still unlocked after its own button")
	}
	// Pointer off it, as it sits behind what the listener works in.
	tt.Move(-20, -20)
	tt.Frame()
	if _, ok := tt.Find("设置"); ok {
		t.Error("still shows its buttons, locked and not pointed at")
	}
	// Pointed at it, they are back: unlocking has to be possible.
	tt.Move(20, 20)
	tt.Frame()
	if _, ok := tt.Find("解锁"); !ok {
		t.Error("no way back out of the lock, pointed at")
	}
}
