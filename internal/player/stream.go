/*
连接下载缓冲与音频解码器，提供 MP3 帧扫描、流读取及跳转。
*/

package player

import (
	"bufio"
	"cmp"
	"encoding/binary"
	"errors"
	"io"
	"math"
	"sync"

	"github.com/egoist/mygo/audio"
	gomp3 "github.com/hajimehoshi/go-mp3"
	"netease-music-mygo/internal/audioformat"
)

// A window reads a download a piece at a time, for scanning it byte by
// byte without reading the file for each.
type window struct {
	dl  *download
	off int64
	b   []byte
}

// windowPiece is how much of the song a window reads at a time: enough that a
// scan locks the download once per piece rather than once per byte.
const windowPiece = 32 << 10

// at returns up to n bytes from off, as far as they are in. It keeps the piece
// it read in b, and reads the next piece into that same b.
func (w *window) at(off int64, n int) []byte {
	if off < w.off || off+int64(n) > w.off+int64(len(w.b)) {
		piece := max(n, windowPiece)
		if cap(w.b) < piece {
			w.b = make([]byte, piece)
		} else {
			w.b = w.b[:piece]
		}
		w.off, w.b = off, w.dl.peekInto(w.b, off)
	}
	// Guard against out-of-bounds access when the download has not reached
	// the requested offset yet (e.g., during rapid song switching).
	start := off - w.off
	if start < 0 || start >= int64(len(w.b)) {
		return nil
	}
	end := min(int64(len(w.b)), start+int64(n))
	if end <= start {
		return nil
	}
	return w.b[start:end]
}

// A downloadReader reads a download from an offset, waiting for its bytes.
// It is no io.Seeker, so go-mp3 does not read the whole file up front to
// find where each frame starts.
type downloadReader struct {
	d   *download
	pos int64
}

func (r *downloadReader) Read(p []byte) (int, error) {
	if len(p) == 0 {
		return 0, nil
	}
	d := r.d
	d.mu.Lock()
	defer d.mu.Unlock()
	for d.rangeEnd(r.pos) == r.pos && !d.done {
		d.cond.Wait()
	}
	if d.rangeEnd(r.pos) == r.pos {
		if d.err != nil {
			return 0, d.err
		}
		return 0, io.EOF
	}
	n, err := d.readAt(p, r.pos)
	if n == 0 {
		return 0, cmp.Or(err, io.ErrUnexpectedEOF)
	}
	r.pos += int64(n)
	return n, nil
}

// MP3 headers and side information arrive in small reads. Buffer them so
// each does not lock the download and read its file again. A partial buffer
// is usable as soon as bytes arrive; it need not fill before playback starts.
func mp3DownloadReader(d *download, at int64) *bufio.Reader {
	return bufio.NewReaderSize(&downloadReader{d: d, pos: at}, audioReadBuffer)
}

// A stream decodes a download for package audio, and measures the
// spectrum of what it decodes, for the visualizers: an MP3's or a FLAC's.
type stream interface {
	audio.Decoder
	Close() error
	// source returns the download decoded, and its spectrum.
	source() (*download, *spectrum)
	// cut returns why the download was cut off, or nil: the song itself
	// ended.
	cut() error
}

// openStream opens any supported container from its bytes, regardless of source.
// An MP3 uses durationMs when its length is not known.
func openStream(dl *download, durationMs int64) (stream, error) {
	return openStreamSpectrum(dl, durationMs, true)
}

func openStreamSpectrum(dl *download, durationMs int64, measure bool) (stream, error) {
	off := id3Size(dl)
	dl.waitRange(off, off+12)
	kind := audioformat.Detect(dl.peek(off, 12))
	dl.mu.Lock()
	dl.format = kind
	dl.mu.Unlock()
	switch kind {
	case "wav":
		return openWAVStreamSpectrum(dl, measure)
	case "ogg":
		return openOggStreamSpectrum(dl, measure)
	case "m4a":
		return openMP4StreamSpectrum(dl, measure)
	case "aac":
		return openAACStreamSpectrum(dl, measure)
	case "flac":
		return openFLACStreamSpectrum(dl, measure)
	default:
		return openMP3StreamSpectrum(dl, durationMs, measure)
	}
}

