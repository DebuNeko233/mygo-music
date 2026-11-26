/*
基准测量播放视图与共享模型接线，并输出播放界面截图。
*/

package app

import (
	"image/png"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/egoist/mygo/ui"

	"netease-music-mygo/internal/features/netease"
	"netease-music-mygo/internal/features/player"
	radiovm "netease-music-mygo/internal/ui/viewmodel/radio"
)

// Playback and FM views reuse player facts rather than copying audio or queue state into the ViewModel.
func BenchmarkPlaybackMVVM(b *testing.B) {
	for _, fm := range []bool{false, true} {
		name := "bar"
		if fm {
			name = "FM"
		}
		b.Run(name, func(b *testing.B) {
			a := catalogApp()
			if fm {
				a.nav.Current = route{Page: pageFM}
			}
			tt := ui.NewTester(a.view, 1200, 800)
			tt.Frame()
			b.ReportAllocs()
			for b.Loop() {
				tt.Frame()
			}
		})
	}
}

// FM controls and playback chrome can be compared visually without starting audio.
func TestPlaybackMVVMFrames(t *testing.T) {
	dir := os.Getenv("NM_FRAMES")
	if dir == "" {
		t.Skip("set NM_FRAMES to save playback views")
	}
	for _, dark := range []bool{false, true} {
		a := catalogApp()
		a.nav.Current = route{Page: pageFM}
		player.Cue(a.pl, []netease.Track{{ID: 7, Name: "春日影", Artists: []netease.Artist{{Name: "MyGO!!!!!"}}, Duration: 180000}})
		a.pl.AttachRadio(radiovm.FMRadio(netease.FMDefault), func(got func([]netease.Track)) { got(nil) })
		tt := ui.NewTester(a.view, 1200, 800)
		tt.SetDark(dark)
		tt.Advance(time.Second)
		tt.Frame()
		name := "playback-fm-light.png"
		if dark {
			name = "playback-fm-dark.png"
		}
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
