/*
连接共享播放器与音量服务，管理音量调整、静音和系统音量同步。
*/

package playback

import (
	"math"

	"netease-music-mygo/internal/mixvol"
	"netease-music-mygo/internal/player"
)

type Playback struct {
	Open              bool
	Pane              int
	Volume, MutedFrom float64
	MixerVolume       bool
}
type VolumeServices struct {
	Player *player.Player
	Mixer  *mixvol.Mixer
}

func (m *Playback) SetVolume(v float64, s VolumeServices) {
	m.Volume = max(0, min(1, v))
	m.SyncVolume(s)
}
func (m *Playback) SyncVolume(s VolumeServices) {
	s.Player.SetVolume(m.Volume)
	s.Mixer.SetVolume(m.Volume)
}
func (m *Playback) StepVolume(down bool, s VolumeServices) {
	step := 0.1
	if down {
		step = -step
	}
	m.SetVolume(math.Round(min(1, max(0, m.Volume+step))*100)/100, s)
}
func (m *Playback) ToggleMute(s VolumeServices) {
	if m.Volume > 0 {
		m.MutedFrom, m.Volume = m.Volume, 0
	} else {
		m.Volume = max(m.MutedFrom, 0.5)
	}
	m.SyncVolume(s)
}
func (m *Playback) SystemVolume(state mixvol.State, s VolumeServices) {
	if !state.Attached {
		if m.MixerVolume {
			m.MixerVolume = false
			s.Player.SetSystemVolume(false)
		}
		return
	}
	if !m.MixerVolume {
		s.Mixer.SetVolume(m.Volume)
		m.MixerVolume = true
		s.Player.SetSystemVolume(true)
		return
	}
	if math.Abs(state.Volume-m.Volume) > 0.005 {
		m.SetVolume(state.Volume, s)
	}
}