// openShared opens a second stream of what dl fetches, for decoding apart
// from what plays: it measures no spectrum, and closing it lets the song
// go for it alone, leaving the download to the stream that plays.
func openShared(dl *download, durationMs int64) (stream, error) {
	if !dl.share() {
		return nil, errors.New("player: the song is gone")
	}
	s, err := openStreamSpectrum(dl, durationMs, false)
	if err != nil {
		dl.release()
		return nil, err
	}
	switch s := s.(type) {
	case *mp3Stream:
		s.drop = dl.release
	case *flacStream:
		s.drop = dl.release
	case *wavStream:
		s.drop = dl.release
	case *oggStream:
		s.drop = dl.release
	case *decoderStream:
		s.drop = dl.release
	case *adtsStream:
		s.drop = dl.release
	}
	return s, nil
}

// mp3Stream decodes a download for package audio, seeking by bytes: songs
// are encoded at a constant bit rate, so time and bytes are in proportion.
// It also measures the spectrum of what it decodes, for the visualizers.
type mp3Stream struct {
	dl      *download
	dec     *gomp3.Decoder
	rate    int
	frames  int64 // the song's length in frames, from its duration
	start   int64 // the first audio frame's byte, after the ID3 tag
	pos     int64 // the frame decoded next
	skip    int   // bytes of decoded audio to drop after a seek
	buf     []byte
	spec    *spectrum
	drop    func() // lets the download go, as the stream closes
	closeMu sync.Once
	failure decoderFailure
}

func openMP3StreamSpectrum(dl *download, durationMs int64, measure bool) (*mp3Stream, error) {
	s := &mp3Stream{dl: dl, drop: dl.Close}
	var reader io.Reader
	if dl.local {
		dl.mu.Lock()
		file := dl.file
		dl.mu.Unlock()
		if file == nil {
			return nil, io.ErrClosedPipe
		}
		reader = io.NewSectionReader(&bufferedReaderAt{source: file}, 0, dl.size)
	} else {
		reader = mp3DownloadReader(dl, 0)
	}
	dec, err := gomp3.NewDecoder(reader)
	if err != nil {
		return nil, err
	}
	s.dec, s.rate = dec, dec.SampleRate()
	if dl.local {
		s.frames = dec.Length() / 4
	} else {
		s.frames = durationMs * int64(s.rate) / 1000
		s.start = id3Size(dl)
	}
	if measure {
		s.spec = newSpectrum(s.rate)
	}
	return s, nil
}

// id3Size returns the length of the ID3v2 tag at the start of the file.
func id3Size(dl *download) int64 {
	dl.waitFor(10)
	return audioformat.ID3Size(dl.peek(0, 10))
}

func (s *mp3Stream) SampleRate() int { return s.rate }
func (s *mp3Stream) Channels() int   { return 2 }
func (s *mp3Stream) Len() int64      { return s.frames }

func (s *mp3Stream) Read(dst []float32) (int, error) {
	n := len(dst) / 2 * 4
	if cap(s.buf) < n {
		s.buf = make([]byte, n)
	}
	b := s.buf[:n]
	for s.skip > 0 {
		got, err := io.ReadFull(s.dec, b[:min(n, s.skip)])
		s.skip -= got
		if err != nil {
			s.skip = 0
			s.failure.record(err)
		}
	}
	got, err := io.ReadFull(s.dec, b)
	// UnexpectedEOF from ReadFull is normal for a final partial PCM buffer.
	if err != io.ErrUnexpectedEOF {
		s.failure.record(err)
	}
	got -= got % 4
	samples := got / 2
	for i := range samples {
		dst[i] = float32(int16(binary.LittleEndian.Uint16(b[2*i:]))) / (1 << 15)
	}
	frames := int64(samples / 2)
	s.spec.feed(s.pos, dst[:samples])
	s.pos += frames
	if samples > 0 {
		return samples, nil
	}
	if err == io.ErrUnexpectedEOF || err == nil {
		// go-mp3 can end at a partial frame: that is a failed download,
		// not the end of the song, or the player would skip it.
		if dlErr := s.dl.Err(); dlErr != nil {
			err = dlErr
		} else {
			err = io.EOF
		}
	}
	return 0, err
}

