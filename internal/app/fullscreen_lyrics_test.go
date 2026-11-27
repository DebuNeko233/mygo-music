/*
验证全屏动画连续改变尺寸与标题栏高度后，歌词当前句仍回到正确阅读位置。
*/

package app

import (
	"fmt"
	"image/png"
	"math"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/egoist/mygo/ui"
	"netease-music-mygo/internal/features/lyrics"
	"netease-music-mygo/internal/features/player"
)

// Full-screen resizing uses many intermediate layouts and a separate title-bar
// change. Without manual input, neither may enter browsing or retain an old target.
func TestLyricsFollowAcrossFullScreenAnimation(t *testing.T) {
	for _, immersive := range []bool{false, true} {
		for _, count := range []int{24, 80} {
			t.Run(fmt.Sprintf("immersive=%v/rows=%d", immersive, count), func(t *testing.T) {
				a := tallLyricsApp(t)
				a.settings.LyricEdgeFade, a.settings.HotComments = immersive, false
				lines := make([]lyrics.Line, count)
				for i := range lines {
					lines[i] = lyrics.Line{At: time.Duration(i) * 5 * time.Second, Text: fmt.Sprintf("动画歌词第%02d句", i), Translation: "A translated line"}
				}
				player.SetLyrics(a.pl, lines)
				player.Hold(a.pl, 41*time.Second)
				tt := ui.NewTester(a.view, 1180, 780)
				tt.SetScale(0.15)
				tt.SetTitleBar(ui.TitleBar{Height: 54, Left: 86, Bottom: 34})
				tt.Advance(2 * time.Second)
				current := 8
				row, _ := tt.AccessByRole(ui.RoleButton, lines[current].Text)
				lineHeight := row.Bounds.H
				position := func() float32 {
					t.Helper()
					row, ok := tt.AccessByRole(ui.RoleButton, lines[current].Text)
					if !ok || row.Offscreen {
						t.Fatalf("current line not visible: %+v", row)
					}
					// A new active row scales about its center. Compare the full-size
					// row origin so that emphasis is not mistaken for scroll motion.
					return row.Bounds.Y + (row.Bounds.H-lineHeight)/2
				}
				before := position()
				checkPosition := func() {
					t.Helper()
					row, _ := tt.AccessByRole(ui.RoleButton, lines[current].Text)
					pane, ok := tt.AccessByRole(ui.RoleScroll, "歌词")
					if !ok {
						t.Fatal("missing lyric viewport")
					}
					want := pane.Bounds.Y + min(pane.Bounds.H*0.3, max(0, pane.Bounds.H-row.Bounds.H))
					if math.Abs(float64(position()-want)) > 1 {
						t.Fatalf("row %.2f, want %.2f in viewport %v, measured height %.2f scroll %+v glide %+v", position(), want, pane.Bounds, a.LyricViewportH, a.LyricScroll, a.LyricGlide)
					}
				}
				checkPosition()
				for _, direction := range []int{1, -1} {
					// Start a normal line-follow glide before AppKit starts resizing.
					current++
					player.Hold(a.pl, time.Duration(current)*5*time.Second+time.Second)
					tt.Frame()
					tt.Advance(16 * time.Millisecond)
					for step := 1; step <= 12; step++ {
						k := step
						if direction < 0 {
							k = 12 - step
						}
						tt.SetSize(1180+548*k/12, 780+337*k/12)
						// Geometry changes reposition the current line immediately;
						// a glide for the old viewport must not keep it off target.
						checkPosition()
						tt.Advance(16 * time.Millisecond)
					}
					// Native title-bar callbacks need not coincide with the final resize.
					tt.Advance(2 * time.Second)
					bar := ui.TitleBar{}
					if direction < 0 {
						bar = ui.TitleBar{Height: 54, Left: 86, Bottom: 34}
					}
					tt.SetTitleBar(bar)
					tt.Advance(2 * time.Second)
					checkPosition()
					if !a.lyricReading.BrowseUntil.IsZero() {
						t.Fatal("window animation was mistaken for manual lyric browsing")
					}
				}
				if after := position(); math.Abs(float64(after-before)) > 1 {
					t.Fatalf("current line shifted after returning to the same window: %.2f -> %.2f", before, after)
				}
			})
		}
	}
}

// Optional visual fixtures cover both themes, minimum size and the final full-screen layout.
func TestFullScreenLyricsFrames(t *testing.T) {
	dir := os.Getenv("NM_FRAMES")
	if dir == "" {
		t.Skip("set NM_FRAMES to export layout fixtures")
	}
	for _, immersive := range []bool{false, true} {
		for _, dark := range []bool{false, true} {
			t.Run(fmt.Sprintf("immersive=%v/dark=%v", immersive, dark), func(t *testing.T) {
				a := tallLyricsApp(t)
				a.settings.LyricEdgeFade, a.settings.HotComments = immersive, false
				tt := ui.NewTester(a.view, 1180, 780)
				tt.SetScale(0.5)
				tt.SetDark(dark)
				for _, size := range [][2]int{{1180, 780}, {1728, 1117}, {880, 580}} {
					tt.SetSize(size[0], size[1])
					bar := ui.TitleBar{Height: 54, Left: 86, Bottom: 34}
					if size[0] == 1728 {
						bar = ui.TitleBar{}
					}
					tt.SetTitleBar(bar)
					tt.Advance(2 * time.Second)
					checkTallLyricsPosition(t, tt, size[0], size[1])
					name := fmt.Sprintf("lyrics-immersive-%v-dark-%v-%dx%d.png", immersive, dark, size[0], size[1])
					f, err := os.Create(filepath.Join(dir, name))
					if err != nil {
						t.Fatal(err)
					}
					err = png.Encode(f, tt.Image())
					closeErr := f.Close()
					if err != nil {
						t.Fatal(err)
					}
					if closeErr != nil {
						t.Fatal(closeErr)
					}
				}
			})
		}
	}
}
