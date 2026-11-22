package player

import (
	"encoding/binary"
	"errors"
	"io"
	"math"
	"sync"

	"github.com/egoist/mygo/audio"
	"github.com/jfreymuth/oggvorbis"
)

// wavStream reads integer and floating-point PCM from a RIFF/WAVE source.
// Extensible WAV headers use the subtype GUID's format code, as ordinary PCM
// headers do, so CD rips with either header can be played without conversion.
type wavStream struct {
	dl        *download
	file      io.ReaderAt
	data      int64
	frames    int64
	channels  int
	bits      int
	validBits int
	format    uint16
	rate      int
	pos       int64
	buf       []byte
	spec      *spectrum
	drop      func()
	once      sync.Once
	failure   decoderFailure
}

func openWAVStreamSpectrum(dl *download, measure bool) (*wavStream, error) {
	file := dl.decoderSource()
	var riff [12]byte
	if _, err := file.ReadAt(riff[:], 0); err != nil {
		return nil, err
	}
	if string(riff[0:4]) != "RIFF" || string(riff[8:12]) != "WAVE" {
		return nil, errors.New("wav: not a RIFF/WAVE file")
	}
	size := int64(binary.LittleEndian.Uint32(riff[4:8])) + 8
	dl.mu.Lock()
	if dl.length >= 0 {
		size = min(size, dl.length)
	}
	dl.mu.Unlock()
	if size < 12 {
		return nil, io.ErrUnexpectedEOF
	}
	source := io.NewSectionReader(file, 0, size)
	r := source
	if _, err := r.Seek(12, io.SeekStart); err != nil {
		return nil, err
	}
	s := &wavStream{dl: dl, file: source}
	// Ignore unusual ancillary chunks rather than assuming data follows fmt.
	for s.data == 0 {
		var header [8]byte
		if _, err := io.ReadFull(r, header[:]); err != nil {
			if err == io.EOF {
				break
			}
			return nil, err
		}
		size := int64(binary.LittleEndian.Uint32(header[4:]))
		at, err := r.Seek(0, io.SeekCurrent)
		if err != nil || size < 0 || size > source.Size()-at {
			return nil, io.ErrUnexpectedEOF
		}
		switch string(header[:4]) {
		case "fmt ":
			if s.rate != 0 || size < 16 || size > 1<<20 {
				return nil, errors.New("wav: invalid format chunk")
			}
			b := make([]byte, size)
			if _, err := io.ReadFull(r, b); err != nil {
				return nil, err
			}
			s.format = binary.LittleEndian.Uint16(b)
			s.channels = int(binary.LittleEndian.Uint16(b[2:]))
			s.rate = int(binary.LittleEndian.Uint32(b[4:]))
			block := int(binary.LittleEndian.Uint16(b[12:]))
			s.bits = int(binary.LittleEndian.Uint16(b[14:]))
			s.validBits = s.bits
			if s.format == 0xfffe && len(b) >= 40 {
				s.format = binary.LittleEndian.Uint16(b[24:])
				s.validBits = int(binary.LittleEndian.Uint16(b[18:]))
				if s.validBits == 0 {
					s.validBits = s.bits
				}
			}
			if s.channels < 1 || s.channels > 32 || s.rate < 1 ||
				block != s.channels*((s.bits+7)/8) ||
				s.validBits < 1 || s.validBits > s.bits {
				return nil, errors.New("wav: invalid PCM layout")
			}
		case "data":
			if s.rate != 0 && s.data == 0 {
				s.data = at
				block := s.channels * ((s.bits + 7) / 8)
				s.frames = size / int64(block)
			}
		}
		if _, err := r.Seek(at+size+size%2, io.SeekStart); err != nil {
			return nil, err
		}
	}
	if s.data == 0 || s.frames == 0 || !s.supported() {
		return nil, errors.New("wav: missing or unsupported PCM data")
	}
	if measure {
		s.spec = newSpectrum(s.rate)
	}
	return s, nil
}

