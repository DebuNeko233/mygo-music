package app

import (
	"math"

	"netease-music-mygo/internal/mixvol"
)

// setVolume sets the volume the app plays at, where the player bar and the
// system's mixer both read it.
func (a *app) setVolume(v float64) {
	a.volume = v
	a.syncVolume()
}

// syncVolume tells the player, and the system's mixer, the volume the app
// plays at. The mixer takes an amplitude, which the ear hears as its square,
// so what the slider shows goes there squared: a slider at half plays at the
// quarter the mixer shows.
func (a *app) syncVolume() {
	a.pl.SetVolume(a.volume)
	a.mix.SetVolume(a.volume * a.volume)
}

// systemVolume follows what the system's mixer holds for the app. Once it
// holds a volume, the mixer applies it and the player must not, and what the
// mixer is set to itself, by its own slider, is the volume the app shows.
func (a *app) systemVolume(s mixvol.State) {
	if !s.Attached {
		if a.mixVolume {
			a.mixVolume = false
			a.pl.SetSystemVolume(false)
		}
		return
	}
	if !a.mixVolume {
		// The mixer is told the volume the app shows before the player stops
		// applying it, so that what plays, which would otherwise play at the
		// mixer's own volume and then at that one again, never jumps.
		a.mix.SetVolume(a.volume * a.volume)
		a.mixVolume = true
		a.pl.SetSystemVolume(true)
		return
	}
	if v := math.Sqrt(s.Volume); math.Abs(v-a.volume) > 0.005 {
		a.setVolume(v)
	}
}
