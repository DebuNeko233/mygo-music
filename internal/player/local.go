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
	var s stream
	switch {
	case dl.waitFor(12) >= 12 && string(dl.peek(0, 4)) == "RIFF" && string(dl.peek(8, 4)) == "WAVE":
		s, err = openWAVStream(dl)
	case dl.waitFor(4) >= 4 && string(dl.peek(0, 4)) == "OggS":
		s, err = openOggStream(dl)
	case dl.waitFor(12) >= 12 && string(dl.peek(4, 4)) == "ftyp":
		s, err = openMP4Stream(dl)
	case isADTS(dl.peek(id3Size(dl), 2)):
		s, err = openAACStream(dl)
	default:
		s, err = openStream(dl, 0)
	}
	if err != nil {
		dl.Close()
		return nil, nil, err
	}
	st := &netease.Stream{Type: "mp3", Size: info.Size()}
	switch s.(type) {
	case *flacStream:
		st.Type, st.Level = "flac", "lossless"
	case *wavStream:
		st.Type, st.Level = "wav", "lossless"
	case *oggStream:
		st.Type = "ogg"
	case *adtsFAADStream:
		st.Type = "aac"
	case *decoderStream:
		st.Type = "m4a"
		if isADTS(dl.peek(id3Size(dl), 2)) {
			st.Type = "aac"
		}
	}
	return st, s, nil
}

func isADTS(header []byte) bool {
	return len(header) == 2 && header[0] == 0xff && header[1]&0xf6 == 0xf0
}

// LocalDuration reads the decoded frame count of a supported local audio file.
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

// ReloadLocalLyrics applies an explicit match without changing the audio.
func (p *Player) ReloadLocalLyrics() {
	if p.track != nil && p.track.IsLocal() && !p.cacheChanging {
		p.loadLocalLyrics(*p.track, p.gen)
	}
}

// ShowLocalArtwork publishes the cover if the track still plays.
func (p *Player) ShowLocalArtwork(track *netease.Track, cover []byte) {
	if p.track == track && track.IsLocal() {
		p.announce(track, cover)
	}
}

func (p *Player) loadLocalLyrics(t netease.Track, gen int) {
	if p.LocalPath == nil || p.lyricBusy {
		return
	}
	path := p.LocalPath(t.LocalID)
	var matched func() (string, string, string)
	if p.LocalLyrics != nil {
		matched = p.LocalLyrics(t)
	}
	p.lyricBusy = true
	go func() {
		lines, display := lyrics.BuildDisplay(localmedia.Lyrics(path), "", "")
		if !display.HasSinging() {
			if matched != nil {
				if fallback, view := lyrics.BuildDisplay(matched()); view.HasSinging() {
					lines, display = fallback, view
				}
			}
			if !display.HasSinging() {
				display = display.WithUnavailableNotice()
			}
		}
		p.post(func() {
			if p.gen != gen {
				return
			}
			p.lyricBusy = false
			p.setLyrics(lines, display)
		})
	}()
}