func (s *wavStream) supported() bool {
	if s.format == 1 {
		return s.bits == 8 || s.bits == 16 || s.bits == 24 || s.bits == 32
	}
	return s.format == 3 && (s.bits == 32 || s.bits == 64)
}

func (s *wavStream) SampleRate() int { return s.rate }
func (s *wavStream) Channels() int   { return 2 }
func (s *wavStream) Len() int64      { return s.frames }

func (s *wavStream) Read(dst []float32) (int, error) {
	n := min(len(dst)/2, int(s.frames-s.pos))
	if n <= 0 {
		return 0, io.EOF
	}
	block := s.channels * ((s.bits + 7) / 8)
	need := n * block
	if cap(s.buf) < need {
		s.buf = make([]byte, need)
	}
	got, err := io.ReadFull(io.NewSectionReader(s.file, s.data+s.pos*int64(block), int64(need)), s.buf[:need])
	got -= got % block
	s.failure.record(err)
	for i := 0; i < got/block; i++ {
		sample := s.buf[i*block:]
		left := wavSample(sample, s.format, s.bits, s.validBits)
		right := left
		if s.channels > 1 {
			right = wavSample(sample[(s.bits+7)/8:], s.format, s.bits, s.validBits)
		}
		// More than two channels are reduced equally, without wrapping integer PCM.
		if s.channels > 2 {
			left, right = 0, 0
			for ch := range s.channels {
				value := wavSample(sample[ch*((s.bits+7)/8):], s.format, s.bits, s.validBits)
				if ch%2 == 0 {
					left += value * 2 / float32(s.channels)
				} else {
					right += value * 2 / float32(s.channels)
				}
			}
		}
		dst[2*i], dst[2*i+1] = left, right
	}
	n = got / block * 2
	s.spec.feed(s.pos, dst[:n])
	s.pos += int64(n / 2)
	if n > 0 {
		return n, nil
	}
	return 0, err
}

func wavSample(b []byte, format uint16, bits, validBits int) float32 {
	switch {
	case format == 3 && bits == 32:
		return float32(math.Float32frombits(binary.LittleEndian.Uint32(b)))
	case format == 3 && bits == 64:
		return float32(math.Float64frombits(binary.LittleEndian.Uint64(b)))
	case bits == 8:
		return (float32(b[0]) - 128) / 128
	case bits == 16:
		return float32(int16(binary.LittleEndian.Uint16(b))) / (1 << 15)
	case bits == 24:
		v := int32(b[0]) | int32(b[1])<<8 | int32(b[2])<<16
		return float32(v<<8>>8) / (1 << 23)
	default:
		v := int32(binary.LittleEndian.Uint32(b))
		// Extensible PCM stores fewer valid bits left-aligned in the container.
		return float32(v>>(bits-validBits)) / float32(int64(1)<<(validBits-1))
	}
}

func (s *wavStream) SetPosition(frame int64) error {
	s.pos = min(max(frame, 0), s.frames)
	s.failure.clear()
	return nil
}
func (s *wavStream) Close() error {
	s.once.Do(func() {
		if s.drop != nil {
			s.drop()
		} else {
			s.dl.Close()
		}
	})
	return nil
}
func (s *wavStream) source() (*download, *spectrum) { return s.dl, s.spec }
func (s *wavStream) cut() error                     { return cmpOr(s.dl.Err(), s.failure.load()) }

func cmpOr(errs ...error) error {
	for _, err := range errs {
		if err != nil {
			return err
		}
	}
	return nil
}

// oggStream seeks by decoded sample position, including Vorbis files with
// an embedded cover or other metadata before their audio packets.
type oggStream struct {
	dl      *download
	dec     *oggvorbis.Reader
	frames  int64
	pos     int64
	buf     []float32
	spec    *spectrum
	drop    func()
	once    sync.Once
	failure decoderFailure
}

