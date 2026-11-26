/*
验证歌词频谱停止行为，并提供频谱界面的截图夹具。
*/

package app

import (
	"image/png"
	"math"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/egoist/mygo/ui"

	"netease-music-mygo/internal/features/lyrics"
	"netease-music-mygo/internal/features/netease"
	"netease-music-mygo/internal/features/player"
	playbackview "netease-music-mygo/internal/ui/view/playback"
)

// The spectrum falls away smoothly during pause, seek, and track loading, and stops when hidden.
func TestLyricSpectrumStops(t *testing.T) {
	a := spectrumTestApp()
	tt := ui.NewTester(a.view, 880, 580)
	tt.SetScale(.5)
	tt.Advance(time.Second)

	seedLyricSpectrum(a, tt.Now())
	tt.Frame()
	if a.NPSpectrum.Renderer.Mask == nil {
		t.Fatal("the lyrics page did not draw the spectrum")
	}
	initial := a.NPSpectrum.Peak
	// Its motion follows display frames rather than skipping every other one.
	updates := 0
	for range 12 {
		before := a.NPSpectrum.At
		tt.Advance(time.Second / 60)
		if a.NPSpectrum.At.After(before) {
			updates++
		}
	}
	if updates < 10 {
		t.Fatalf("spectrum updated on only %d of 12 display frames", updates)
	}
	if peak := a.NPSpectrum.Peak; peak <= initial*0.40 || peak >= initial {
		t.Errorf("the paused spectrum should fall gradually, from %g to %g", initial, peak)
	}
	tt.Advance(2 * time.Second)
	if a.NPSpectrum.Peak > playbackview.LyricSpectrumSilent {
		t.Fatal("the paused spectrum did not settle")
	}
	at := a.NPSpectrum.At
	tt.Advance(time.Second)
	if !at.Equal(a.NPSpectrum.At) {
		t.Error("an idle spectrum kept asking for frames")
	}

	seedLyricSpectrum(a, tt.Now())
	peak := a.NPSpectrum.Peak
	a.pl.Seek(12 * time.Second)
	tt.Frame()
	if a.NPSpectrum.Peak != peak {
		t.Error("the spectrum jumped on the first frame after seeking")
	}
	tt.Advance(250 * time.Millisecond)
	if next := a.NPSpectrum.Peak; next <= 0 || next >= peak {
		t.Errorf("the spectrum did not ease out while seeking: %g → %g", peak, next)
	}
	peak = a.NPSpectrum.Peak
	player.Cue(a.pl, []netease.Track{{ID: 8, Name: "下一首", Duration: 200000}})
	tt.Frame()
	if a.NPSpectrum.Peak != peak {
		t.Error("the spectrum jumped on the first frame of the next song")
	}
	tt.Advance(250 * time.Millisecond)
	if next := a.NPSpectrum.Peak; next <= 0 || next >= peak {
		t.Errorf("the outgoing spectrum did not fall while waiting for audio: %g → %g", peak, next)
	}
	seedLyricSpectrum(a, tt.Now())
	peak, mask := a.NPSpectrum.Peak, a.NPSpectrum.Renderer.Mask
	a.playbackState.Pane = playbackview.PaneStage
	tt.Frame()
	if a.NPSpectrum.Peak != peak || a.NPSpectrum.Renderer.Mask != mask {
		t.Error("switching to the stage discarded the current spectrum")
	}
	a.playbackState.Pane = playbackview.PaneQueue
	tt.Frame()
	if a.NPSpectrum != (playbackview.LyricSpectrum{}) {
		t.Error("the queue kept the lyrics page's spectrum")
	}
	a.playbackState.Pane = playbackview.PaneLyrics
	tt.Frame()
	seedLyricSpectrum(a, tt.Now())
	tt.SetPreferences(ui.Preferences{ReduceMotion: true})
	if a.NPSpectrum != (playbackview.LyricSpectrum{}) {
		t.Error("Reduce Motion kept the animated spectrum")
	}
}

// With NM_FRAMES set, save the spectrum behind the real lyrics view at two window sizes.
func TestLyricSpectrumFrames(t *testing.T) {
	dir := os.Getenv("NM_FRAMES")
	if dir == "" {
		t.Skip()
	}
	for _, size := range []struct {
		name          string
		width, height int
		pane          int
	}{{"desktop", 1200, 800, playbackview.PaneLyrics}, {"compact", 750, 580, playbackview.PaneLyrics}, {"stage", 1200, 800, playbackview.PaneStage}} {
		a := spectrumTestApp()
		a.playbackState.Pane = size.pane
		tt := ui.NewTester(a.view, size.width, size.height)
		tt.Advance(time.Second)
		seedLyricSpectrum(a, tt.Now())
		tt.Frame()
		f, err := os.Create(filepath.Join(dir, "lyric-spectrum-"+size.name+".png"))
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

func spectrumTestApp() *app {
	a := newTestApp()
	a.settings = defaultSettings()
	a.settings.LyricSpectrum = true
	a.playbackState.Open, a.playbackState.Pane = true, playbackview.PaneLyrics
	player.Cue(a.pl, []netease.Track{{ID: 7, Name: "夜航", Artists: []netease.Artist{{Name: "测试音轨"}}, Duration: 200000}})
	player.SetLyrics(a.pl, lyrics.Build("[00:00.00]夜色落在海面\n[00:04.00]星光照亮归途\n[00:08.00]远处的灯缓缓亮起\n[00:12.00]风从耳边经过\n[00:16.00]潮水轻轻退去\n[00:20.00]月亮停在天边\n", "", ""))
	a.pl.Seek(8 * time.Second)
	return a
}

func seedLyricSpectrum(a *app, now time.Time) {
	st := &a.NPSpectrum
	st.At = now
	for i := range st.Levels {
		x := float64(i) / float64(len(st.Levels)-1)
		st.Levels[i] = float32((.48 + .22*math.Sin(16*x) + .19*math.Sin(35*x) + .04*math.Cos(150*x)) * (1 - .50*x))
	}
	st.SmoothContour()
}
