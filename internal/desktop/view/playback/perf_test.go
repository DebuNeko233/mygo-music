/*
验证歌词逐字绘制、虚拟列表和舞台动画的帧预算及空闲调度。
*/

package playback

import (
	"bytes"
	"fmt"
	"image"
	"math"
	"strings"
	"testing"
	"time"

	"github.com/egoist/mygo/ui"

	"netease-music-mygo/internal/captionstore"
	artcache "netease-music-mygo/internal/desktop/artwork"
	"netease-music-mygo/internal/desktop/view/components"
	"netease-music-mygo/internal/lyrics"
	"netease-music-mygo/internal/netease"
	"netease-music-mygo/internal/player"
	"netease-music-mygo/internal/preferences"
	"netease-music-mygo/internal/testutil"
)

// An ending word can finish before the next regular highlight tick without rebuilding the view.
func TestLyricWordFinishesBeforeRegularTick(t *testing.T) {
	a := newTestView(t)
	a.Player().Cue([]netease.Track{{ID: 1, Duration: 8000}})
	line := lyrics.Line{Text: "Quick", Words: []lyrics.Word{
		{Text: "Quick", Start: time.Second, End: 1050 * time.Millisecond},
	}}
	a.Player().Hold(1030 * time.Millisecond)
	builds := 0
	tt := ui.NewTester(func(c *ui.Context) {
		builds++
		c.Root().Background(ui.RGB(0, 0, 0))
		a.PaintLyricWords(c, &line, 200, LinesStyle{Size: 30, Weight: 800, LineHeight: 1.25})
	}, 200, 65)
	partial := tt.Image()
	before := builds

	a.Player().Hold(line.Words[0].End)
	// The last 20 ms of the word should schedule its own paint even
	// though the regular tick is 40 ms away.
	tt.Advance(25 * time.Millisecond)
	finished := tt.Image()
	if builds != before || !pixelsLowerInBand(finished, partial, 0, 65) {
		t.Fatal("the word did not finish on a paint-only frame near its end")
	}
	tt.Frame()
	if settled := tt.Image(); !bytes.Equal(finished.Pix, settled.Pix) {
		t.Fatal("the deadline frame still showed a partially lit word")
	}
}

// Word colors repaint without rebuilding the view, and a gap needs no periodic frame.
func TestLyricWordPaintBudget(t *testing.T) {
	line := lyrics.Line{Text: "Alpha Beta", Words: []lyrics.Word{
		{Text: "Alpha ", Start: time.Second, End: 1500 * time.Millisecond},
		{Text: "Beta", Start: 3 * time.Second, End: 3500 * time.Millisecond},
	}}
	a := newTestView(t)
	a.Player().Cue([]netease.Track{{ID: 1, Duration: 8000}})
	a.Player().Hold(900 * time.Millisecond)
	builds := 0
	tt := ui.NewTester(func(c *ui.Context) {
		builds++
		c.Root().Background(ui.RGB(0, 0, 0))
		a.PaintLyricWords(c, &line, 240, LinesStyle{Size: 30, Weight: 800, LineHeight: 1.25})
	}, 240, 70)
	brightness := func(r image.Rectangle) int {
		img := tt.Image()
		total := 0
		for y := r.Min.Y; y < r.Max.Y; y++ {
			for x := r.Min.X; x < r.Max.X; x++ {
				total += int(img.RGBAAt(x, y).R)
			}
		}
		return total
	}
	left, right := image.Rect(0, 0, 82, 70), image.Rect(100, 0, 240, 70)
	dimLeft, dimRight, before := brightness(left), brightness(right), builds
	if dimLeft == 0 || dimRight == 0 {
		t.Fatal("dim words were not drawn")
	}
	a.Player().Hold(1300 * time.Millisecond)
	tt.Advance(145 * time.Millisecond)
	if got := brightness(left); got <= dimLeft {
		t.Fatalf("word did not brighten: %d -> %d", dimLeft, got)
	}
	if brightness(right) != dimRight || builds != before {
		t.Fatal("highlight changed a later word or rebuilt the view")
	}
	a.Player().Hold(1600 * time.Millisecond)
	tt.Advance(45 * time.Millisecond)
	a.Player().Hold(3400 * time.Millisecond)
	tt.Advance(800 * time.Millisecond)
	if brightness(right) != dimRight {
		t.Fatal("periodic highlight updated during the silent gap")
	}
	tt.Advance(2 * time.Second)
	if brightness(right) <= dimRight || builds != before {
		t.Fatal("the next word did not repaint after the gap")
	}
}

