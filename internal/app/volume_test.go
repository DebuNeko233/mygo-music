package app

import (
	"testing"

	"github.com/egoist/mygo/ui"
)

// volumeTester puts the volume at zero and points at the speaker button.
func volumeTester(t *testing.T) (*app, *ui.Tester, float32, float32) {
	t.Helper()
	a := newTestApp()
	a.settings = defaultSettings()
	a.volume = 0
	tt := ui.NewTester(a.view, 1000, 600)
	tt.Frame()
	r, ok := tt.Find("音量")
	if !ok {
		t.Fatal("no volume button")
	}
	return a, tt, r.X + r.W/2, r.Y + r.H/2
}

// nearVolume says whether the volume is at the ten-percent step wanted.
func nearVolume(t *testing.T, a *app, want float64) {
	t.Helper()
	if a.volume < want-0.001 || a.volume > want+0.001 {
		t.Fatalf("volume %.2f, want %.2f", a.volume, want)
	}
}

// A notch of the wheel changes the volume by ten percent, clamped at full.
func TestVolumeWheelUsesTenPercentSteps(t *testing.T) {
	a, tt, x, y := volumeTester(t)
	for i := 1; i <= 10; i++ {
		tt.Wheel(x, y, 0, -120)
		tt.Frame()
		nearVolume(t, a, float64(i)/10)
	}
	tt.Wheel(x, y, 0, -120)
	tt.Frame()
	nearVolume(t, a, 1)
}

// Scrolling down at silence leaves it there, and down-steps come back to it.
func TestVolumeWheelClampsAtZero(t *testing.T) {
	a, tt, x, y := volumeTester(t)
	tt.Wheel(x, y, 0, 120)
	tt.Frame()
	nearVolume(t, a, 0)
	for i := 1; i <= 10; i++ {
		tt.Wheel(x, y, 0, -120)
		tt.Frame()
	}
	nearVolume(t, a, 1)
	for i := 9; i >= 0; i-- {
		tt.Wheel(x, y, 0, 120)
		tt.Frame()
		nearVolume(t, a, float64(i)/10)
	}
	tt.Wheel(x, y, 0, 120)
	tt.Frame()
	nearVolume(t, a, 0)
}

// A touchpad's small pixel moves gather into one step; each used to jump ten percent.
func TestVolumeTouchpadAccumulatesSmallMoves(t *testing.T) {
	a, tt, x, y := volumeTester(t)
	for i := 0; i < 20; i++ {
		tt.Scroll(x, y, 0, -4)
	}
	tt.Frame()
	nearVolume(t, a, 0) // 80 DIPs are not yet a notch
	for i := 0; i < 5; i++ {
		tt.Scroll(x, y, 0, -4)
	}
	tt.Frame()
	nearVolume(t, a, 0.1)
}

// One fast scroll gesture is capped, rather than jumping the whole range.
func TestVolumeScrollCapsOneGesture(t *testing.T) {
	a, tt, x, y := volumeTester(t)
	tt.Scroll(x, y, 0, -100000)
	tt.Frame()
	if a.volume <= 0 || a.volume > 0.5 {
		t.Fatalf("one gesture took the volume to %.2f", a.volume)
	}
}
