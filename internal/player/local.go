package player

import (
	"errors"
	"fmt"
	"io"
	"os"
	"time"

	"netease-music-mygo/internal/localmedia"
	"netease-music-mygo/internal/lyrics"
	"netease-music-mygo/internal/netease"
)

var errLocalFile = errors.New("local audio file is unavailable")

func openLocal(path string) (*netease.Stream, stream, error) {
	if st, err := os.Stat(path); err != nil || !st.Mode().IsRegular() {
		return nil, nil, errLocalFile
	}
	f, err := os.Open(path)
	if err != nil {
		return nil, nil, fmt.Errorf("%w: %v", errLocalFile, err)
	}
	info, err := f.Stat()
	if err != nil || !info.Mode().IsRegular() {
		f.Close()
		return nil, nil, errLocalFile
	}
	dl := opened(f, info.Size())
	dl.local = true
	s, err := openStream(dl, 0)
	if err != nil {
		dl.Close()
		return nil, nil, err
	}
	st := &netease.Stream{Type: "mp3", Size: info.Size()}
	if _, ok := s.(*flacStream); ok {
		st.Type, st.Level = "flac", "lossless"
	}
	return st, s, nil
}

// LocalDuration reads the exact MP3 frame count or the FLAC stream length.
func LocalDuration(path string) (time.Duration, error) {
	_, s, err := openLocal(path)
	if err != nil {
		return 0, err
	}
	defer s.Close()
	return localDuration(s)
}

// LocalAudio holds a validated decoder until it is played or closed.
type LocalAudio struct {
	info     *netease.Stream
	stream   stream
	Duration time.Duration
}

// PrepareLocalAudio checks that sound can be decoded without interrupting playback.
func PrepareLocalAudio(path string) (*LocalAudio, error) {
	info, s, err := openLocal(path)
	if err != nil {
		return nil, err
	}
	duration, err := localDuration(s)
	if err == nil && s.Len() >= 0 {
		var first [2]float32
		if n, readErr := s.Read(first[:]); n == 0 {
			err = readErr
			if err == nil {
				err = io.ErrUnexpectedEOF
			}
		}
	}
	if err == nil {
		err = s.SetPosition(0)
	}
	if err != nil || duration <= 0 {
		s.Close()
		if err == nil {
			err = io.ErrUnexpectedEOF
		}
		return nil, err
	}
	return &LocalAudio{info: info, stream: s, Duration: duration}, nil
}

func (a *LocalAudio) Close() { a.stream.Close() }

func localDuration(s stream) (time.Duration, error) {
	frames := s.Len()
	if frames < 0 {
		frames = 0
		buf := make([]float32, 4096)
		for {
			n, err := s.Read(buf)
			frames += int64(n / s.Channels())
			if err != nil || n == 0 {
				break
			}
		}
		if err := s.cut(); err != nil {
			return 0, err
		}
	}
	rate := int64(s.SampleRate())
	return time.Duration(frames/rate)*time.Second + time.Duration(frames%rate)*time.Second/time.Duration(rate), nil
}

func (p *Player) loadLocalMetadata(t netease.Track, gen int) {
	if p.LocalPath == nil || p.lyricBusy || p.lyricsLoaded {
		return
	}
	path := p.LocalPath(t.LocalID)
	p.lyricBusy = true
	go func() {
		lines, display := lyrics.BuildDisplay(localmedia.Lyrics(path), "", "")
		cover := localmedia.Cover(path)
		p.post(func() {
			if p.gen != gen {
				return
			}
			p.lyricBusy = false
			p.setLyrics(lines, display)
			if len(cover) != 0 {
				p.announce(&t, cover)
			}
		})
	}()
}
