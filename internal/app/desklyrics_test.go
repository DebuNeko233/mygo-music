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

// A newly opened window is horizontally centred, its centre 75% down the
// screen; a remembered one stays on the screen when its display has changed.
func TestLyricWindowStaysOnScreen(t *testing.T) {
	area := mygo.Rectangle{X: 0, Y: 25, Width: 1440, Height: 875}

	if x, y := lyricWindowPlace(0, 0, 840, 172, area); x != 300 || y != area.Y+area.Height*3/4-86 {
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

	w, h := lyricWindowSize(a.settings.LyricSize, a.settings.LyricLines)
	tt := ui.NewTester(d.view, int(w), int(h))
	// Pointed at, the buttons show: they are how the window is locked. The
	// window itself is not there: the words stand on what is under them.
	tt.Move(w/2, h/2)
	tt.Frame()
	if _, ok := tt.Find("锁定"); !ok {
		t.Fatal("no button to lock it, which it shows while it may be moved")
	}
	if alpha := tt.Image().RGBAAt(4, 4).A; alpha != 51 {
		t.Errorf("hovering the unlocked window: alpha %d, want 20%%", alpha)
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
	// The padlock is in the lyrics' own window, the one part of it that
	// takes the pointer: no little window of its own.
	d.hot = true
	tt.Move(-20, -20)
	tt.Frame()
	if _, ok := tt.Find("设置"); ok {
		t.Error("the unlocked buttons show beside the padlock")
	}
	if err := tt.Click("解锁"); err != nil {
		t.Fatalf("unlocking it: %v", err)
	}
	if a.settings.LyricLocked {
		t.Error("still locked after the click on its padlock")
	}
}

// The words stand in the window's middle, however large it was resized: the
// buttons over them do not make their padding uneven.
func TestDeskLyricsAreVerticallyCentered(t *testing.T) {
	a := newTestApp()
	a.settings = defaultSettings()
	a.pl.Cue([]netease.Track{{ID: 1, Name: "时过境迁"}})
	a.pl.SetLyrics([]lyrics.Line{{At: 0, Text: "第一句"}, {At: 2 * time.Second, Text: "第二句"}})
	a.pl.Hold(0)
	tt := ui.NewTester((&deskLyrics{a: a}).view, 840, 400)
	tt.Frame()
	first, ok := tt.Find("第一句")
	if !ok {
		t.Fatal("no line to place")
	}
	second, ok := tt.Find("第二句")
	if !ok {
		t.Fatal("no second line to place")
	}
	middle := (first.Y + second.Y + second.H) / 2
	if diff := middle - 200; diff < -1 || diff > 1 {
		t.Errorf("words centered at %.1f in a 400-high window", middle)
	}
}

// A transparent resize edge keeps the opposite side still, and never makes
// the window smaller than the settings' minimum.
func TestTransparentResizeEdgesKeepTheirOppositeSide(t *testing.T) {
	r := &lyricResize{bounds: mygo.Rectangle{X: 100, Y: 200, Width: 700, Height: 300}, x: 0, y: 0, left: true, top: true}
	got := r.moved(50, 60, 600, 240)
	want := (mygo.Rectangle{X: 150, Y: 260, Width: 650, Height: 240})
	if got != want {
		t.Fatalf("dragging top-left: got %+v, want %+v", got, want)
	}
	got = r.moved(200, 200, 600, 240)
	if got.X != 200 || got.Y != 260 || got.Width != 600 || got.Height != 240 {
		t.Errorf("minimum size moved the opposite side: %+v", got)
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

	settings := ui.NewTester(a.settingsView, 560, 3400)
	settings.Frame()
	if err := settings.Click("单行"); err != nil {
		t.Fatalf("choosing one line: %v", err)
	}
	if a.settings.LyricLines != 1 {
		t.Fatalf("lines: %d", a.settings.LyricLines)
	}

	shown := func() *ui.Tester {
		w, h := lyricWindowSize(a.settings.LyricSize, a.settings.LyricLines)
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

// The settings offer the fonts the system has, store one as the system names
// it, and keep the system's own as no name, so that the lines take the
// system's font again.
func TestTheSettingsChooseAFontTheSystemHas(t *testing.T) {
	isolateUserData(t)
	a := newTestApp()
	a.settings = defaultSettings()
	names := lyricFontNames()
	if len(names) < 2 {
		t.Skip("this system lists no fonts")
	}
	font := names[1]

	settings := ui.NewTester(a.settingsView, 560, 3400)
	settings.Frame()
	if err := settings.Click("选择字体"); err != nil {
		t.Fatalf("opening the fonts: %v", err)
	}
	if !settings.HasText(font) {
		t.Fatalf("the fonts offered show none of the system's")
	}
	if err := settings.Click(font); err != nil {
		t.Fatalf("choosing %s: %v", font, err)
	}
	if a.settings.LyricFont != font {
		t.Fatalf("font: %q", a.settings.LyricFont)
	}

	// The system's own is the first of them, and names no font.
	if err := settings.Click("选择字体"); err != nil {
		t.Fatalf("opening the fonts again: %v", err)
	}
	settings.Key(0, ui.KeyDown)
	settings.Key(0, ui.KeyEnter)
	if a.settings.LyricFont != "" {
		t.Fatalf("font: %q, with the system's own chosen", a.settings.LyricFont)
	}
}

// A font setting stands for the families an older settings file named by a
// name of the app's, for the family the system gives, and for none at all
// when it names the system's own.
func TestTheFamiliesAFontSettingStandsFor(t *testing.T) {
	if got := lyricFamily(lyricFontSystem); got != "" {
		t.Errorf("the system's own stands for %q", got)
	}
	if got := lyricFamily(""); got != "" {
		t.Errorf("no choice stands for %q", got)
	}
	for old, families := range lyricOldFonts {
		if got := lyricFamily(old); got != families {
			t.Errorf("%q stands for %q", old, got)
		}
	}
	names := lyricFontNames()
	if len(names) > 1 {
		if got := lyricFamily(names[1]); got != names[1] {
			t.Errorf("the system's font %q stands for %q", names[1], got)
		}
	}
}
