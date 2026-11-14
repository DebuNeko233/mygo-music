package app

import (
	"fmt"
	"testing"
	"time"

	"github.com/egoist/mygo/ui"

	"netease-music-mygo/internal/lyrics"
	"netease-music-mygo/internal/netease"
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
			a.comments = commentState{track: 1, loaded: true, total: 1, latest: []netease.Comment{{ID: 1, Content: "Comment survives minimize"}}}
			a.nowPlaying, a.npPane = true, pane.id
			tt := ui.NewTester(a.view, 1200, 800)
			tt.Frame()
			tt.Advance(time.Second)
			tt.Frame()
			tt.SetWindowHidden(true)
			tt.SetSize(0, 0)
			tt.SetWindowHidden(false)
			tt.SetSize(1200, 800)
			tt.Advance(time.Second)
			if _, ok := tt.Access("关闭"); !ok {
				t.Fatal("Now Playing did not return after restoring the window")
			}
		})
	}
}