// Turning fog off discards its speed and bass immediately: the old easing
// state repainted the static background for about two seconds after a switch.
func TestStaticBackdropStopsFog(t *testing.T) {
	a := newTestView(t)
	a.Pictures = func([]string) []*artcache.Picture { return nil }
	*a.Config = preferences.Default()
	a.Config.Fog = true
	tt := ui.NewTester(func(c *ui.Context) { a.Backdrop(c, nil, ui.RGB(50, 80, 140)) }, 240, 160)
	// Tester uses software rendering, so seed the moving GPU fog's state
	// before switching modes; no audio device or wall-clock phase is needed.
	bd := &a.BackdropCache
	bd.Speed, bd.Last = 1, time.Unix(1, 0)
	bd.Level.Step(bd.Last, [player.Bands]float32{1}, true)
	a.Config.Fog = false
	tt.Frame()
	at, canvas := bd.At, bd.Canvas
	for range 120 {
		tt.Advance(BackdropFrame)
		tt.Frame()
		// FramePending alone misses delayed Painter.After callbacks, so
		// also verify that the cache and paint timestamp never change.
		if bd.At != at || bd.Canvas != canvas || bd.Speed != 0 || bd.Level != (components.BarState{}) || !bd.Last.IsZero() {
			t.Fatal("static background kept the fog's animation state or repainted")
		}
		if tt.FramePending() {
			t.Fatal("static background requested another frame")
		}
	}
}

// Centering short lyrics must settle after layout, rather than asking for
// another measurement frame throughout playback or after a resize.
func TestShortLyricsFrameBudget(t *testing.T) {
	lines := []lyrics.Line{{Text: "纯音乐，请欣赏"}}
	a, tt := lyricPaneView(t, lines, 420, 600)
	tt.SetPreferences(ui.Preferences{})
	for _, size := range [][2]int{{420, 600}, {280, 350}, {420, 600}} {
		tt.SetSize(size[0], size[1])
		if n := testutil.FramesAt(a.Player(), tt, 30*time.Second); n > 3 {
			t.Fatalf("%v: short lyrics requested %d idle frames", size, n)
		}
	}
}

// Whole-line highlighting stays idle between two LRC timestamps.
func TestImmersiveLRCStaysIdleBetweenLines(t *testing.T) {
	lines := lyrics.Build("[00:00.00]First line\n[00:10.00]Second line\n[00:20.00]Third line", "", "")
	a := newTestView(t)
	a.Config.LyricEdgeFade = true
	a.Player().Cue([]netease.Track{{ID: 1, Duration: 30000}})
	a.Player().SetLyrics(lines)
	a.Player().Hold(2 * time.Second)
	paints := 0
	tt := ui.NewTesterWith(func(c *ui.Context) {
		c.Root().Draw(func(*ui.Painter, ui.Rect) { paints++ })
		a.LyricsPane(c, a.Player().Current(), 110, ui.Rect{}, 0)
	}, 180, 110, ui.TesterOptions{Preferences: ui.Preferences{ReduceMotion: true}})
	before := paints
	tt.Advance(time.Second)
	if paints != before {
		t.Fatalf("a settled LRC line triggered %d extra paints", paints-before)
	}
}

func lyricScrollFixture(t *testing.T, n int) (*ui.Tester, *int, func(time.Duration)) {
	t.Helper()
	a := newTestView(t)
	tr := netease.Track{ID: 1, Duration: int64(n * 2000)}
	a.Player().Cue([]netease.Track{tr})
	lines := make([]lyrics.Line, n)
	for i := range lines {
		lines[i] = lyrics.Line{
			At: time.Duration(i) * 2 * time.Second, Text: fmt.Sprintf("第 %d 句歌词", i),
			Translation: "Translation of the lyrics",
		}
	}
	a.Player().SetLyrics(lines)
	pos := time.Duration(n/2) * 2 * time.Second
	a.Player().Seek(pos)
	builds := 0
	var seekAt time.Duration
	var seekTime time.Time
	tt := ui.NewTester(func(c *ui.Context) {
		builds++
		if !seekTime.IsZero() {
			if left := seekTime.Sub(c.Now()); left > 0 {
				c.After(left)
			} else {
				a.Player().Seek(seekAt)
				seekTime = time.Time{}
			}
		}
		a.LyricsPane(c, a.Player().Current(), 520, ui.Rect{}, 0)
	}, 480, 520)
	tt.Advance(2 * time.Second)
	seek := func(pos time.Duration) {
		seekAt, seekTime = pos, tt.Now().Add(20*time.Millisecond)
		tt.Frame()
	}
	return tt, &builds, seek
}

