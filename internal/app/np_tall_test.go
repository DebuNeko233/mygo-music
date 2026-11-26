/*
验证歌词在高窗口、缩放和沉浸模式中的排版、滚动锚点及画布释放。
*/

package app

import (
	"fmt"
	"image"
	"image/png"
	"math"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/egoist/mygo/ui"

	playbackview "netease-music-mygo/internal/desktop/view/playback"
	"netease-music-mygo/internal/desktop/view/shell"
	"netease-music-mygo/internal/lyrics"
	"netease-music-mygo/internal/netease"
	"netease-music-mygo/internal/player"
	"netease-music-mygo/internal/testutil"
	commentsvm "netease-music-mygo/internal/viewmodel/comments"
)

func tallLyricsApp(t *testing.T) *app {
	t.Helper()
	isolateUserData(t)
	a := newTestApp()
	a.settings = defaultSettings()
	a.settings.LyricEdgeFade = false
	player.Cue(a.pl, []netease.Track{{ID: 1, Name: "高窗口布局", Duration: 150000}})
	var lines []lyrics.Line
	for i := range 30 {
		at := time.Duration(i) * 5 * time.Second
		lines = append(lines, lyrics.Line{At: at, End: at + 5*time.Second, Text: fmt.Sprintf("第%02d句", i)})
	}
	player.SetLyrics(a.pl, lines)
	player.Hold(a.pl, 77*time.Second)
	a.playbackState.Open, a.playbackState.Pane = true, playbackview.PaneLyrics
	a.comments = commentsvm.Comments{Track: 1, Loaded: true}
	return a
}

// Instrumental credits use the same reading area when immersive lyrics is toggled.
func TestNowPlayingInstrumentalLayoutMatchesBothModes(t *testing.T) {
	a := tallLyricsApp(t)
	a.settings.HotComments = false
	const credit = "作曲：A"
	player.RefreshLyrics(a.pl, 1, "[00:01]"+credit, "", "")
	tt := ui.NewTesterWith(a.view, 960, 700, ui.TesterOptions{Preferences: ui.Preferences{ReduceMotion: true}})
	tt.SetScale(0.25)
	bounds := func(text string) ui.Rect {
		t.Helper()
		r, ok := tt.Find(text)
		if !ok {
			t.Fatalf("missing %q in the instrumental panel", text)
		}
		return r
	}
	title, creditRow := bounds(lyrics.InstrumentalText), bounds(credit)
	if title.X != creditRow.X || title.Y >= creditRow.Y {
		t.Fatalf("title and credit do not share a left edge: %+v, %+v", title, creditRow)
	}
	a.settings.LyricEdgeFade = true
	tt.Frame()
	if bounds(lyrics.InstrumentalText) != title || bounds(credit) != creditRow {
		t.Fatal("toggling immersive lyrics moved the instrumental panel")
	}
	tt.SetSize(680, 500)
	if resized := bounds(lyrics.InstrumentalText); resized.H > title.H+1 {
		t.Fatalf("the cover's scrollbar inset squeezed the notice in a narrow window: %+v", resized)
	}
}

// Entering or leaving a tall window must not paint lyrics at the previous height.
func TestNowPlayingLyricsUseResizedHeightOnFirstFrame(t *testing.T) {
	for _, immersive := range []bool{false, true} {
		for _, count := range []int{0, 1, 28, playbackview.TimedListThreshold + 1} {
			t.Run(fmt.Sprintf("immersive=%t/rows=%d", immersive, count), func(t *testing.T) {
				a := tallLyricsApp(t)
				a.settings.LyricEdgeFade = immersive
				a.settings.HotComments = false
				text := lyrics.InstrumentalText
				lines := make([]lyrics.Line, count)
				for i := range lines {
					lines[i] = lyrics.Line{At: time.Duration(i) * time.Second, Text: fmt.Sprintf("第%d句", i)}
				}
				if count > 0 {
					row := lines[count/2]
					player.Hold(a.pl, row.At)
					text = row.Text
				}
				player.SetLyrics(a.pl, lines)

				var tt *ui.Tester
				findY := func() float32 {
					t.Helper()
					r, ok := tt.Find(text)
					if !ok {
						t.Fatalf("lyric %q was missing during resize", text)
					}
					return r.Y
				}
				var targetHeight float32
				var resizedBuild bool
				var painted []float32
				tt = ui.NewTesterWith(func(c *ui.Context) {
					_, h := c.Size()
					if h == targetHeight {
						// The tester still exposes the preceding paint when it
						// starts the next build at the new size.
						if resizedBuild {
							painted = append(painted, findY())
						}
						resizedBuild = true
					}
					a.view(c)
				}, 900, 650, ui.TesterOptions{Preferences: ui.Preferences{ReduceMotion: true}})
				tt.SetScale(0.15)
				for _, size := range [][2]int{{1728, 1117}, {900, 650}} {
					targetHeight, resizedBuild, painted = float32(size[1]), false, painted[:0]
					before := findY()
					tt.SetSize(size[0], size[1])
					settled := findY()
					if playbackview.Abs32(settled-before) < 100 {
						t.Fatalf("the resize did not move the lyric from %.1f", before)
					}
					for _, y := range painted {
						if playbackview.Abs32(y-settled) > 1 {
							t.Fatalf("at %v: painted lyric at %.1f before settling at %.1f", size, y, settled)
						}
					}
				}
			})
		}
	}
}

