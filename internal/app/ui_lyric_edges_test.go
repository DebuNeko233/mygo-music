package app

import (
	"bytes"
	"fmt"
	"image"
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

// Growing the backing texture must not change the blur at the viewport edge.
func TestLyricBlurIgnoresCanvasPadding(t *testing.T) {
	var pass *ui.Canvas
	render := func(canvasHeight int) *image.RGBA {
		pass = ui.NewCanvas(64, canvasHeight)
		tt := ui.NewTester(func(c *ui.Context) {
			c.Root().Background(ui.RGB(0, 0, 0))
			viewport := ui.Box(c).Fill()
			viewport.Children(func() {
				ui.Box(c).Absolute().Bottom(0).Width(64).Height(1).Background(white)
			})
			viewport.Effect(lyricEdgeFade{pass: &pass})
		}, 64, 128)
		return tt.Image()
	}
	exact, padded := render(128), render(192)
	if !bytes.Equal(exact.Pix, padded.Pix) {
		t.Fatal("the viewport edge sampled the intermediate canvas padding")
	}
}

// The scrollbar gutter keeps the original pixels in both automatic and manual scrolling.
func TestLyricScrollbarGutterStaysClear(t *testing.T) {
	for _, browsing := range []bool{false, true} {
		t.Run(fmt.Sprint(browsing), func(t *testing.T) {
			const width, height, gutter = 80, 100, 16
			var pass *ui.Canvas
			enabled := false
			tt := ui.NewTester(func(c *ui.Context) {
				c.Root().Background(ui.RGB(0, 0, 0))
				viewport := ui.Box(c).Fill()
				viewport.Children(func() {
					ui.Box(c.Key("thumb")).Absolute().Right(3).Top(1).Size(6, 42).Radius(3).Background(white)
					ui.Box(c.Key("ink")).Absolute().Right(gutter).Top(0).Size(14, 14).Background(white)
				})
				if enabled {
					viewport.Effect(lyricEdgeFade{pass: &pass, browsing: browsing, gutter: gutter})
				}
			}, width, height)
			plain := tt.Image()
			enabled = true
			tt.Frame()
			filtered := tt.Image()
			for y := 0; y < height; y++ {
				for x := width - gutter; x < width; x++ {
					if plain.RGBAAt(x, y) != filtered.RGBAAt(x, y) {
						t.Fatalf("gutter pixel changed at %d,%d", x, y)
					}
				}
			}
			if filtered.RGBAAt(width-gutter-6, 1).R >= plain.RGBAAt(width-gutter-6, 1).R {
				t.Fatal("lyric ink next to the gutter did not fade")
			}
		})
	}
}

// The active row stays sharp while adjacent rows spread and fade near the viewport edges.
func TestLyricEdgeFadeCoverage(t *testing.T) {
	const w, h = 64, 150
	enabled := false
	var pass *ui.Canvas
	tt := ui.NewTester(func(c *ui.Context) {
		c.Root().Background(ui.RGB(0, 0, 0))
		viewport := ui.Box(c).Fill()
		var active ui.Element
		viewport.Children(func() {
			for _, y := range []int{4, 40, 75, 110, 145} {
				line := ui.Box(c.Key(y)).Absolute().Top(float32(y - 8)).Left(0).Right(0).Height(16).
					Draw(func(p *ui.Painter, r ui.Rect) {
						p.Fill(ui.Rect{X: r.X + 16, Y: r.Y + 7, W: 32, H: 2}, white, 0)
					})
				if y == 75 {
					active = line
				}
			}
		})
		if enabled {
			viewport.Effect(lyricEdgeFade{clearBand: lyricClearBand(viewport, active, ui.Element{}, nil), pass: &pass})
		}
	}, w, h)
	for _, scale := range []int{1, 2} {
		t.Run(fmt.Sprintf("%dx", scale), func(t *testing.T) {
			enabled = false
			tt.SetScale(float32(scale))
			tt.Frame()
			checkLyricBlurCoverage(t, tt, scale, w, h, func() { enabled = true })
		})
	}
}

func checkLyricBlurCoverage(t *testing.T, tt *ui.Tester, scale, w, h int, enable func()) {
	t.Helper()
	plain := tt.Image()
	enable()
	tt.Frame()
	effect := tt.Image()
	mass := func(img *image.RGBA, y int) int {
		total := 0
		for j := max(0, y-7) * scale; j < min(h, y+7)*scale; j++ {
			for x := 0; x < w*scale; x++ {
				total += int(img.RGBAAt(x, j).R)
			}
		}
		return total
	}
	for y := 69 * scale; y < 81*scale; y++ {
		for x := 0; x < w*scale; x++ {
			if effect.RGBAAt(x, y) != plain.RGBAAt(x, y) {
				t.Fatal("current row went through the blur")
			}
		}
	}
	for _, y := range []int{40, 110} {
		if plain.RGBAAt(32*scale, (y-3)*scale).R != 0 || effect.RGBAAt(32*scale, (y-3)*scale).R == 0 {
			t.Fatalf("adjacent row at %d did not spread", y)
		}
	}
	// The rectangle starts at x=16. Ink above and left of its corner
	// comes from the Gaussian's diagonal samples, which a cross lacks.
	axis := effect.RGBAAt(18*scale, 37*scale).R
	diagonal := effect.RGBAAt(14*scale, 37*scale).R
	if diagonal == 0 || diagonal >= axis {
		t.Fatalf("expected a weaker diagonal contribution: diagonal=%d, axis=%d", diagonal, axis)
	}
	if mass(effect, 4) >= mass(effect, 40) || mass(effect, 145) >= mass(effect, 110) {
		t.Fatal("rows at the edges did not fade")
	}
}

// Two rows equally far from the active row blur at different rates when it sits above center.
func TestLyricBlurRampsFromActiveRow(t *testing.T) {
	var pass *ui.Canvas
	tt := ui.NewTester(func(c *ui.Context) {
		c.Root().Background(ui.RGB(0, 0, 0))
		viewport := ui.Box(c).Fill()
		var active ui.Element
		viewport.Children(func() {
			active = ui.Box(c.Key("active")).Absolute().Top(48).Height(12).Width(64)
			for _, y := range []int{30, 78} {
				ui.Box(c.Key(y)).Absolute().Top(float32(y)).Width(64).Height(1).Background(white)
			}
		})
		viewport.Effect(lyricEdgeFade{clearBand: lyricClearBand(viewport, active, ui.Element{}, nil), pass: &pass})
	}, 64, 150)
	frame := tt.Image()
	if past, upcoming := frame.RGBAAt(32, 30).R, frame.RGBAAt(32, 78).R; past >= upcoming {
		t.Fatalf("upcoming row should stay clearer: past=%d, upcoming=%d", past, upcoming)
	}
}

// Hover clears the blur while preserving the edge fade and the current line.
func TestLyricHoverKeepsEdgeFade(t *testing.T) {
	for _, browsing := range []bool{false, true} {
		t.Run(fmt.Sprint(browsing), func(t *testing.T) {
			a := newTestApp()
			tt := ui.NewTester(func(c *ui.Context) {
				c.Root().Background(ui.RGB(0, 0, 0))
				viewport := ui.Box(c).Fill()
				var active, hovered ui.Element
				viewport.Children(func() {
					for _, y := range []int{4, 48, 91} {
						row := ui.ButtonBase(c.Key(y)).Absolute().Top(float32(y - 3)).Width(64).Height(7).
							Draw(func(p *ui.Painter, r ui.Rect) {
								p.Fill(ui.Rect{X: r.X, Y: r.Y + 3, W: r.W, H: 1}, white, 0)
							})
						if y == 48 {
							active = row
						}
						if row.Hovered() {
							hovered = row
						}
					}
				})
				a.lyricEdges(viewport, active, hovered, ui.Element{}, ui.Element{}, nil, browsing, [2]ui.Element{}, 0)
			}, 64, 96)
			before := tt.Image().RGBAAt(32, 4).R
			tt.Move(32, 4)
			frame := tt.Image()
			hover := frame.RGBAAt(32, 4).R
			if hover == 0 || hover >= 255 || frame.RGBAAt(32, 5).R != 0 {
				t.Fatal("the hovered edge row should be sharp and stay faded")
			}
			if browsing && hover != before || !browsing && hover <= before {
				t.Fatalf("unexpected hover change from %d to %d (browsing=%v)", before, hover, browsing)
			}
			if frame.RGBAAt(32, 48).R != 255 || frame.RGBAAt(32, 91).R >= 255 {
				t.Fatal("hover changed the active row or cleared the other edge")
			}
			tt.Move(32, 30)
			if got := tt.Image().RGBAAt(32, 4).R; got != before {
				t.Fatalf("leaving the row did not restore its fade: got %d, want %d", got, before)
			}
		})
	}
}

// Opening credits fade by position while scrolling, without blurring their strokes.
func TestLyricIntroCreditsFadeWhileScrolling(t *testing.T) {
	for _, browsing := range []bool{false, true} {
		t.Run(fmt.Sprint(browsing), func(t *testing.T) {
			a := newTestApp()
			opening := true
			y := float32(32)
			tt := ui.NewTester(func(c *ui.Context) {
				c.Root().Background(ui.RGB(0, 0, 0))
				viewport := ui.Box(c).Fill()
				var credits [2]ui.Element
				viewport.Children(func() {
					for i, top := range []float32{y, y + 9, 91} {
						row := ui.Box(c.Key(i)).Absolute().Top(top - 2).Width(64).Height(5).
							Draw(func(p *ui.Painter, r ui.Rect) {
								p.Fill(ui.Rect{X: r.X, Y: r.Y + 2, W: r.W, H: 1}, white, 0)
							})
						if opening && i < len(credits) {
							credits[i] = row
						}
					}
				})
				a.lyricEdges(viewport, ui.Element{}, ui.Element{}, ui.Element{}, ui.Element{}, nil, browsing, credits, 0)
			}, 64, 96)
			if tt.Image().RGBAAt(32, 32).R != 255 {
				t.Fatal("credits should be fully visible away from the edges")
			}
			y = 4
			tt.Frame()
			frame := tt.Image()
			outer, inner := frame.RGBAAt(32, 4).R, frame.RGBAAt(32, 13).R
			if outer == 0 || outer >= inner || inner >= 255 || frame.RGBAAt(32, 91).R >= 255 {
				t.Fatalf("expected a gradual fade at both edges, credits outer=%d inner=%d", outer, inner)
			}
			if frame.RGBAAt(32, 5).R != 0 || frame.RGBAAt(32, 12).R != 0 {
				t.Fatal("fading credits spread into neighboring pixels")
			}
			if !browsing {
				opening = false
				tt.Frame()
				if tt.Image().RGBAAt(32, 4).R >= outer {
					t.Fatal("credits did not return to the normal blur after the intro")
				}
			}
		})
	}
}

// Manual scrolling fades both edges without spreading strokes into adjacent pixels.
func TestLyricManualEdgeFadeKeepsTextSharp(t *testing.T) {
	a := newTestApp()
	tt := ui.NewTester(func(c *ui.Context) {
		c.Root().Background(ui.RGB(0, 0, 0))
		viewport := ui.Box(c).Fill()
		viewport.Children(func() {
			for _, y := range []int{4, 48, 91} {
				ui.Box(c.Key(y)).Absolute().Top(float32(y)).Width(64).Height(1).Background(white)
			}
		})
		a.lyricEdges(viewport, ui.Element{}, ui.Element{}, ui.Element{}, ui.Element{}, nil, true, [2]ui.Element{}, 0)
	}, 64, 96)
	frame := tt.Image()
	if middle := frame.RGBAAt(32, 48).R; middle != 255 {
		t.Fatalf("middle stroke changed opacity: %d", middle)
	}
	for _, y := range []int{4, 91} {
		stroke := frame.RGBAAt(32, y).R
		if stroke == 0 || stroke >= 255 || frame.RGBAAt(32, y-1).R != 0 || frame.RGBAAt(32, y+1).R != 0 {
			t.Fatalf("expected a faded, sharp stroke at y=%d, got %d", y, stroke)
		}
	}
	if a.lyricBlurPass != nil {
		t.Fatal("manual scrolling allocated a Gaussian canvas")
	}
}

// The immersive lyric viewport stays out of the window controls and extends below the volume row.
func TestImmersiveLyricViewportTracksControls(t *testing.T) {
	a := tallLyricsApp(t)
	a.settings.LyricEdgeFade = true
	a.settings.HotComments = false
	// One line exercises the same viewport without paying for the blur in the test.
	a.pl.SetLyrics([]lyrics.Line{{Text: "A short lyric"}})
	var barHeight float32
	tt := ui.NewTesterWith(func(c *ui.Context) {
		barHeight = topBar(c)
		a.nowPlayingView(c)
	}, 1200, 800, ui.TesterOptions{Preferences: ui.Preferences{ReduceMotion: true}})
	for _, size := range [][2]int{{1200, 800}, {880, 580}, {1920, 1080}, {750, 1330}, {880, 580}, {1200, 800}} {
		tt.SetSize(size[0], size[1])
		volume, okVolume := tt.AccessByRole(ui.RoleSlider, "音量")
		view, okView := tt.AccessByRole(ui.RoleScroll, "歌词")
		if !okVolume || !okView {
			t.Fatalf("at %v: missing volume (%v) or lyric viewport (%v)", size, okVolume, okView)
		}
		// The slider and its speaker share a center, and the speaker's
		// hit area sets the height of the volume row.
		speaker, ok := tt.AccessByRole(ui.RoleButton, "静音")
		if !ok {
			t.Fatal("missing volume speaker")
		}
		bottom := max(volume.Bounds.Y+volume.Bounds.H, speaker.Bounds.Y+speaker.Bounds.H)
		const extension = 16
		height := float32(size[1])
		wantTop := barHeight
		wantBottom := min(max(bottom+extension, height-32), height-16)
		if abs32(view.Bounds.Y-wantTop) > 1 || abs32(view.Bounds.Y+view.Bounds.H-wantBottom) > 1 {
			t.Fatalf("at %v: lyrics %v, want top %.1f and bottom %.1f", size, view.Bounds, wantTop, wantBottom)
		}
	}
}

// Turning off immersive lyrics or leaving the pane releases its intermediate canvas.
func TestImmersiveLyricReleasesCanvas(t *testing.T) {
	a := tallLyricsApp(t)
	a.settings.LyricEdgeFade = true
	a.settings.HotComments = false
	a.pl.SetLyrics([]lyrics.Line{{Text: "A short lyric"}})
	tt := ui.NewTesterWith(a.view, 880, 580, ui.TesterOptions{Preferences: ui.Preferences{ReduceMotion: true}})
	a.lyricBlurPass = ui.NewCanvas(1, 1)
	a.settings.LyricEdgeFade = false
	tt.Frame()
	if a.lyricBlurPass != nil {
		t.Fatal("canvas was retained after disabling immersive lyrics")
	}
	a.settings.LyricEdgeFade = true
	a.lyricBlurPass = ui.NewCanvas(1, 1)
	a.playbackState.Pane = paneQueue
	tt.Frame()
	if a.lyricBlurPass != nil {
		t.Fatal("canvas was retained after switching panes")
	}
}

// Switching to lyrics that need no blur releases the canvas while the pane stays open.
func TestLyricCanvasAcrossTrackChanges(t *testing.T) {
	a := newTestApp()
	a.settings.LyricEdgeFade = true
	lines := make([]lyrics.Line, 12)
	for i := range lines {
		lines[i] = lyrics.Line{At: time.Duration(i) * time.Second, Text: fmt.Sprintf("Lyric %d", i)}
	}
	a.pl.Cue([]netease.Track{{ID: 1, Duration: 60000}})
	a.pl.SetLyrics(lines)
	tt := ui.NewTesterWith(func(c *ui.Context) {
		ui.Column(c).Fill().Children(func() { a.lyricsPane(c, a.pl.Current(), 180, ui.Rect{}, 0) })
	}, 160, 180, ui.TesterOptions{Preferences: ui.Preferences{ReduceMotion: true}})
	for i, text := range []string{lyrics.InstrumentalText, "A short lyric"} {
		if a.lyricBlurPass == nil {
			t.Fatal("scrolling lyrics did not allocate a canvas")
		}
		a.pl.Cue([]netease.Track{{ID: int64(i) + 2, Duration: 60000}})
		a.pl.SetLyrics([]lyrics.Line{{Text: text}})
		tt.Frame()
		if a.lyricBlurPass != nil {
			t.Fatalf("canvas was held while displaying %q", text)
		}
		a.pl.SetLyrics(lines)
		tt.Frame()
	}
	if a.lyricBlurPass == nil {
		t.Fatal("canvas was not recreated for scrolling lyrics")
	}
}

// Enabling the edge effect preserves manual scroll and click-to-seek in both list layouts.
func TestLyricEdgeFadeScrolling(t *testing.T) {
	for _, count := range []int{20, 120} {
		t.Run(fmt.Sprint(count), func(t *testing.T) {
			a := newTestApp()
			a.pl.Cue([]netease.Track{{ID: 1, Duration: 200000}})
			lines := make([]lyrics.Line, count)
			for i := range lines {
				lines[i] = lyrics.Line{At: time.Duration(i+1) * time.Second, Text: fmt.Sprintf("第 %02d 句歌词", i), Translation: "The lyrics keep scrolling"}
			}
			a.pl.SetLyrics(lines)
			a.pl.Seek(lines[count/2].At)
			tt := ui.NewTesterWith(func(c *ui.Context) {
				_, h := c.Size()
				c.Root().Background(ui.Hex("#22384b"))
				ui.Column(c).Fill().Children(func() { a.lyricsPane(c, a.pl.Current(), h, ui.Rect{}, 0) })
			}, 280, 320, ui.TesterOptions{Preferences: ui.Preferences{ReduceMotion: true}})
			before := a.lyricScroll.Y
			if before <= 0 {
				t.Fatal("lyrics did not follow playback")
			}
			dir := os.Getenv("NM_FRAMES")
			if dir == "" {
				// The test checks input and scroll state. The pixel tests above
				// still exercise the Gaussian at their original resolution.
				tt.SetScale(0.25)
			}
			save := func(label string) {
				t.Helper()
				if dir == "" {
					return
				}
				f, err := os.Create(filepath.Join(dir, fmt.Sprintf("lyric-edge-%d-%s.png", count, label)))
				if err != nil {
					t.Fatal(err)
				}
				defer f.Close()
				if err := png.Encode(f, tt.Image()); err != nil {
					t.Fatal(err)
				}
			}
			save("off")
			a.settings.LyricEdgeFade = true
			tt.Frame()
			before = a.lyricScroll.Y
			save("on")
			tt.Scroll(180, 180, 0, 36)
			if got := a.lyricScroll.Y; got <= before {
				t.Fatalf("could not scroll with the effect enabled: %v -> %v", before, got)
			}
			save("manual")
			manual := a.lyricScroll.Y
			tt.Advance(lyricBrowseDelay + 100*time.Millisecond)
			if a.lyricBrowseUntil.IsZero() || a.lyricScroll.Y != manual {
				t.Fatal("paused playback did not preserve the reading position")
			}
			tt.Scroll(180, 180, 0, 24)
			manual = a.lyricScroll.Y
			a.pl.Hold(lines[count/2+1].At)
			tt.Frame()
			tt.Advance(3 * time.Second)
			if a.lyricBrowseUntil.IsZero() || a.lyricScroll.Y != manual {
				t.Fatalf("a new active line interrupted manual browsing: %v -> %v", manual, a.lyricScroll.Y)
			}
			tt.Scroll(180, 180, 0, 30)
			tt.Advance(3 * time.Second)
			if a.lyricBrowseUntil.IsZero() {
				t.Fatal("a second scroll did not extend browsing")
			}
			line := lines[count/2+1]
			if err := tt.Click(line.Text); err != nil || a.pl.Position() != line.At {
				t.Fatalf("visible lyric could not seek: %v, position %v", err, a.pl.Position())
			}
			if !a.lyricBrowseUntil.IsZero() {
				t.Fatal("clicking the current line did not restore following")
			}
			tt.Scroll(180, 180, 0, 30)
			tt.Advance(lyricBrowseDelay + 100*time.Millisecond)
			if !a.lyricBrowseUntil.IsZero() {
				t.Fatal("following did not resume after scrolling stopped")
			}
			if dir != "" {
				a.pl.Seek(lines[0].At)
				tt.Frame()
				save("start")
			}
		})
	}
}

// Songs with scrolling lyrics give the first sung line the same anchor as later lines.
func TestImmersiveFirstLyricAnchor(t *testing.T) {
	const h = 180
	for _, count := range []int{12, timedListThreshold} {
		t.Run(fmt.Sprint(count), func(t *testing.T) {
			lines := make([]lyrics.Line, count)
			for i := range lines {
				lines[i] = lyrics.Line{At: time.Duration(i+6) * time.Second, Text: fmt.Sprintf("Lyric %02d", i)}
			}
			a, tt := shortLyricsApp(lines, 160, h)
			a.settings.LyricEdgeFade = true
			tt.Frame()
			first, ok := tt.AccessByRole(ui.RoleButton, lines[0].Text)
			if !ok || first.Bounds.Y < h*0.3+lyricIntroHeight {
				t.Fatalf("first lyric did not start below the anchored intro: %+v", first)
			}
			for _, i := range []int{0, 1, count - 1} {
				a.pl.Hold(lines[i].At)
				tt.Frame()
				row, ok := tt.AccessByRole(ui.RoleButton, lines[i].Text)
				if !ok || abs32(row.Bounds.Y-h*0.3) > 1 {
					t.Fatalf("row %d did not reach the reading position: %+v", i, row)
				}
			}
		})
	}
}

// Reflow in manual mode keeps the first visible row within reach.
func TestLyricManualResizeKeepsReadingPosition(t *testing.T) {
	lines := make([]lyrics.Line, 16)
	for i := range lines {
		lines[i] = lyrics.Line{At: time.Duration(i+1) * time.Second, Text: fmt.Sprintf("Lyric %02d", i),
			Translation: strings.Repeat("A lyric translation wraps as the window gets narrower. ", 3)}
	}
	a, tt := shortLyricsApp(lines, 500, 400)
	tt.SetScale(0.25) // accessibility bounds use DIPs
	a.settings.LyricEdgeFade = true
	a.pl.Hold(lines[5].At)
	tt.Frame()
	tt.Scroll(180, 180, 0, 280)
	firstVisible := func() int {
		for i, line := range lines {
			if row, ok := tt.AccessByRole(ui.RoleButton, line.Text); ok && !row.Offscreen {
				return i
			}
		}
		t.Fatal("no visible lyric row")
		return 0
	}
	before := firstVisible()
	if a.lyricBrowseUntil.IsZero() || before == 0 {
		t.Fatal("manual browsing did not start")
	}
	tt.SetSize(260, 400)
	if got := firstVisible(); got != before {
		t.Fatalf("resize moved the reading position from row %d to %d", before, got)
	}
}

// Player-level seeks restore lyric following in both list layouts, even while paused.
func TestImmersiveLyricsFollowPlayerSeekWhileBrowsing(t *testing.T) {
	for _, count := range []int{20, 120} {
		for _, playing := range []bool{false, true} {
			t.Run(fmt.Sprintf("%d/playing=%v", count, playing), func(t *testing.T) {
				lines := make([]lyrics.Line, count)
				for i := range lines {
					lines[i] = lyrics.Line{At: time.Duration(i) * time.Second, Text: fmt.Sprintf("Lyric %03d", i)}
				}
				a, tt := shortLyricsApp(lines, 240, 220)
				tt.SetScale(0.25)
				a.settings.LyricEdgeFade = true
				if !playing {
					// shortLyricsApp pins a running clock; use a fresh paused player.
					paused := newTestApp().pl
					paused.Cue([]netease.Track{{ID: 1, Duration: 200000}})
					paused.SetLyrics(lines)
					a.pl = paused
				}
				a.pl.Seek(lines[6].At)
				tt.Frame()
				tt.Scroll(120, 120, 0, 80)
				if a.lyricBrowseUntil.IsZero() {
					t.Fatal("manual scroll did not enter browsing mode")
				}
				// System media controls call this same player entry point.
				target := count - 3
				a.pl.Seek(lines[target].At)
				tt.Frame()
				row, ok := tt.AccessByRole(ui.RoleButton, lines[target].Text)
				if !a.lyricBrowseUntil.IsZero() || a.lyricAt != target || !ok || row.Offscreen {
					t.Fatalf("seek did not restore following: row=%+v, active=%d", row, a.lyricAt)
				}
			})
		}
	}
}
