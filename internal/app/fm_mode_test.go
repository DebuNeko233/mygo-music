package app

import (
	"sync"
	"testing"
	"time"

	"github.com/egoist/mygo/ui"

	"netease-music-mygo/internal/netease"
)

// TestFMKeepsTheModeChosenWhileItStarts checks that the mode the listener
// picks last is the one that plays: the mode was taken from the radio on every
// frame, so a pick made while the radio still played the mode before it went
// back, and the songs the pick was fetching then played a mode the listener
// had already left.
func TestFMKeepsTheModeChosenWhileItStarts(t *testing.T) {
	a := newTestApp()
	a.settings = defaultSettings()
	a.route = route{page: pageFM}
	a.pl.AttachRadio(fmRadio(netease.FMDefault), func(got func([]netease.Track)) {})

	asked := make(chan string, 4)
	release := make(chan struct{})
	releaseFM := sync.OnceFunc(func() { close(release) })
	old := personalFM
	personalFM = func(mode string) ([]netease.Track, error) {
		asked <- mode
		<-release
		return []netease.Track{{ID: 7, Name: "探索的歌"}}, nil
	}
	t.Cleanup(func() { personalFM = old; releaseFM() })

	tt := ui.NewTester(a.view, 1200, 800)
	tt.Frame()
	if a.fmMode != 0 {
		t.Fatalf("mode %d, for the radio playing 推荐", a.fmMode)
	}

	// 探索, then back to 推荐 before its songs arrive.
	if err := tt.Click("探索"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	tt.Frame()
	if mode := <-asked; mode != netease.FMExplore {
		t.Fatalf("asked for %q, for the mode clicked", mode)
	}
	if a.fmMode != 2 {
		t.Fatalf("mode %d, for the mode clicked", a.fmMode)
	}
	if err := tt.Click("推荐"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	tt.Frame()
	if a.fmMode != 0 {
		t.Fatalf("mode %d, for the mode clicked last", a.fmMode)
	}

	// The songs 探索 asked for land now, and must not start it.
	releaseFM()
	tt.Advance(time.Second)
	tt.Frame()
	tt.Frame()
	if radio := a.pl.Radio(); radio != fmRadio(netease.FMDefault) {
		t.Fatalf("radio %q, for the mode clicked last", radio)
	}
	if a.fmMode != 0 {
		t.Errorf("mode %d, for the mode clicked last", a.fmMode)
	}
}
