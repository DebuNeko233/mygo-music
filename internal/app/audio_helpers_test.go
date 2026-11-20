package app

import "github.com/egoist/mygo/audio"

// silence is a song of silence, to play without a stream.
type silence struct{ pos int64 }

func (s *silence) SampleRate() int           { return 48000 }
func (s *silence) Channels() int             { return 2 }
func (s *silence) Len() int64                { return 48000 * 200 }
func (s *silence) SetPosition(f int64) error { s.pos = f; return nil }
func (s *silence) Read(buf []float32) (int, error) {
	clear(buf)
	s.pos += int64(len(buf) / 2)
	return len(buf), nil
}

// newSilentAudio leaves playback and closing to its caller.
func newSilentAudio() *audio.Player {
	out := audio.NewPlayer(&silence{})
	out.SetVolume(0)
	return out
}
