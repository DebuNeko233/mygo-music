//go:build darwin && !ios && !mygo_noinspector

/*
在真实 macOS 窗口中检查全屏完成后的歌词首帧，不通过软件 Tester 自动补布局。
原生事件循环和只读自动化接口仅编译进测试程序。
*/

package app

import (
	"encoding/json"
	"fmt"
	"math"
	"net"
	"os"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/egoist/mygo"
	_ "github.com/egoist/mygo/plugins/automation"
	"github.com/egoist/mygo/ui"

	"netease-music-mygo/internal/features/lyrics"
	"netease-music-mygo/internal/features/player"
)

func TestMain(m *testing.M) {
	if os.Getenv("MYGO_E2E") != "1" {
		os.Exit(m.Run())
	}
	mygo.App.SetName("Nyago Lyric Layout Test")
	mygo.App.OnWindowAllClosed(func() {})
	code := 1
	mygo.App.WhenReady(func() {
		go func() {
			code = m.Run()
			mygo.App.Quit()
		}()
	})
	if err := mygo.App.Run(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	os.Exit(code)
}

// The first painted frame after either native completion already follows the
// current lyric; reading its geometry must not trigger an extra view update.
func TestNativeFullScreenLyricsFirstFrame(t *testing.T) {
	if os.Getenv("MYGO_E2E") != "1" {
		t.Skip("requires MYGO_E2E=1 and MYGO_AUTOMATION socket")
	}
	for _, immersive := range []bool{false, true} {
		for _, count := range []int{24, 80} {
			t.Run(fmt.Sprintf("immersive=%v/rows=%d", immersive, count), func(t *testing.T) {
				a := tallLyricsApp(t)
				a.settings.LyricEdgeFade, a.settings.HotComments = immersive, false
				lines := make([]lyrics.Line, count)
				for i := range lines {
					lines[i] = lyrics.Line{At: time.Duration(i) * 5 * time.Second, Text: fmt.Sprintf("Native lyric %02d", i), Translation: strings.Repeat("A long translated lyric wraps in a narrow window. ", 4)}
				}
				player.SetLyrics(a.pl, lines)
				current := count - 2
				player.Hold(a.pl, lines[current].At+time.Second)
				var stage atomic.Int32
				painted := make(chan int32, 128)
				ready := make(chan struct{}, 1)
				focused := make(chan struct{}, 1)
				modes := make(chan int32, 2)
				var w *mygo.Window
				mygo.RunOnMain(func() {
					w = mygo.NewWindow(mygo.WindowOptions{Width: 880, Height: 580,
						TitleBarStyle: mygo.TitleBarHidden, TrafficLightPosition: &mygo.Point{X: 20, Y: 20},
						Content: ui.View(func(c *ui.Context) {
							a.views.playback.NowPlayingView(c)
							c.Root().DrawOver(func(_ *ui.Painter, _ ui.Rect) {
								select {
								case painted <- stage.Load():
								default:
								}
							})
						})})
					w.OnReadyToShow(func() { ready <- struct{}{} })
					w.OnFocus(func() {
						select {
						case focused <- struct{}{}:
						default:
						}
					})
					w.OnEnterFullScreen(func() { stage.Store(1); modes <- 1 })
					w.OnLeaveFullScreen(func() { stage.Store(2); modes <- 2 })
				})
				t.Cleanup(func() {
					if w.IsFullScreen() {
						w.SetFullScreen(false)
						for nativeLyricWait(t, modes, "exit before window cleanup") != 2 {
						}
					}
					w.Destroy()
				})
				nativeLyricWait(t, ready, "window first frame")
				w.Focus()
				if !w.IsFocused() {
					nativeLyricWait(t, focused, "window focused")
				}

				for _, phase := range []int32{1, 2} {
					w.SetFullScreen(phase == 1)
					nativeLyricWait(t, modes, "full-screen mode callback")

					for nativeLyricWait(t, painted, "paint after full-screen completion") != phase {
					}
					var browsing bool
					mygo.RunOnMain(func() { browsing = !a.lyricReading.BrowseUntil.IsZero() })
					if browsing {
						t.Fatal("native reflow suspended following without user input")
					}
					row, viewport := nativeLyricRects(t, w.ID(), lines[current].Text)
					want := viewport.Y + min(viewport.H*0.3, max(0, viewport.H-row.H))
					if math.Abs(float64(row.Y-want)) > 1 {
						t.Fatalf("phase %d first paint: current row %v, viewport %v, want y %.2f", phase, row, viewport, want)
					}
				}
			})
		}
	}
}

func nativeLyricWait[T any](t *testing.T, ch <-chan T, what string) T {
	t.Helper()
	select {
	case v := <-ch:
		return v
	case <-time.After(time.Minute):
		t.Fatalf("native event did not arrive: %s", what)
		var zero T
		return zero
	}
}

// The automation plugin reads the last committed tree of a visible window. It
// does not build an extra frame there, unlike Tester.SetSize's settling loop.
func nativeLyricRects(t *testing.T, window int, label string) (row, viewport ui.Rect) {
	t.Helper()
	c, err := net.Dial("unix", os.Getenv("MYGO_AUTOMATION"))
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	if err := c.SetDeadline(time.Now().Add(time.Minute)); err != nil {
		t.Fatal(err)
	}
	if err := json.NewEncoder(c).Encode(map[string]any{"id": 1, "method": "tree", "params": map[string]int{"window": window}}); err != nil {
		t.Fatal(err)
	}
	var response struct {
		Error  any
		Result struct {
			Nodes []struct {
				Role, Label string
				Bounds      struct{ X, Y, Width, Height float32 }
			}
		}
	}
	if err := json.NewDecoder(c).Decode(&response); err != nil {
		t.Fatal(err)
	}
	if response.Error != nil {
		t.Fatalf("read committed tree: %v", response.Error)
	}
	for _, node := range response.Result.Nodes {
		b := node.Bounds
		box := ui.Rect{X: b.X, Y: b.Y, W: b.Width, H: b.Height}
		if node.Label == label {
			row = box
		}
		if node.Label == "歌词" && node.Role == "scroll" {
			viewport = box
		}
	}
	if row.H <= 0 || viewport.H <= 0 {
		t.Fatalf("missing committed geometry: row %v, viewport %v", row, viewport)
	}
	return
}
