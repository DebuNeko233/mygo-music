package app

import (
	"fmt"
	"testing"
	"time"

	"github.com/egoist/mygo/ui"

	"netease-music-mygo/internal/netease"
	"netease-music-mygo/internal/player"
	"netease-music-mygo/internal/viewmodel"
)

// Clicking or pressing Space toggles shuffle and its accessible state without changing the current track in any transport size.
func TestTransportShuffleToggle(t *testing.T) {
	for _, size := range []float32{18, 20, 22, 28} {
		t.Run(fmt.Sprint(size), func(t *testing.T) {
			a := newTestApp()
			a.pl.Cue([]netease.Track{{ID: 1}, {ID: 2}, {ID: 3}})
			tt := ui.NewTester(func(c *ui.Context) {
				a.transport(c, white, white.Alpha(0.45), white, size)
			}, 400, 100)
			tt.Frame()
			for i, on := range []bool{false, true, false, true} {
				if i == 2 {
					tt.Key(0, ui.KeySpace)
				} else if i > 0 {
					if err := tt.Click("随机播放"); err != nil {
						t.Fatal(err)
					}
				}
				tt.Frame()
				button, ok := tt.AccessByRole(ui.RoleToggleButton, "随机播放")
				if !ok || button.Checked != on || a.pl.Shuffle() != on || a.pl.Current().ID != 1 || len(a.pl.Queue()) != 3 {
					t.Fatalf("shuffle %v, checked %v (toggle found %v), track %v", a.pl.Shuffle(), button.Checked, ok, a.pl.Current())
				}
			}
		})
	}
}

// The highlighted loop button still cycles through both repeat modes and back to off.
func TestTransportRepeatCycle(t *testing.T) {
	a := newTestApp()
	a.pl.Cue([]netease.Track{{ID: 1}})
	tt := ui.NewTester(func(c *ui.Context) {
		a.transport(c, white, white.Alpha(0.45), white, 22)
	}, 400, 100)
	for _, want := range []player.RepeatMode{player.RepeatAll, player.RepeatOne, player.RepeatOff} {
		if err := tt.Click("循环"); err != nil {
			t.Fatal(err)
		}
		if got := a.pl.Repeat(); got != want {
			t.Fatalf("repeat mode = %v, want %v", got, want)
		}
	}
}

// Centering the volume track leaves its click-to-set and the speaker's mute/unmute behavior intact.
func TestNowPlayingVolumeTrackAndMute(t *testing.T) {
	a := newTestApp()
	a.settings = defaultSettings()
	a.pl.Cue([]netease.Track{{ID: 1, Name: "歌曲", Duration: 200000}})
	a.pl.SetLyrics(nil)
	a.comments = viewmodel.Comments{Track: 1, Loaded: true}
	a.playbackState.Open = true
	a.setVolume(0.5)
	tt := ui.NewTester(a.view, 1200, 800)
	tt.SetPreferences(ui.Preferences{ReduceMotion: true})
	tt.Frame()
	tt.Advance(time.Second)
	tt.Frame()
	slider, ok := tt.AccessByRole(ui.RoleSlider, "音量")
	if !ok {
		t.Fatal("no volume track")
	}
	r := slider.Bounds
	tt.ClickAt(r.X+3*r.W/4, r.Y+r.H/2)
	tt.Frame()
	nearVolume(t, a, 0.75)
	if err := tt.Click("静音"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	nearVolume(t, a, 0)
	if err := tt.Click("静音"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	nearVolume(t, a, 0.75)
}
