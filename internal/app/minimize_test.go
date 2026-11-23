package app

import (
	"fmt"
	"testing"
	"time"

	"github.com/egoist/mygo/ui"

	"netease-music-mygo/internal/lyrics"
	"netease-music-mygo/internal/netease"
	commentsvm "netease-music-mygo/internal/viewmodel/comments"
)

// Windows lays out a minimized window at zero size. The cover divided its
// gesture angles by that size and crashed every Now Playing pane with NaN.
func TestNowPlayingRestoreAfterMinimize(t *testing.T) {
	for _, pane := range []struct {
		id   int
		name string
	}{{paneLyrics, "lyrics"}, {paneComments, "comments"}, {paneQueue, "queue"}} {
		t.Run(pane.name, func(t *testing.T) {
			a := newTestApp()
			a.settings = defaultSettings()
			a.pl.Cue([]netease.Track{{ID: 1, Name: "One", Duration: 200000}, {ID: 2, Name: "Two", Duration: 200000}})
			lines := make([]lyrics.Line, 300)
			for i := range lines {
				lines[i] = lyrics.Line{At: time.Duration(i) * time.Second, Text: fmt.Sprintf("Lyric %03d", i)}
			}
			a.pl.SetLyrics(lines)
			a.comments = commentsvm.Comments{Track: 1, Loaded: true, Total: 1, Latest: []netease.Comment{{ID: 1, Content: "Comment survives minimize"}}}
			a.playbackState.Open, a.playbackState.Pane = true, pane.id
			tt := ui.NewTester(a.view, 1200, 800)
			if pane.id == paneLyrics {
				a.pl.Hold(lines[150].At)
			}
			// The minimized layout and restored hit targets are measured in DIPs.
			tt.SetScale(0.25)
			tt.Frame()
			tt.Advance(time.Second)
			tt.Frame()
			if pane.id == paneLyrics {
				if a.lyricScroll.Y == 0 {
					t.Fatalf("lyrics did not follow before minimize: at=%d viewport=%vx%v", a.lyricReading.At, a.lyricViewportW, a.lyricViewportH)
				}
				row, ok := tt.AccessByRole(ui.RoleButton, lines[150].Text)
				if !ok || row.Offscreen {
					t.Fatalf("active lyric was not followed before minimize: offset=%v row=%+v", a.lyricScroll.Y, row)
				}
			}
			tt.SetWindowHidden(true)
			tt.SetSize(0, 0)
			tt.SetWindowHidden(false)
			tt.SetSize(1200, 800)
			tt.Advance(time.Second)
			if _, ok := tt.Access("关闭"); !ok {
				t.Fatal("Now Playing did not return after restoring the window")
			}
			if pane.id == paneLyrics {
				tt.Advance(time.Second)
				row, ok := tt.AccessByRole(ui.RoleButton, lines[150].Text)
				if !ok || row.Offscreen || a.lyricScroll.Y == 0 {
					t.Fatalf("restoring the window lost the active lyric: offset=%v row=%+v", a.lyricScroll.Y, row)
				}
			}
		})
	}
}