func openOggStreamSpectrum(dl *download, measure bool) (*oggStream, error) {
	source, err := dl.decoderReader()
	if err != nil {
		return nil, err
	}
	dec, err := oggvorbis.NewReader(source)
	if err != nil {
		return nil, err
	}
	if dec.Channels() < 1 || dec.SampleRate() < 1 || dec.Length() <= 0 {
		return nil, errors.New("ogg: missing or empty Vorbis stream")
	}
	s := &oggStream{dl: dl, dec: dec, frames: dec.Length()}
	if measure {
		s.spec = newSpectrum(dec.SampleRate())
	}
	return s, nil
}

func (s *oggStream) SampleRate() int { return s.dec.SampleRate() }
func (s *oggStream) Channels() int   { return 2 }
func (s *oggStream) Len() int64      { return s.frames }

func (s *oggStream) Read(dst []float32) (int, error) {
	channels := s.dec.Channels()
	frames := len(dst) / 2
	need := frames * channels
	if cap(s.buf) < need {
		s.buf = make([]float32, need)
	}
	n, err := s.dec.Read(s.buf[:need])
	s.failure.record(err)
	frames = n / channels
	for i := range frames {
		left := s.buf[i*channels]
		right := left
		if channels > 1 {
			right = s.buf[i*channels+1]
		}
		if channels > 2 {
			left, right = 0, 0
			for ch := range channels {
				if ch%2 == 0 {
					left += s.buf[i*channels+ch] * 2 / float32(channels)
				} else {
					right += s.buf[i*channels+ch] * 2 / float32(channels)
				}
			}
		}
		dst[2*i], dst[2*i+1] = left, right
	}
	s.spec.feed(s.pos, dst[:frames*2])
	s.pos += int64(frames)
	if frames > 0 {
		return frames * 2, nil
	}
	return 0, cmpOr(err, io.EOF)
}

func (s *oggStream) SetPosition(frame int64) error {
	if err := s.dec.SetPosition(min(max(frame, 0), s.frames)); err != nil {
		s.failure.record(err)
		return err
	}
	s.pos = min(max(frame, 0), s.frames)
	s.failure.clear()
	return nil
}
func (s *oggStream) Close() error {
	s.once.Do(func() {
		if s.drop != nil {
			s.drop()
		} else {
			s.dl.Close()
		}
	})
	return nil
}
func (s *oggStream) source() (*download, *spectrum) { return s.dl, s.spec }
func (s *oggStream) cut() error                     { return cmpOr(s.dl.Err(), s.failure.load()) }

// decoderStream adds ownership and spectrum metering to a container decoder.
type decoderStream struct {
	dl       *download
	dec      audio.Decoder
	closer   io.Closer
	closeErr error
	pos      int64
	spec     *spectrum
	drop     func()
	once     sync.Once
	failure  decoderFailure
}

func (s *decoderStream) SampleRate() int { return s.dec.SampleRate() }
func (s *decoderStream) Channels() int   { return 2 }
func (s *decoderStream) Len() int64      { return s.dec.Len() }
func (s *decoderStream) Read(dst []float32) (int, error) {
	n, err := s.dec.Read(dst)
	s.failure.record(err)
	s.spec.feed(s.pos, dst[:n])
	s.pos += int64(n / 2)
	return n, err
}
func (s *decoderStream) SetPosition(frame int64) error {
	if err := s.dec.SetPosition(frame); err != nil {
		s.failure.record(err)
		return err
	}
	s.pos = frame
	s.failure.clear()
	return nil
}
func (s *decoderStream) Close() error {
	s.once.Do(func() {
		if s.closer != nil {
			s.closeErr = s.closer.Close()
		}
		if s.drop != nil {
			s.drop()
		} else {
			s.dl.Close()
		}
	})
	return s.closeErr
}
func (s *decoderStream) source() (*download, *spectrum) { return s.dl, s.spec }
func (s *decoderStream) cut() error                     { return cmpOr(s.dl.Err(), s.failure.load()) }
