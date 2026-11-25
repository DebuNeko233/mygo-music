/*
基准测量歌词视图，并输出歌词和评论界面截图。
*/

package app

import (
	"fmt"
	"image/png"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/egoist/mygo/ui"

	playbackview "netease-music-mygo/internal/desktop/view/playback"
	"netease-music-mygo/internal/lyrics"
	"netease-music-mygo/internal/netease"
)

func lyricsMVVMPerfApp(comments bool) *app {
	a := catalogApp()
	a.pl.Cue([]netease.Track{{ID: 71, Name: "测试歌曲", Artists: []netease.Artist{{Name: "MyGO!!!!!"}}, Duration: 900000}})
	lines := make([]lyrics.Line, 300)
	for i := range lines {
		lines[i] = lyrics.Line{At: time.Duration(i) * 3 * time.Second, End: time.Duration(i+1) * 3 * time.Second, Text: fmt.Sprintf("歌词 %03d", i), Translation: "翻译文字"}
	}
	a.pl.SetLyrics(lines)
	a.pl.Hold(60 * time.Second)
	posts := testPosts(a)
	old := threadComments
	threadComments = func(string, int, int) (*netease.Comments, error) {
		latest := make([]netease.Comment, 100)
		for i := range latest {
			latest[i] = netease.Comment{ID: int64(i + 1), Content: fmt.Sprintf("评论 %03d", i), LikedCount: 25}
		}
		return &netease.Comments{Latest: latest, Total: 100}, nil
	}
	a.commentsFor(a.pl.Current())
	(<-posts)()
	threadComments = old
	a.playbackState.Open = true
	a.playbackState.Pane = playbackview.PaneLyrics
	if comments {
		a.playbackState.Pane = playbackview.PaneComments
	}
	return a
}

// Long lyrics and loaded comment lists reuse published slices during UI builds.
func BenchmarkLyricsMVVM(b *testing.B) {
	for _, comments := range []bool{false, true} {
		name := "lyrics"
		if comments {
			name = "comments"
		}
		b.Run(name, func(b *testing.B) {
			a := lyricsMVVMPerfApp(comments)
			tt := ui.NewTester(a.view, 1200, 800)
			tt.Frame()
			b.ReportAllocs()
			for b.Loop() {
				tt.Frame()
			}
		})
	}
}

// Held playback makes lyrics and comments screenshots repeatable without a live account or audio device.
func TestLyricsMVVMFrames(t *testing.T) {
	dir := os.Getenv("NM_FRAMES")
	if dir == "" {
		t.Skip("set NM_FRAMES to save lyrics and comments")
	}
	for _, comments := range []bool{false, true} {
		for _, dark := range []bool{false, true} {
			a := lyricsMVVMPerfApp(comments)
			tt := ui.NewTester(a.view, 1200, 800)
			tt.SetDark(dark)
			for range 3 {
				tt.Advance(time.Second)
				tt.Frame()
			}
			name := fmt.Sprintf("lyrics-mvvm-comments-%v-dark-%v.png", comments, dark)
			f, err := os.Create(filepath.Join(dir, name))
			if err != nil {
				t.Fatal(err)
			}
			err = png.Encode(f, tt.Image())
			f.Close()
			if err != nil {
				t.Fatal(err)
			}
		}
	}
}