// Following a new line keeps sliding without rebuilding the lyrics at each refresh.
func TestLyricsScrollFrameBudget(t *testing.T) {
	tt, builds, seek := lyricScrollFixture(t, 120)
	line := func() ui.Rect {
		t.Helper()
		info, ok := tt.AccessByRole(ui.RoleButton, "第 61 句歌词")
		if !ok {
			t.Fatal("next line not laid out")
		}
		return info.Bounds
	}
	start := line()
	seek(122 * time.Second)
	before := *builds
	tt.Advance(150 * time.Millisecond)
	mid := line()
	tt.Advance(1850 * time.Millisecond)
	end := line()
	if mid.Y >= start.Y-5 || mid.Y <= end.Y+5 || math.Abs(float64(end.Y-520*0.3)) > 1 {
		t.Fatalf("line did not glide to the upper third: start %v, mid %v, end %v", start, mid, end)
	}
	if n := *builds - before; n > 6 {
		t.Fatalf("one automatic glide rebuilt the view %d times", n)
	}
}

// Opening and resizing a long transcript should only measure rows near the reader.
func TestLongCaptionMeasurementBudget(t *testing.T) {
	a := captionView(t)
	cs := a.Captions
	cs.Program = a.Player().Current().ProgramID
	cs.Transcript = &captionstore.Transcript{Spans: []captionstore.Span{{0, a.Player().Duration()}}}
	const count = 2000
	for i := range count {
		cs.Lines = append(cs.Lines, lyrics.Line{
			At:   time.Duration(i) * 250 * time.Millisecond,
			Text: fmt.Sprintf("第 %04d 句，%s", i, strings.Repeat("这段播客字幕会换行", 4)),
		})
	}
	const active = count / 2
	a.Player().Seek(cs.Lines[active].At)
	tt := ui.NewTester(func(c *ui.Context) {
		_, h := c.Size()
		a.CaptionsPane(c, a.Player().Current(), h)
	}, 640, 460)

	for _, width := range []int{640, 485, 340} {
		tt.SetSize(width, 460)
		tt.Advance(time.Second)
		row, ok := tt.AccessByRole(ui.RoleButton, cs.Lines[active].Text)
		if !ok || row.Offscreen || math.Abs(float64(row.Bounds.Y-460*0.3)) > 2 {
			t.Fatalf("width %d: active caption lost its reading position: %+v", width, row.Bounds)
		}
		measured := 0
		for _, done := range a.TimedList.Measured {
			if done {
				measured++
			}
		}
		if measured > 80 {
			t.Fatalf("width %d: measured %d rows to show one viewport", width, measured)
		}
	}
}

// Manual scrolling during a glide picks up where the lyrics are painted.
func TestLyricsManualScrollInterruptsGlide(t *testing.T) {
	for _, n := range []int{30, 120} {
		t.Run(fmt.Sprint(n), func(t *testing.T) {
			tt, _, seek := lyricScrollFixture(t, n)
			y := func() float32 {
				t.Helper()
				line, ok := tt.AccessByRole(ui.RoleButton, fmt.Sprintf("第 %d 句歌词", n/2))
				if !ok {
					t.Fatal("the anchor line is missing")
				}
				return line.Bounds.Y
			}
			seek(time.Duration(n+4) * time.Second)
			tt.Advance(100 * time.Millisecond)
			before := y()
			tt.Scroll(260, 170, 0, 32)
			tt.Advance(2 * time.Second)
			if got := y(); math.Abs(float64(got-(before-32))) > 24 {
				t.Fatalf("manual scroll jumped to the automatic destination: %.1f -> %.1f", before, got)
			}
		})
	}
}