// Opening at the end of a song puts the current lyric at the same inset as a middle verse.
func TestNowPlayingOpensAtLastLyric(t *testing.T) {
	for _, immersive := range []bool{false, true} {
		for _, count := range []int{30, 120} {
			t.Run(fmt.Sprintf("immersive=%t/rows=%d", immersive, count), func(t *testing.T) {
				a := tallLyricsApp(t)
				a.playbackState.Open = false
				a.settings.LyricEdgeFade = immersive
				a.settings.HotComments = false
				lines := make([]lyrics.Line, count)
				for i := range lines {
					lines[i] = lyrics.Line{At: time.Duration(i) * time.Second, Text: fmt.Sprintf("第 %d 句", i)}
					if i%3 == 1 {
						lines[i].Translation = strings.Repeat("换行测试 ", 15)
					}
				}
				last, middle := lines[count-1], lines[count/2]
				player.Cue(a.pl, []netease.Track{{ID: 1, Duration: int64(count+2) * 1000}})
				player.SetLyrics(a.pl, lines)
				// Exercise the animated remount that used to enter browsing
				// mode in long immersive lyrics.
				reduceMotion := !immersive || count < playbackview.TimedListThreshold
				tt := ui.NewTesterWith(a.view, 750, 1330, ui.TesterOptions{Preferences: ui.Preferences{ReduceMotion: reduceMotion}})
				tt.SetScale(0.1) // The test inspects layout, so keep software rasterization small.
				visibleY := func(line lyrics.Line) float32 {
					t.Helper()
					row, ok := tt.AccessByRole(ui.RoleButton, line.Text)
					if !ok || row.Offscreen || !a.lyricReading.BrowseUntil.IsZero() {
						t.Fatalf("playback lost %q after opening: visible=%t offset=%.1f browsing=%t",
							line.Text, ok && !row.Offscreen, a.LyricScroll.Y, !a.lyricReading.BrowseUntil.IsZero())
					}
					return row.Bounds.Y
				}
				openAtEnd := func() float32 {
					player.Hold(a.pl, last.At+2*time.Second)
					a.playbackState.Open = true
					tt.Frame()
					visibleY(last)
					tt.Advance(playbackview.NPSettle)
					return visibleY(last)
				}
				firstOpen := openAtEnd()
				player.Hold(a.pl, middle.At)
				tt.Frame()
				tt.Advance(time.Second)
				want := visibleY(middle)
				a.playbackState.Open = false
				tt.Frame()
				tt.Advance(playbackview.NPSettle)
				reopened := openAtEnd()
				if playbackview.Abs32(firstOpen-want) > 2 || playbackview.Abs32(reopened-want) > 2 {
					t.Fatalf("last lyric at %.1f on first open and %.1f on reopen, want %.1f", firstOpen, reopened, want)
				}
			})
		}
	}
}

