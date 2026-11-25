/*
异步准备临时音频，将解码器移交播放器或在打开意图失效后释放。
*/

package local

import (
	"math"
	"slices"

	"netease-music-mygo/internal/localcatalog"
	"netease-music-mygo/internal/lyrics"
	"netease-music-mygo/internal/player"
)

// LocalOpenServices transfers a prepared decoder to the existing player, or releases it.
type LocalOpenServices struct {
	Post         func(func())
	PostResource func(func(), func())
	Stopped      func() bool
	Prepare      func(string) (localcatalog.Song, *player.LocalAudio)
	Release      func(*player.LocalAudio)
	Play         func(previous, song localcatalog.Song, audio *player.LocalAudio)
	Notify       func(string)
}

// CancelOpen retires a decoder being prepared for an older opening intent.
func (m *LocalLibrary) CancelOpen() { m.openRequest++ }

func (m *LocalLibrary) OpenTemporary(path string, s LocalOpenServices) {
	m.openRequest++
	request := m.openRequest
	post := s.PostResource
	if post == nil {
		post = func(fn, _ func()) { s.Post(fn) }
	}
	go func() {
		song, audio := s.Prepare(path)
		release := func() {
			if audio != nil {
				s.Release(audio)
			}
		}
		post(func() {
			if s.Stopped() || m.openRequest != request {
				release()
				return
			}
			if audio == nil {
				s.Notify("无法播放所选音频，请选择支持的音频文件")
				return
			}
			if i := slices.IndexFunc(m.Catalog.Songs, func(known localcatalog.Song) bool { return known.Path == song.Path }); i >= 0 {
				song.Track.LocalID = m.Catalog.Songs[i].Track.LocalID
			} else {
				// Library IDs ascend from 1; temporary IDs descend from MaxInt64.
				if m.temporaryID == 0 {
					m.temporaryID = math.MaxInt64
				}
				song.Track.LocalID = m.temporaryID
				m.temporaryID--
			}
			previous := m.Temporary
			m.Temporary, m.TemporaryLyrics = song, lyrics.Text{}
			s.Play(previous, song, audio)
		}, release)
	}()
}