// stageClock advances playback with the Tester's clock, counting actual
// view builds, including those due to After rather than FramePending.
func stageClock(t *testing.T, style string, reduced, playing bool, pos time.Duration) (*View, *ui.Tester, *int) {
	t.Helper()
	a := newTestView(t)
	*a.Config = preferences.Default()
	a.Config.HotComments = false
	a.Config.StageStyle, a.Config.StageSparks = style, true
	a.Player().Cue([]netease.Track{{ID: 7, Duration: 30000}})
	a.Player().SetLyrics([]lyrics.Line{
		{At: 2 * time.Second, End: 7 * time.Second, Text: "一 二", Words: []lyrics.Word{
			{Text: "一 ", Start: 2 * time.Second, End: 4 * time.Second},
			{Text: "二", Start: 6 * time.Second, End: 7 * time.Second},
		}},
		{At: 20 * time.Second, End: 21 * time.Second, Text: "三", Words: []lyrics.Word{
			{Text: "三", Start: 20 * time.Second, End: 21 * time.Second},
		}},
	})
	builds := 0
	var start time.Time
	tt := ui.NewTesterWith(func(c *ui.Context) {
		if start.IsZero() {
			start = c.Now()
		}
		builds++
		if playing {
			a.Player().Hold(pos + c.Now().Sub(start))
		} else {
			a.Player().Seek(pos)
		}
		ui.Column(c).Fill().Children(func() { a.Stage(c, a.Player().Current(), ui.RGB(255, 100, 100)) })
	}, 1200, 800, ui.TesterOptions{Preferences: ui.Preferences{ReduceMotion: reduced}})
	return a, tt, &builds
}

// Reduce Motion used to keep rebuilding at display rate while hand-written
// word animations continued. All styles now wake only where text changes.
func TestStageReducedMotionBoundaries(t *testing.T) {
	for _, style := range StageStyles {
		t.Run(style.Label, func(t *testing.T) {
			a, tt, builds := stageClock(t, style.ID, true, true, 0)
			for _, step := range []struct {
				advance time.Duration
				change  bool
			}{
				{time.Second, false}, // intro
				{time.Second, true},  // first line and word
				{time.Second, false}, // held word
				{time.Second, true},  // word ends
				{time.Second, false}, // between words
				{time.Second, true},  // second word
				{time.Second, true},  // second word ends
				{time.Second, true},  // interlude
				{11 * time.Second, false},
				{time.Second, true}, // next line
				{time.Second, true}, // last word ends
				{5 * time.Second, false},
			} {
				before := *builds
				tt.Advance(step.advance)
				n := *builds - before
				if step.change && (n == 0 || n > 2) || !step.change && n != 0 {
					t.Fatalf("after %v: %d builds, change=%v", a.Player().Position(), n, step.change)
				}
			}
		})
	}
}

// The normal stage still moves, a reduced-motion held word or interlude
// sleeps, and a paused stage asks for no rebuilds under either preference.
func TestStageMotionFrameBudget(t *testing.T) {
	for _, style := range StageStyles {
		for _, reduced := range []bool{false, true} {
			for _, playing := range []bool{false, true} {
				for _, pos := range []time.Duration{2500 * time.Millisecond, 12 * time.Second} {
					_, tt, builds := stageClock(t, style.ID, reduced, playing, pos)
					before := *builds
					tt.Advance(time.Second)
					n := *builds - before
					t.Logf("%s reduced=%v playing=%v pos=%v: %d builds/s", style.Label, reduced, playing, pos, n)
					if reduced || !playing {
						if n != 0 {
							t.Errorf("stationary stage rebuilt %d times", n)
						}
					} else if n == 0 {
						t.Error("normal stage stopped updating")
					}
				}
			}
		}
	}
}

// Changing the system preference while Stage is open starts and stops its
// motion without needing to leave the pane or seek.
func TestStageMotionPreferenceChanges(t *testing.T) {
	_, tt, builds := stageClock(t, "tilt", true, true, 2200*time.Millisecond)
	tt.SetPreferences(ui.Preferences{})
	before := *builds
	tt.Advance(200 * time.Millisecond)
	if *builds == before {
		t.Fatal("motion did not resume")
	}
	tt.SetPreferences(ui.Preferences{ReduceMotion: true})
	tt.Advance(100 * time.Millisecond) // let an already queued frame finish
	before = *builds
	tt.Advance(200 * time.Millisecond)
	if n := *builds - before; n != 0 {
		t.Fatalf("reduced motion kept rebuilding: %d", n)
	}
}
