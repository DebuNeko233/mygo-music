package app

import (
	"fmt"
	"image/png"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/egoist/mygo/ui"
	"netease-music-mygo/internal/lyrics"
	"netease-music-mygo/internal/netease"
)

func tallLyricsApp(t *testing.T) *app {
	t.Helper()
	isolateUserData(t)
	a := newTestApp()
	a.settings = defaultSettings()
	a.pl.Cue([]netease.Track{{ID: 1, Name: "高窗口布局", Duration: 150000}})
	var lines []lyrics.Line
	for i := range 30 {
		at := time.Duration(i) * 5 * time.Second
		lines = append(lines, lyrics.Line{At: at, End: at + 5*time.Second, Text: fmt.Sprintf("第%02d句", i)})
	}
	a.pl.SetLyrics(lines)
	a.pl.Hold(77 * time.Second)
	a.nowPlaying, a.npPane = true, paneLyrics
	a.comments = commentState{track: 1, loaded: true}
	return a
}

func settleTallLyrics(tt *ui.Tester) {
	tt.Frame()
	for range 2 {
		tt.Advance(time.Second)
		tt.Frame()
		tt.Frame()
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
			settleTallLyrics(tt)
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
	settleTallLyrics(tt)
	first := checkTallLyricsPosition(t, tt, 1200, 800)
	for _, size := range [][2]int{{750, 1330}, {600, 1400}, {1200, 800}} {
		w, h := size[0], size[1]
		tt.SetSize(w, h)
		settleTallLyrics(tt)
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
	settleTallLyrics(tt)
	tt.SetSize(750, 1330)
	settleTallLyrics(tt)
	before := checkTallLyricsPosition(t, tt, 750, 1330)
	tt.Scroll(620, 600, 0, 220)
	settleTallLyrics(tt)
	scrolled, ok := tt.Find("第15句")
	if !ok || scrolled.Y > before.Y-40 {
		t.Fatal("manual scroll was reset by the unchanged current line")
	}
	settleTallLyrics(tt)
	held, _ := tt.Find("第15句")
	if held.Y < scrolled.Y-1 || held.Y > scrolled.Y+1 {
		t.Fatal("lyrics re-centered after manual scrolling")
	}
}
