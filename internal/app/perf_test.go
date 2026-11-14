package app

import (
	"bytes"
	"fmt"
	"image"
	"math"
	"strings"
	"testing"
	"time"

	"github.com/egoist/mygo/ui"

	"netease-music-mygo/internal/lyrics"
	"netease-music-mygo/internal/netease"
)

// An ending word can finish before the next regular highlight tick without rebuilding the view.
func TestLyricWordFinishesBeforeRegularTick(t *testing.T) {
	a := newTestApp()
	a.pl.Cue([]netease.Track{{ID: 1, Duration: 8000}})
	line := lyrics.Line{Text: "Quick", Words: []lyrics.Word{
		{Text: "Quick", Start: time.Second, End: 1050 * time.Millisecond},
	}}
	a.pl.Hold(1030 * time.Millisecond)
	builds := 0
	tt := ui.NewTester(func(c *ui.Context) {
		builds++
		c.Root().Background(ui.RGB(0, 0, 0))
		a.paintLyricWords(c, &line, 200, linesStyle{size: 30, weight: 800, lineHeight: 1.25})
	}, 200, 65)
	partial := tt.Image()
	before := builds

	a.pl.Hold(line.Words[0].End)
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
	a := newTestApp()
	a.pl.Cue([]netease.Track{{ID: 1, Duration: 8000}})
	a.pl.Hold(900 * time.Millisecond)
	builds := 0
	tt := ui.NewTester(func(c *ui.Context) {
		builds++
		c.Root().Background(ui.RGB(0, 0, 0))
		a.paintLyricWords(c, &line, 240, linesStyle{size: 30, weight: 800, lineHeight: 1.25})
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
	a.pl.Hold(1300 * time.Millisecond)
	tt.Advance(145 * time.Millisecond)
	if got := brightness(left); got <= dimLeft {
		t.Fatalf("word did not brighten: %d -> %d", dimLeft, got)
	}
	if brightness(right) != dimRight || builds != before {
		t.Fatal("highlight changed a later word or rebuilt the view")
	}
	a.pl.Hold(1600 * time.Millisecond)
	tt.Advance(45 * time.Millisecond)
	a.pl.Hold(3400 * time.Millisecond)
	tt.Advance(800 * time.Millisecond)
	if brightness(right) != dimRight {
		t.Fatal("periodic highlight updated during the silent gap")
	}
	tt.Advance(2 * time.Second)
	if brightness(right) <= dimRight || builds != before {
		t.Fatal("the next word did not repaint after the gap")
	}
}

func lyricScrollFixture(n int) (*ui.Tester, *int, func(time.Duration)) {
	a := newTestApp()
	tr := netease.Track{ID: 1, Duration: int64(n * 2000)}
	a.pl.Cue([]netease.Track{tr})
	lines := make([]lyrics.Line, n)
	for i := range lines {
		lines[i] = lyrics.Line{
			At: time.Duration(i) * 2 * time.Second, Text: fmt.Sprintf("第 %d 句歌词", i),
			Translation: "Translation of the lyrics",
		}
	}
	a.pl.SetLyrics(lines)
	pos := time.Duration(n/2) * 2 * time.Second
	a.pl.Seek(pos)
	builds := 0
	var seekAt time.Duration
	var seekTime time.Time
	tt := ui.NewTester(func(c *ui.Context) {
		builds++
		if !seekTime.IsZero() {
			if left := seekTime.Sub(c.Now()); left > 0 {
				c.After(left)
			} else {
				a.pl.Seek(seekAt)
				seekTime = time.Time{}
			}
		}
		a.lyricsPane(c, a.pl.Current(), 520, ui.Rect{})
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
	tt, builds, seek := lyricScrollFixture(120)
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
	a := captionsTestApp(t, true)
	cs := &a.captions
	cs.program = a.pl.Current().ProgramID
	cs.tr = &transcript{Spans: []span{{0, a.pl.Duration()}}}
	const count = 2000
	for i := range count {
		cs.lines = append(cs.lines, lyrics.Line{
			At:   time.Duration(i) * 250 * time.Millisecond,
			Text: fmt.Sprintf("第 %04d 句，%s", i, strings.Repeat("这段播客字幕会换行", 4)),
		})
	}
	const active = count / 2
	a.pl.Seek(cs.lines[active].At)
	tt := ui.NewTester(func(c *ui.Context) {
		_, h := c.Size()
		a.captionsPane(c, a.pl.Current(), h)
	}, 640, 460)

	for _, width := range []int{640, 485, 340} {
		tt.SetSize(width, 460)
		tt.Advance(time.Second)
		row, ok := tt.AccessByRole(ui.RoleButton, cs.lines[active].Text)
		if !ok || row.Offscreen || math.Abs(float64(row.Bounds.Y-460*0.3)) > 2 {
			t.Fatalf("width %d: active caption lost its reading position: %+v", width, row.Bounds)
		}
		measured := 0
		for _, done := range a.timedList.measured {
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
			tt, _, seek := lyricScrollFixture(n)
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

// The player bar cover asks for frames while fading, then rests.
func TestPlayerBarCoverFrameBudget(t *testing.T) {
	a := newTestApp()
	before, after := "test://before", "test://after"
	for _, url := range []string{before, after} {
		img := image.NewRGBA(image.Rect(0, 0, 2, 2))
		a.art.add(netease.Sized(url, barCoverPixels), &artwork{bmp: ui.NewBitmap(img)})
	}
	url := before
	builds := 0
	tt := ui.NewTester(func(c *ui.Context) {
		builds++
		a.playerBarArtwork(c, url)
	}, 64, 64)
	tt.Advance(time.Second)
	url = after
	tt.Frame()
	if !tt.FramePending() {
		t.Fatal("a new cover did not schedule animation frames")
	}
	tt.Advance(time.Second)
	resting := builds
	tt.Advance(time.Second)
	if tt.FramePending() || builds != resting {
		t.Fatal("a finished cover transition kept scheduling frames")
	}
}

// A cover slide moves through painter poses, then releases its outgoing
// layer and stops rebuilding. Count After wakeups as well as display frames.
func TestCoverSlideFrameBudget(t *testing.T) {
	for _, reduced := range []bool{false, true} {
		a := newTestApp()
		a.pl.Cue([]netease.Track{{ID: 1}, {ID: 2}, {ID: 3}})
		builds := 0
		tt := ui.NewTesterWith(func(c *ui.Context) {
			builds++
			ui.Column(c).Fill().Center().Children(func() { a.npCover(c, a.pl.Current(), nil, 220) })
		}, 320, 320, ui.TesterOptions{Preferences: ui.Preferences{ReduceMotion: reduced}})
		a.pl.Next()
		tt.Frame()
		tt.Advance(time.Second)
		if a.npCoverState.switching || a.npCoverState.previous != nil {
			t.Fatal("the outgoing cover outlived its animation")
		}
		before := builds
		tt.Advance(time.Second)
		if n := builds - before; n != 0 {
			t.Errorf("reduced=%v: a resting cover rebuilt %d times", reduced, n)
		}
	}
}

// playingApp is an app playing a track with lyrics, at a position the test
// sets with pl.Seek, and the number of times its view was built.
func playingApp() (*app, *ui.Tester, *int) {
	a := newTestApp()
	a.settings = defaultSettings()
	tr := netease.Track{ID: 7, Name: "宝船", Artists: []netease.Artist{{Name: "DOUDOU"}}, Duration: 200000}
	a.pl.Cue([]netease.Track{tr})
	// A line every 10 seconds, sung over the first 5 of them.
	var lrc string
	for i := range 12 {
		lrc += "[0" + string(rune('0'+i*10/60)) + ":" + string(rune('0'+i*10%60/10)) + "0.00]一二三四五六\n"
	}
	lines := lyrics.Build(lrc, "", "")
	for i := range lines {
		l := &lines[i]
		l.End = l.At + 5*time.Second
		l.Words = lyrics.SpreadWords(l.Text, l.At, l.End)
	}
	a.pl.SetLyrics(lines)
	a.pl.Hold(0)
	builds := 0
	tt := ui.NewTester(func(c *ui.Context) { builds++; a.view(c) }, 1200, 800)
	return a, tt, &builds
}

// framesAt counts the frames the view asks for over a second, at 60 a
// second, with playback held at pos.
func framesAt(a *app, tt *ui.Tester, pos time.Duration) int {
	a.pl.Seek(pos)
	tt.Frame()
	tt.Advance(time.Second)
	tt.Frame()
	asked := 0
	for range 60 {
		tt.Advance(time.Second / 60)
		if tt.FramePending() {
			asked++
			tt.Frame()
		}
	}
	return asked
}

// Now Playing asks for frames only as fast as what it shows changes. The
// Tester runs timers as it settles a frame, so this counts only what does
// not change for a while: a frame log of the app (MYGO_FRAME_STATS) measures
// the rest.
func TestNowPlayingFrameBudget(t *testing.T) {
	a, tt, _ := playingApp()
	if n := framesAt(a, tt, 12*time.Second); n > 3 {
		t.Errorf("a page under the player: %d frames a second", n)
	}
	// Its rows still, the wall asks for no more than the lyrics beside it
	// do: it moves only as it is moved.
	tt.SetPreferences(ui.Preferences{ReduceMotion: true})
	a.nowPlaying, a.npPane = true, paneLyrics
	lyricsFrames := framesAt(a, tt, 12*time.Second)
	a.npPane = paneWall
	if n := framesAt(a, tt, 12*time.Second); n > lyricsFrames+3 {
		t.Errorf("the wall, still: %d frames a second, the lyrics %d", n, lyricsFrames)
	}
	tt.SetPreferences(ui.Preferences{})
	a.npPane = paneStage
	a.settings.StageSparks = true
	for _, s := range stageStyles {
		a.settings.StageStyle = s.id
		if n := framesAt(a, tt, 18*time.Second); n > 12 {
			t.Errorf("the stage between lines, in %s: %d frames a second", s.label, n)
		}
	}
}

// Another song's backdrop fades in over the last one's, which is then let
// go, so that the backdrop stops asking for frames.
func TestBackdropFadeEnds(t *testing.T) {
	a, tt, _ := coverApp(t)
	a.art.art[netease.Sized("a", 600)+"#backdrop"] = &artwork{tint: ui.RGB(200, 40, 40)}
	a.art.art[netease.Sized("b", 600)+"#backdrop"] = &artwork{tint: ui.RGB(40, 40, 200)}
	q := a.pl.Queue()
	q[1].Album.PicURL, q[2].Album.PicURL = "a", "b"
	tt.Frame()
	a.pl.Next()
	tt.Frame()
	if a.backdropCache.before == nil {
		t.Fatal("the backdrop changed songs without fading")
	}
	tt.Advance(backdropFade + 100*time.Millisecond)
	tt.Frame()
	if a.backdropCache.before != nil {
		t.Error("the last song's backdrop still fades after the fade")
	}
}

// Turning fog off discards its speed and bass immediately: the old easing
// state repainted the static background for about two seconds after a switch.
func TestStaticBackdropStopsFog(t *testing.T) {
	a := newTestApp()
	a.settings = defaultSettings()
	a.settings.Fog = true
	tt := ui.NewTester(func(c *ui.Context) { a.backdrop(c, nil, ui.RGB(50, 80, 140)) }, 240, 160)
	// Tester uses software rendering, so seed the moving GPU fog's state
	// before switching modes; no audio device or wall-clock phase is needed.
	bd := &a.backdropCache
	bd.speed, bd.last = 1, time.Unix(1, 0)
	bd.level.h[0], bd.level.last = 1, bd.last
	a.settings.Fog = false
	tt.Frame()
	at, canvas := bd.at, bd.canvas
	for range 120 {
		tt.Advance(backdropFrame)
		tt.Frame()
		// FramePending alone misses delayed Painter.After callbacks, so
		// also verify that the cache and paint timestamp never change.
		if bd.at != at || bd.canvas != canvas || bd.speed != 0 || bd.level != (barState{}) || !bd.last.IsZero() {
			t.Fatal("static background kept the fog's animation state or repainted")
		}
		if tt.FramePending() {
			t.Fatal("static background requested another frame")
		}
	}
}

// A paused playlist must stop scheduling frames with its glass locate
// button visible, after locating hides it, and after its header changes.
func TestPlaylistLocateFrameBudget(t *testing.T) {
	_, pl, tt := locateFixture(t, 199, false)
	for _, stage := range []string{"offscreen", "visible", "header changed"} {
		switch stage {
		case "visible":
			clickLocate(t, tt)
		case "header changed":
			pl.Description = "A refreshed playlist description."
			tt.Frame()
		}
		tt.Move(100, 100)
		tt.Advance(time.Second)
		tt.Frame()
		tt.Frame()
		asked := 0
		for range 30 {
			tt.Advance(time.Second / 60)
			if tt.FramePending() {
				asked++
				tt.Frame()
			}
		}
		if asked > 1 {
			t.Fatalf("%s idle locate requested %d frames", stage, asked)
		}
	}
}

// Centering short lyrics must settle after layout, rather than asking for
// another measurement frame throughout playback or after a resize.
func TestShortLyricsFrameBudget(t *testing.T) {
	lines := []lyrics.Line{{Text: "纯音乐，请欣赏"}}
	a, tt := shortLyricsApp(lines, 420, 600)
	tt.SetPreferences(ui.Preferences{})
	for _, size := range [][2]int{{420, 600}, {280, 350}, {420, 600}} {
		tt.SetSize(size[0], size[1])
		if n := framesAt(a, tt, 30*time.Second); n > 3 {
			t.Fatalf("%v: short lyrics requested %d idle frames", size, n)
		}
	}
}

func hotCommentStageClock(style string, reduced bool, pos time.Duration) (*app, *ui.Tester, *int) {
	a, _, _ := quietApp(pos)
	a.settings.StageStyle = style
	builds := 0
	var start time.Time
	tt := ui.NewTesterWith(func(c *ui.Context) {
		if start.IsZero() {
			start = c.Now()
		}
		builds++
		a.pl.Hold(pos + c.Now().Sub(start))
		ui.Column(c).Fill().Children(func() { a.stage(c, a.pl.Current(), ui.RGB(255, 100, 100)) })
	}, 1200, 800, ui.TesterOptions{Preferences: ui.Preferences{ReduceMotion: reduced}})
	return a, tt, &builds
}

// Reduce Motion must cover hot-comment entry, replacement and exit too:
// comments appear centered immediately, without fades, movement or frame loops.
func TestStageHotCommentsReducedMotion(t *testing.T) {
	for _, style := range stageStyles {
		t.Run(style.label, func(t *testing.T) {
			a, tt, builds := hotCommentStageClock(style.id, true, 0)
			elapsed := time.Duration(0)
			for _, step := range []struct {
				at time.Duration
				id int64
			}{{1250 * time.Millisecond, 1}, {10150 * time.Millisecond, 2}, {19050 * time.Millisecond, 0}} {
				tt.Advance(step.at - elapsed)
				elapsed = step.at
				card, shown := tt.Find("查看评论")
				if step.id == 0 {
					if shown {
						t.Fatal("comment lingered after the quiet ended")
					}
					continue
				}
				if !shown || a.quiet.cm.ID != step.id || math.Abs(float64(card.Y-(800-card.H)/2)) > 0.5 {
					t.Fatalf("comment did not appear immediately in place: id=%d bounds=%v shown=%v", step.id, card, shown)
				}
				before := *builds
				tt.Advance(200 * time.Millisecond)
				elapsed += 200 * time.Millisecond
				if *builds != before {
					t.Fatalf("static hot comment requested %d extra builds", *builds-before)
				}
			}
		})
	}
}
