package app

import (
	"testing"

	"github.com/egoist/mygo/ui"
)

// The wheel changes volume in ten-percent steps, not by the platform's delta.
func TestVolumeWheelUsesTenPercentSteps(t *testing.T) {
	a := newTestApp()
	a.settings = defaultSettings()
	a.volume = 0
	tt := ui.NewTester(a.view, 1000, 600)
	tt.Frame()
	r, ok := tt.Find("音量")
	if !ok {
		t.Fatal("no volume button")
	}
	x, y := r.X+r.W/2, r.Y+r.H/2
	for want := 0.1; want <= 1; want += 0.1 {
		tt.Wheel(x, y, 0, -120)
		tt.Frame()
		if a.volume < want-0.001 || a.volume > want+0.001 {
			t.Fatalf("volume %.2f, want %.2f", a.volume, want)
		}
	}
	tt.Wheel(x, y, 0, -120)
	tt.Frame()
	if a.volume != 1 {
		t.Fatalf("wheel passed full volume: %.2f", a.volume)
	}
	tt.Scroll(x, y, 0, 1)
	tt.Frame()
	if a.volume < 0.9-0.001 || a.volume > 0.9+0.001 {
		t.Fatalf("precise scroll should use the same step: %.2f", a.volume)
	}
}
