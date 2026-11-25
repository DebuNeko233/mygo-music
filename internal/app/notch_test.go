/*
验证刘海播放器的首次问候与悬停保持，并生成首次使用截图。
*/

package app

import (
	"fmt"
	"image/png"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"

	playbackview "netease-music-mygo/internal/desktop/view/playback"
	"netease-music-mygo/internal/netease"
)

// The notch, turned off, shows itself as the first song plays, and only
// then; its button keeps it on.
func TestNotchGreetsOnce(t *testing.T) {
	a := newTestApp()
	a.settings = defaultSettings()
	n := &playbackview.Notch{A: &a.views.playback, W0: 200, H0: 32, WinW: playbackview.NotchOpenW + 60, WinH: 240}
	a.notch = n
	a.pl.Cue([]netease.Track{{ID: 7, Name: "宝船", Duration: 200000}})
	a.syncWindows()
	if n.Hello {
		t.Fatal("greeted before the song played")
	}
	out := newSilentAudio()
	a.pl.Attach(out)
	defer out.Close()
	a.syncWindows()
	if !n.Hello || !a.settings.NotchGreeted {
		t.Fatal("did not greet as the song played")
	}

	tt := ui.NewTester(n.View, int(n.WinW), int(n.WinH))
	tt.Advance(time.Second)
	tt.Frame()
	if err := tt.Click("开启刘海播放器"); err != nil {
		t.Fatal(err)
	}
	if !a.settings.Notch || n.Hello {
		t.Fatalf("kept: notch %v, greeting %v", a.settings.Notch, n.Hello)
	}

	// Turned off again, it does not greet the next song.
	a.settings.Notch = false
	a.pl.Cue([]netease.Track{{ID: 8, Name: "下一首", Duration: 200000}})
	a.pl.Attach(out)
	a.syncWindows()
	if n.Hello {
		t.Error("greeted twice")
	}
}

// The pointer resting on the greeting leaves it as it is, so its button can
// be clicked. It used to open the player, which hid the button, and leaving
// brought it back only to open again as the pointer came back to it.
func TestNotchGreetingKeptUnderPointer(t *testing.T) {
	a := newTestApp()
	a.settings = defaultSettings()
	n := &playbackview.Notch{A: &a.views.playback, Real: true, W0: 185, H0: 32, WinW: playbackview.NotchOpenW + 60, WinH: 240}
	a.notch = n
	a.pl.Cue([]netease.Track{{ID: 7, Name: "宝船", Duration: 200000}})
	n.Greet()
	if !n.Hello {
		t.Fatal("did not greet")
	}

	defer func(f func() mygo.Point) { cursorPoint = f }(cursorPoint)
	cursorPoint = func() mygo.Point {
		return mygo.Point{X: int(n.ScreenX + n.WinW/2 + 120), Y: int(n.ScreenY + n.H0 + 28)}
	}
	n.Poll()
	n.EnteredAt = n.LastInside.Add(-time.Second) // as if it had rested there
	n.Poll()
	if n.Open {
		t.Fatal("opened the player over the greeting")
	}

	tt := ui.NewTester(n.View, int(n.WinW), int(n.WinH))
	tt.Advance(time.Second)
	tt.Frame()
	if err := tt.Click("开启刘海播放器"); err != nil {
		t.Fatal(err)
	}
	if !a.settings.Notch || n.Hello {
		t.Fatalf("kept: notch %v, greeting %v", a.settings.Notch, n.Hello)
	}
}

// With NM_FRAMES set, saves the notch as it greets, growing and grown, and
// the player bar holding a song a login picked up.
func TestFirstUseFrames(t *testing.T) {
	dir := os.Getenv("NM_FRAMES")
	if dir == "" {
		t.Skip()
	}
	save := func(name string, tt *ui.Tester) {
		f, _ := os.Create(filepath.Join(dir, name+".png"))
		png.Encode(f, tt.Image())
		f.Close()
	}
	a := newTestApp()
	a.settings = defaultSettings()
	n := &playbackview.Notch{A: &a.views.playback, Real: true, W0: 185, H0: 32, WinW: playbackview.NotchOpenW + 60, WinH: 240}
	a.notch = n
	a.pl.Cue([]netease.Track{{ID: 7, Name: "NAINAINAI", Artists: []netease.Artist{{Name: "ATARASHII GAKKO!"}}, Duration: 193877}})
	main := ui.NewTester(a.view, 1200, 800) // for the app's colors
	main.Frame()
	tt := ui.NewTester(n.View, int(n.WinW), int(n.WinH))
	tt.Frame()
	n.Greet()
	for _, ms := range []int{120, 600} {
		tt.Advance(time.Duration(ms) * time.Millisecond)
		tt.Frame()
		save(fmt.Sprintf("notch-hello-%d", ms), tt)
	}

	a.picked = playbackview.PickedUp{ID: 7, Heard: "3分钟前在手机上听过"}
	main.Advance(time.Second)
	main.Frame()
	save("pickup-bar", main)
}