// preroll is how many frames of sound a seek decodes before the one asked
// for, and drops. An MP3 frame borrows bits from the frames before it, up
// to 511 bytes back, which at 128 kbps is two frames, and its sound
// overlaps theirs: the first few decoded from the middle of a file are
// noise, as loud as the loudest sound.
const preroll = 4 * 1152

func (s *mp3Stream) SetPosition(frame int64) error {
	if s.dl.local {
		frame = min(max(frame, 0), s.frames)
		from := max(frame-preroll, 0)
		if _, err := s.dec.Seek(from*4, io.SeekStart); err != nil {
			s.failure.record(err)
			return err
		}
		s.pos, s.skip = frame, int(frame-from)*4
		s.failure.clear()
		return nil
	}
	// The frame to decode from, and where its bytes are, which is unknown:
	// the bytes are spread over the song in proportion to the frames.
	off := s.start
	if s.frames > 0 {
		from := max(frame-preroll, 0)
		size := s.dl.size
		if size <= 0 {
			size = s.dl.waitFor(math.MaxInt64)
		}
		off = s.start + int64(float64(size-s.start)*float64(from)/float64(s.frames))
	}
	// Prioritize any missing bytes around the seek target. Already available
	// frames, including the start of the song on repeat, can be read in place.
	s.dl.seekTo(off)
	off = findFrame(s.dl, off)
	dec, err := gomp3.NewDecoder(mp3DownloadReader(s.dl, off))
	if err != nil {
		s.failure.record(err)
		return err
	}
	s.dec, s.pos = dec, frame
	s.failure.clear()
	// Drop what comes before frame: the preroll, or near the start, all
	// of it from there.
	s.skip = int(min(frame, preroll)) * 4
	return nil
}

func (s *mp3Stream) source() (*download, *spectrum) { return s.dl, s.spec }

// cut returns why the download was cut off, or nil: the song itself ended.
func (s *mp3Stream) cut() error { return cmp.Or(s.dl.Err(), s.failure.load()) }

func (s *mp3Stream) Close() error {
	s.closeMu.Do(s.drop)
	return nil
}

// findFrame returns the start of the first MP3 frame at or after off whose
// header the next frame's confirms, so that bytes that only look like one
// within the audio do not count.
func findFrame(dl *download, off int64) int64 {
	w := window{dl: dl}
	for tries := 0; tries < 64<<10; tries++ {
		have := dl.waitRange(off, off+4)
		if have < off+4 {
			return off
		}
		size := frameSize(w.at(off, 4))
		if size > 0 {
			nextOff := off + int64(size)
			dl.waitRange(nextOff, nextOff+4)
			next := w.at(nextOff, 4)
			if len(next) < 4 || frameSize(next) > 0 {
				return off
			}
		}
		off++
	}
	return off
}

var (
	bitratesV1 = [16]int{0, 32, 40, 48, 56, 64, 80, 96, 112, 128, 160, 192, 224, 256, 320, 0}
	bitratesV2 = [16]int{0, 8, 16, 24, 32, 40, 48, 56, 64, 80, 96, 112, 128, 144, 160, 0}
	rates      = [4]int{44100, 48000, 32000, 0}
)

// frameSize returns the length of the MPEG layer III frame whose header b
// starts with, or 0 if it starts with none.
func frameSize(b []byte) int {
	if len(b) < 4 || b[0] != 0xff || b[1]&0xe0 != 0xe0 {
		return 0
	}
	version := (b[1] >> 3) & 3 // 3: MPEG 1, 2: MPEG 2, 0: MPEG 2.5
	layer := (b[1] >> 1) & 3   // 1: layer III
	if version == 1 || layer != 1 {
		return 0
	}
	bi, ri := b[2]>>4, (b[2]>>2)&3
	if bi == 0 || bi == 15 || ri == 3 {
		return 0
	}
	pad := int(b[2]>>1) & 1
	rate := rates[ri]
	switch version {
	case 3:
		return 144*bitratesV1[bi]*1000/rate + pad
	case 2:
		return 72*bitratesV2[bi]*1000/(rate/2) + pad
	default:
		return 72*bitratesV2[bi]*1000/(rate/4) + pad
	}
}
