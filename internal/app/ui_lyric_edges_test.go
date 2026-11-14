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
			viewport.Effect(lyricEdgeFade{viewport: viewport, pass: &pass})
		}, 64, 128)
		return tt.Image()
	}
	exact, padded := render(128), render(192)
	if !bytes.Equal(exact.Pix, padded.Pix) {
		t.Fatal("the viewport edge sampled the intermediate canvas padding")
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
			viewport.Effect(lyricEdgeFade{viewport: viewport, active: active, pass: &pass})
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
		viewport.Effect(lyricEdgeFade{viewport: viewport, active: active, pass: &pass})
	}, 64, 150)
	frame := tt.Image()
	if past, upcoming := frame.RGBAAt(32, 30).R, frame.RGBAAt(32, 78).R; past >= upcoming {
		t.Fatalf("upcoming row should stay clearer: past=%d, upcoming=%d", past, upcoming)
	}
}

// The immersive lyric viewport extends from the controls toward both window edges on resize.
func TestImmersiveLyricViewportTracksControls(t *testing.T) {
	a := tallLyricsApp(t)
	a.settings.LyricEdgeFade = true
	a.settings.HotComments = false
	// One line exercises the same viewport without paying for the blur in the test.
	a.pl.SetLyrics([]lyrics.Line{{Text: "A short lyric"}})
	tt := ui.NewTesterWith(a.nowPlayingView, 1200, 800, ui.TesterOptions{Preferences: ui.Preferences{ReduceMotion: true}})
	for _, size := range [][2]int{{1200, 800}, {880, 580}, {1920, 1080}, {750, 1330}, {880, 580}, {1200, 800}} {
		tt.SetSize(size[0], size[1])
		tab, okTab := tt.AccessByRole(ui.RoleButton, "歌词")
		volume, okVolume := tt.AccessByRole(ui.RoleSlider, "音量")
		view, okView := tt.AccessByRole(ui.RoleScroll, "歌词")
		if !okTab || !okVolume || !okView {
			t.Fatalf("at %v: missing tab (%v), volume (%v), or lyric viewport (%v)", size, okTab, okVolume, okView)
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
		wantTop := max(min(tab.Bounds.Y-extension, 32), 16)
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
	tt.Frame()
	if a.lyricBlurPass == nil {
		t.Fatal("canvas was discarded while immersive lyrics were open")
	}
	a.settings.LyricEdgeFade = false
	tt.Frame()
	if a.lyricBlurPass != nil {
		t.Fatal("canvas was retained after disabling immersive lyrics")
	}
	a.settings.LyricEdgeFade = true
	a.lyricBlurPass = ui.NewCanvas(1, 1)
	a.npPane = paneQueue
	tt.Frame()
	if a.lyricBlurPass != nil {
		t.Fatal("canvas was retained after switching panes")
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
				ui.Column(c).Fill().Children(func() { a.lyricsPane(c, a.pl.Current(), h) })
			}, 280, 320, ui.TesterOptions{Preferences: ui.Preferences{ReduceMotion: true}})
			before := a.lyricScroll.Y
			if before <= 0 {
				t.Fatal("lyrics did not follow playback")
			}
			dir := os.Getenv("NM_FRAMES")
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