// The visible lyric thumb mirrors the cover's left edge and can drag the pane in either mode.
func TestNowPlayingLyricScrollbarAlignmentAndDrag(t *testing.T) {
	for _, immersive := range []bool{false, true} {
		t.Run(fmt.Sprint(immersive), func(t *testing.T) {
			a := tallLyricsApp(t)
			tt := ui.NewTesterWith(a.view, 880, 580, ui.TesterOptions{Preferences: ui.Preferences{ReduceMotion: true}})
			// Scale only the software raster, so full-screen layout tests stay inexpensive.
			const scale = 0.5
			tt.SetScale(scale)
			for _, size := range [][2]int{{1600, 550}, {1728, 1117}} {
				w, h := size[0], size[1]
				// Settle the viewport before entering manual mode. Resetting the
				// song's scroll state would otherwise clear browsing and run
				// the full-screen Gaussian, which has its own gutter test.
				a.settings.LyricEdgeFade = false
				tt.SetSize(w, h)
				a.views.playback.SeekLyricFollow()
				tt.Frame()
				if immersive {
					a.settings.LyricEdgeFade = true
					a.lyricReading.BrowseUntil = tt.Now().Add(playbackview.LyricBrowseDelay)
					tt.Frame()
					if a.LyricBlurPass != nil {
						t.Fatal("manual scrolling ran the automatic blur pass")
					}
				}
				cover, ok := tt.Find("专辑封面")
				if !ok {
					t.Fatal("cover missing")
				}
				wantRight := float32(w) - cover.X
				want := int(math.Round(float64(wantRight * scale)))
				// The native thumb appears when the pointer enters the scroll pane.
				tt.Move(1, 1)
				hidden := tt.Image()
				tt.Move(wantRight-4, float32(h)/2)
				visible := tt.Image()
				changed := image.Rectangle{}
				for y := 0; y < visible.Bounds().Dy(); y++ {
					for x := want - 8; x < want+8; x++ {
						if hidden.RGBAAt(x, y) != visible.RGBAAt(x, y) {
							changed = changed.Union(image.Rect(x, y, x+1, y+1))
						}
					}
				}
				if changed.Dy() < 12 || playbackview.Abs32(float32(changed.Max.X-want)) > 1 {
					t.Fatalf("%dx%d: cover left %.1f, want scrollbar right %d, got %v", w, h, cover.X, want, changed)
				}
				before := a.LyricScroll.Y
				x, y := float32(changed.Min.X+changed.Max.X)/(2*scale), float32(changed.Min.Y+changed.Max.Y)/(2*scale)
				tt.Press(x, y)
				tt.Move(x, y+32)
				tt.Release(x, y+32)
				if a.LyricScroll.Y <= before {
					t.Fatalf("%dx%d: dragging the thumb did not scroll: %.1f → %.1f", w, h, before, a.LyricScroll.Y)
				}
			}
		})
	}
}

func checkTallLyricsPosition(t *testing.T, tt *ui.Tester, w, h int) ui.Rect {
	t.Helper()
	tab, ok := tt.Find("歌词")
	if !ok {
		t.Fatal("no lyrics tab")
	}
	lit, ok := tt.Find("第15句")
	if !ok {
		t.Fatal("current line not shown")
	}
	cover, ok := tt.Find("专辑封面")
	if !ok {
		t.Fatal("no cover")
	}
	// The sung line stays near the upper third, with room for preceding lines;
	// it must not follow the vertically centered cover down a portrait window.
	top := max(cover.Y, tab.Y+tab.H+8)
	if h > w {
		top = tab.Y + tab.H + 8
	}
	if lit.Y < top {
		t.Errorf("current line above its pane: %.1f < %.1f", lit.Y, top)
	}
	if want := top + (float32(h)-top)*0.3; lit.Y > want+60 {
		t.Errorf("%dx%d: sung line at %.1f, want near %.1f in the upper third", w, h, lit.Y, want)
	}
	return lit
}

// Tall windows left a large blank area over the lyrics and cut off earlier
// lines; the sung line must stay near the upper third in either orientation.
func TestLyricsFillATallWindow(t *testing.T) {
	for _, size := range [][2]int{{1200, 800}, {750, 1330}, {600, 1400}} {
		w, h := size[0], size[1]
		t.Run(fmt.Sprintf("%dx%d", w, h), func(t *testing.T) {
			a := tallLyricsApp(t)
			tt := ui.NewTester(a.view, w, h)
			testutil.Settle(tt)
			checkTallLyricsPosition(t, tt, w, h)
			if dir := os.Getenv("NM_FRAMES"); dir != "" {
				f, err := os.Create(filepath.Join(dir, fmt.Sprintf("lyrics-%dx%d.png", w, h)))
				if err != nil {
					t.Fatal(err)
				}
				err = png.Encode(f, tt.Image())
				f.Close()
				if err != nil {
					t.Fatal(err)
				}
			}
		})
	}
}

// Resizing from landscape to portrait and back must recalculate the lyrics'
// position and return the landscape layout to where it started.
func TestLyricsReflowWhenWindowResizes(t *testing.T) {
	a := tallLyricsApp(t)
	tt := ui.NewTester(a.view, 1200, 800)
	testutil.Settle(tt)
	first := checkTallLyricsPosition(t, tt, 1200, 800)
	for _, size := range [][2]int{{750, 1330}, {600, 1400}, {1200, 800}} {
		w, h := size[0], size[1]
		tt.SetSize(w, h)
		testutil.Settle(tt)
		got := checkTallLyricsPosition(t, tt, w, h)
		if w == 1200 && (got.Y < first.Y-1 || got.Y > first.Y+1) {
			t.Fatalf("landscape line moved from %.1f to %.1f after resizing back", first.Y, got.Y)
		}
	}
}

