package app

import (
	"testing"
	"time"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"

	"netease-music-mygo/internal/lyrics"
	"netease-music-mygo/internal/netease"
)

// The window shows the line being sung and the ones after it, as many as it
// is set to show, and none of them lit before the song starts.
func TestLyricWindowLinesFollowTheLineSung(t *testing.T) {
	lines := []lyrics.Line{
		{At: 0, Text: "one"},
		{At: 2 * time.Second, Text: "two"},
		{At: 4 * time.Second, Text: "three"},
		{At: 6 * time.Second, Text: "four"},
		{At: 8 * time.Second, Text: "five"},
	}

	// Two lines: the one sung and the next.
	shown, start := lyricWindowLines(lines, 1, 2)
	if start != 1 || len(shown) != 2 || shown[0].Text != "two" || shown[1].Text != "three" {
		t.Fatalf("two lines, at two: %d from %d", len(shown), start)
	}
	// One line: the one sung alone.
	shown, start = lyricWindowLines(lines, 1, 1)
	if start != 1 || len(shown) != 1 || shown[0].Text != "two" {
		t.Fatalf("one line, at two: %d from %d", len(shown), start)
	}
	// The last line shows alone, not past the song's.
	shown, start = lyricWindowLines(lines, 4, 2)
	if start != 4 || len(shown) != 1 || shown[0].Text != "five" {
		t.Fatalf("at five: %d from %d", len(shown), start)
	}
	// Before the first, the first ones show, with none of them lit.
	shown, start = lyricWindowLines(lines, -1, 2)
	if start != 0 || len(shown) != 2 || shown[0].Text != "one" {
		t.Fatalf("before the first: %d from %d", len(shown), start)
	}
	// A song without lines has none to show, and no settings ask for none.
	if shown, _ := lyricWindowLines(nil, 0, 2); shown != nil {
		t.Fatalf("a song without lines: %d", len(shown))
	}
	if shown, _ := lyricWindowLines(lines, 0, 0); len(shown) != 1 {
		t.Fatalf("no lines asked for: %d", len(shown))
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

	w, h := lyricWindowSize(false, a.settings.LyricSize, a.settings.LyricLines)
	tt := ui.NewTester(d.view, int(w), int(h))
	// Pointed at, the buttons show: they are how the window is locked. The
	// window itself is not there: the words stand on what is under them.
	tt.Move(w/2, h/2)
	tt.Frame()
	if _, ok := tt.Find("锁定"); !ok {
		t.Fatal("no button to lock it, which it shows while it may be moved")
	}
	if alpha := tt.Image().RGBAAt(4, 4).A; alpha != 0 {
		t.Error("a plate under the words, which are to stand on what is under them")
	}
	if err := tt.Click("锁定"); err != nil {
		t.Fatalf("locking it: %v", err)
	}
	if !a.settings.LyricLocked {
		t.Error("still unlocked after its own button")
	}
	// Pointer off it, as it sits behind what the listener works in: nothing
	// but the lines is left, not even a plate to see the desktop through.
	tt.Move(-20, -20)
	tt.Frame()
	if _, ok := tt.Find("设置"); ok {
		t.Error("still shows its buttons, locked and not pointed at")
	}
	if alpha := tt.Image().RGBAAt(4, 4).A; alpha != 0 {
		t.Error("a plate left behind, with the pointer away")
	}
	// Pointed at it, they are back: unlocking has to be possible.
	tt.Move(20, 20)
	tt.Frame()
	if _, ok := tt.Find("解锁"); !ok {
		t.Error("no way back out of the lock, pointed at")
	}
}

// A line set vertically stands its Chinese upright, one character under the
// next, and turns the words in it a quarter beside them, as Word does.
func TestALineStandsUprightWithItsWordsTurned(t *testing.T) {
	runs := lyricRuns("我们去看 Hello World 演唱会")
	want := []lyricRun{
		{text: "我", upright: true},
		{text: "们", upright: true},
		{text: "去", upright: true},
		{text: "看", upright: true},
		{text: " Hello World ", upright: false},
		{text: "演", upright: true},
		{text: "唱", upright: true},
		{text: "会", upright: true},
	}
	if len(runs) != len(want) {
		t.Fatalf("%d runs, wanted %d: %+v", len(runs), len(want), runs)
	}
	for i, run := range runs {
		if run != want[i] {
			t.Errorf("run %d: %+v, wanted %+v", i, run, want[i])
		}
	}
}

// The settings choose one line or two, and the window shows that many: the
// one being sung, and the one after it only when two are asked for.
func TestTheSettingsChooseHowManyLinesTheWindowShows(t *testing.T) {
	isolateUserData(t)
	a := newTestApp()
	a.settings = defaultSettings()
	a.pl.Cue([]netease.Track{{ID: 1, Name: "时过境迁"}})
	a.pl.SetLyrics([]lyrics.Line{
		{At: 0, Text: "第一句"},
		{At: 2 * time.Second, Text: "第二句"},
		{At: 4 * time.Second, Text: "第三句"},
	})
	a.pl.Hold(2 * time.Second)

	settings := ui.NewTester(a.settingsView, 560, 2900)
	settings.Frame()
	if err := settings.Click("单行"); err != nil {
		t.Fatalf("choosing one line: %v", err)
	}
	if a.settings.LyricLines != 1 {
		t.Fatalf("lines: %d", a.settings.LyricLines)
	}

	shown := func() *ui.Tester {
		w, h := lyricWindowSize(false, a.settings.LyricSize, a.settings.LyricLines)
		tt := ui.NewTester((&deskLyrics{a: a}).view, int(w), int(h))
		tt.Move(w/2, h/2)
		tt.Frame()
		return tt
	}
	one := shown()
	if _, ok := one.Find("第二句"); !ok {
		t.Error("the line being sung is missing")
	}
	if _, ok := one.Find("第三句"); ok {
		t.Error("shows the line after it, with one line asked for")
	}

	settings.Frame()
	if err := settings.Click("双行"); err != nil {
		t.Fatalf("choosing two lines: %v", err)
	}
	two := shown()
	if _, ok := two.Find("第三句"); !ok {
		t.Error("the line after it is missing, with two asked for")
	}
}
