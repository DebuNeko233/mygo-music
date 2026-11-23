package app

import (
	"netease-music-mygo/internal/mixvol"
	playbackvm "netease-music-mygo/internal/viewmodel/playback"
)

func (a *app) volumeServices() playbackvm.VolumeServices {
	return playbackvm.VolumeServices{Player: a.pl, Mixer: a.mix}
}

func (a *app) setVolume(v float64) { a.playbackState.SetVolume(v, a.volumeServices()) }

func (a *app) syncVolume() { a.playbackState.SyncVolume(a.volumeServices()) }

func (a *app) systemVolume(s mixvol.State) { a.playbackState.SystemVolume(s, a.volumeServices()) }
