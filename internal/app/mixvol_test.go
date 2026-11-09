package app

import (
	"math"
	"testing"

	"netease-music-mygo/internal/mixvol"
)

// The volume the system's mixer is set to becomes the volume the app shows,
// once the mixer holds one.
func TestTheMixerSetsTheAppsVolume(t *testing.T) {
	a := newTestApp()
	a.volume = 0.5
	// The mixer has just taken the volume: what it held before, the one it
	// starts at, is not what the app plays at.
	a.systemVolume(mixvol.State{Attached: true, Volume: 1})
	if a.volume != 0.5 {
		t.Fatalf("attaching the mixer left the volume at %.2f", a.volume)
	}
	if !a.mixVolume {
		t.Fatal("the mixer applies the volume, and the app does too")
	}
	// Set in the mixer itself: the same number shows in the app.
	a.systemVolume(mixvol.State{Attached: true, Volume: 0.36})
	if math.Abs(a.volume-0.36) > 0.005 {
		t.Fatalf("the mixer's 36 percent shows as %.2f", a.volume)
	}
	// Gone again, as a device change takes it: the app applies the volume.
	a.systemVolume(mixvol.State{Attached: false})
	if a.mixVolume {
		t.Fatal("no mixer holds the volume, and the app leaves it out")
	}
	if math.Abs(a.volume-0.36) > 0.005 {
		t.Fatalf("losing the mixer left the volume at %.2f", a.volume)
	}
}