// Following a resized viewport must not keep re-centering the current line
// when the listener scrolls manually without another size or line change.
func TestLyricsManualScrollStaysAfterResize(t *testing.T) {
	a := tallLyricsApp(t)
	tt := ui.NewTester(a.view, 1200, 800)
	testutil.Settle(tt)
	tt.SetSize(750, 1330)
	testutil.Settle(tt)
	before := checkTallLyricsPosition(t, tt, 750, 1330)
	tt.Scroll(620, 600, 0, 220)
	testutil.Settle(tt)
	scrolled, ok := tt.Find("第15句")
	if !ok || scrolled.Y > before.Y-40 {
		t.Fatal("manual scroll was reset by the unchanged current line")
	}
	testutil.Settle(tt)
	held, _ := tt.Find("第15句")
	if held.Y < scrolled.Y-1 || held.Y > scrolled.Y+1 {
		t.Fatal("lyrics re-centered after manual scrolling")
	}
}

// Width-only resizing could reflow long lyrics out of view without asking
// for a frame to follow them; test-driven Frame calls used to hide this.
func TestLyricsWidthResizeSchedulesFollowUp(t *testing.T) {
	a := tallLyricsApp(t)
	lines, _ := a.pl.Lyrics()
	for i := range lines {
		lines[i].Text = fmt.Sprintf("%02d%s", i, strings.Repeat("lyrics ", 14))
	}
	player.SetLyrics(a.pl, lines)
	tt := ui.NewTester(a.view, 1200, 800)
	testutil.Settle(tt)
	before, ok := tt.AccessByRole(ui.RoleButton, lines[15].Text)
	if !ok || before.Offscreen {
		t.Fatal("current line not visible before resize")
	}
	tt.SetSize(1400, 800)
	// Advance renders only frames the view actually requested. Do not call
	// Frame here: it would conceal the missing layout-followup regression.
	tt.Advance(2 * time.Second)
	after, ok := tt.AccessByRole(ui.RoleButton, lines[15].Text)
	if !ok || after.Offscreen || math.Abs(float64(after.Bounds.Y-before.Bounds.Y)) > 3 {
		t.Fatalf("resize lost current line: %+v -> %+v", before.Bounds, after.Bounds)
	}
}

// The immersive lyric viewport stays out of the window controls and extends below the volume row.
func TestImmersiveLyricViewportTracksControls(t *testing.T) {
	a := tallLyricsApp(t)
	a.settings.LyricEdgeFade = true
	a.settings.HotComments = false
	player.
		// One line exercises the same viewport without paying for the blur in the test.
		SetLyrics(a.pl, []lyrics.Line{{Text: "A short lyric"}})
	var barHeight float32
	tt := ui.NewTesterWith(func(c *ui.Context) {
		barHeight = shell.TopBar(c)
		a.views.playback.NowPlayingView(c)
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
		if playbackview.Abs32(view.Bounds.Y-wantTop) > 1 || playbackview.Abs32(view.Bounds.Y+view.Bounds.H-wantBottom) > 1 {
			t.Fatalf("at %v: lyrics %v, want top %.1f and bottom %.1f", size, view.Bounds, wantTop, wantBottom)
		}
	}
}

// Turning off immersive lyrics or leaving the pane releases its intermediate canvas.
func TestImmersiveLyricReleasesCanvas(t *testing.T) {
	a := tallLyricsApp(t)
	a.settings.LyricEdgeFade = true
	a.settings.HotComments = false
	player.SetLyrics(a.pl, []lyrics.Line{{Text: "A short lyric"}})
	tt := ui.NewTesterWith(a.view, 880, 580, ui.TesterOptions{Preferences: ui.Preferences{ReduceMotion: true}})
	a.LyricBlurPass = ui.NewCanvas(1, 1)
	a.settings.LyricEdgeFade = false
	tt.Frame()
	if a.LyricBlurPass != nil {
		t.Fatal("canvas was retained after disabling immersive lyrics")
	}
	a.settings.LyricEdgeFade = true
	a.LyricBlurPass = ui.NewCanvas(1, 1)
	a.playbackState.Pane = playbackview.PaneQueue
	tt.Frame()
	if a.LyricBlurPass != nil {
		t.Fatal("canvas was retained after switching panes")
	}
}
