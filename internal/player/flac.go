package player

import (
	"bufio"
	"bytes"
	"cmp"
	"errors"
	"io"
	"math"
	"sync"

	"github.com/mewkiz/flac"
	"github.com/mewkiz/flac/frame"
	"github.com/mewkiz/flac/meta"
)

// flacStream decodes a FLAC download for package audio: the lossless and
// Hi-Res streams. Its frames stand alone, borrowing nothing from those
// before, so a seek lands on the very sample: decoding from the frame that
// holds it, found by its header near where the bytes say it is.
type flacStream struct {
	dl      *download
	info    *meta.StreamInfo
	start   int64 // the first frame's byte, after the metadata
	r       *bufio.Reader
	pos     int64     // the frame Read returns next
	skip    int64     // frames to drop before it, after a seek
	pend    []float32 // decoded, and not yet read: stereo
	spec    *spectrum
	drop    func() // lets the download go, as the stream closes
	once    sync.Once
	failure decoderFailure

	// A seek looks for the frame holding a sample by scanning the bytes for a
	// frame's header. What that scan reads it keeps, so looking again neither
	// allocates nor reads the same bytes twice.
	scan     []byte         // the piece the scan looks at
	readFrom downloadReader // where Read reads the frame next
	lookFrom downloadReader // where the scan reads the frame it looks at
	lookR    *bufio.Reader  // the scan's frame, header first
}

func openFLACStream(dl *download) (*flacStream, error) {
	start, err := flacDataStart(dl)
	if err != nil {
		return nil, err
	}
	from := id3Size(dl)
	st, err := flac.New(bytes.NewReader(dl.peek(from, int(start-from))))
	if err != nil {
		return nil, err
	}
	s := &flacStream{dl: dl, info: st.Info, start: start, drop: dl.Close}
	if s.info.NChannels == 0 || s.info.SampleRate == 0 {
		return nil, errors.New("flac: no stream info")
	}
	s.spec = newSpectrum(int(s.info.SampleRate))
	s.readFrom = downloadReader{d: dl, pos: start}
	s.r = bufio.NewReaderSize(&s.readFrom, 64<<10)
	return s, nil
}

// flacDataStart returns where a FLAC file's first frame starts, after its
// ID3 tag, if any, and its metadata blocks.
func flacDataStart(dl *download) (int64, error) {
	off := id3Size(dl)
	if dl.waitFor(off+4) < off+4 || string(dl.peek(off, 4)) != "fLaC" {
		return 0, errors.New("flac: not a FLAC file")
	}
	off += 4
	for {
		if dl.waitFor(off+4) < off+4 {
			return 0, io.ErrUnexpectedEOF
		}
		h := dl.peek(off, 4)
		last := h[0]&0x80 != 0
		off += 4 + (int64(h[1])<<16 | int64(h[2])<<8 | int64(h[3]))
		if last {
			if dl.waitFor(off) < off {
				return 0, io.ErrUnexpectedEOF
			}
			return off, nil
		}
	}
}

func (s *flacStream) SampleRate() int { return int(s.info.SampleRate) }
func (s *flacStream) Channels() int   { return 2 }

func (s *flacStream) Len() int64 {
	if s.info.NSamples == 0 {
		return -1
	}
	return int64(s.info.NSamples)
}

// parse parses the frame r is at: its header, then its samples, taking
// what the header leaves to the stream info from there.
func (s *flacStream) parse(r io.Reader) (*frame.Frame, error) {
	f, err := frame.New(r)
	if err != nil {
		return nil, err
	}
	if f.BitsPerSample == 0 {
		f.BitsPerSample = s.info.BitsPerSample
	}
	if f.SampleRate == 0 {
		f.SampleRate = s.info.SampleRate
	}
	if err := f.Parse(); err != nil {
		return nil, err
	}
	return f, nil
}

func (s *flacStream) Read(dst []float32) (int, error) {
	for len(s.pend) == 0 {
		f, err := s.parse(s.r)
		if err != nil {
			// An end before the samples the stream says it has is a
			// download cut short, not the song's end; so is the error the
			// download itself reports, which says more than an EOF does.
			if errors.Is(err, io.EOF) || errors.Is(err, io.ErrUnexpectedEOF) {
				if s.Len() > 0 && s.pos < s.Len() {
					err = io.ErrUnexpectedEOF
				} else if errors.Is(err, io.ErrUnexpectedEOF) {
					err = io.EOF
				}
			}
			if dlErr := s.dl.Err(); dlErr != nil {
				err = dlErr
			}
			s.failure.record(err)
			return 0, err
		}
		s.decode(f)
	}
	n := copy(dst[:len(dst)/2*2], s.pend)
	s.pend = s.pend[n:]
	s.spec.feed(s.pos, dst[:n])
	s.pos += int64(n / 2)
	return n, nil
}

// decode adds a frame's samples to those pending, in stereo, but those to
// skip.
func (s *flacStream) decode(f *frame.Frame) {
	n := int64(f.BlockSize)
	from := min(s.skip, n)
	s.skip -= from
	if from == n {
		return
	}
	left, right := f.Subframes[0].Samples, f.Subframes[0].Samples
	if len(f.Subframes) > 1 {
		right = f.Subframes[1].Samples
	}
	scale := float32(1 / math.Exp2(float64(f.BitsPerSample-1)))
	if cap(s.pend) < int(n)*2 {
		s.pend = make([]float32, 0, n*2)
	}
	out := s.pend[:0]
	for i := from; i < n; i++ {
		out = append(out, float32(left[i])*scale, float32(right[i])*scale)
	}
	s.pend = out
}

// SetPosition goes to frame: to the frame of FLAC holding it, then drops
// what comes before it there. It finds that frame by the secant method,
// guessing where in the bytes it is from the frames found on either side,
// until one is found less than a second before it. A download that stopped
// short of the song fails the seek rather than report a position whose frame
// is not there: a decoder told it went there reads on from a frame that
// cannot follow, asks again at once, and does so for as long as it plays.
func (s *flacStream) SetPosition(target int64) error {
	total := int64(s.info.NSamples)
	declared := s.dl.size
	if declared <= 0 {
		declared = s.dl.waitFor(math.MaxInt64)
	}
	size := declared
	// What came in bounds where frames are looked for: a download that
	// stopped short has none past the bytes it brought.
	short := false
	if _, in, stopped := s.dl.arrived(); stopped && in < declared && in > s.start {
		short, size = true, in
	}
	if short && total > 0 {
		// The frames are spread evenly enough to tell which of them came
		// in: past that, the bytes the sample asked for would be in are
		// not there, so the scan is spared for a seek that cannot work.
		if target > total*(size-s.start)/(declared-s.start) {
			return s.seekFailed()
		}
	}
	loOff, lo := s.start, int64(0)
	hiOff, hi := size, total
	// Ask for the part the first guess is in rather than wait for the
	// download to reach it: the seek sounds at once, if the server sends
	// the song from a byte.
	if target > 0 && total > 0 {
		s.dl.seekTo(s.start + int64(float64(size-s.start)*float64(target)/float64(total)))
	}
	found := false
	for tries := 0; tries < 8 && target-lo > int64(s.info.SampleRate) && hi > lo; tries++ {
		guess := loOff + int64(float64(hiOff-loOff)*float64(target-lo)/float64(hi-lo))
		off, at, ok := s.frameAfter(max(guess, loOff+1), hiOff)
		if !ok {
			break
		}
		found = true
		if at > target {
			hiOff, hi = off, at
		} else {
			loOff, lo = off, at
		}
	}
	if !found && target > int64(s.info.SampleRate) && s.cutShort() {
		return s.seekFailed()
	}
	s.readFrom.pos = loOff
	if s.r == nil {
		s.r = bufio.NewReaderSize(&s.readFrom, 64<<10)
	} else {
		s.r.Reset(&s.readFrom)
	}
	s.pos, s.skip, s.pend = target, target-lo, s.pend[:0]
	s.failure.clear()
	return nil
}

// seekFailed ends a seek that cannot be made, reporting what stopped the
// download: the decoder takes the error as the song's end rather than ask
// for the same position again.
func (s *flacStream) seekFailed() error {
	err := cmp.Or(s.dl.Err(), io.ErrUnexpectedEOF)
	s.failure.record(err)
	return err
}

// cutShort reports whether the download stopped before the whole song came
// in, as one whose length the API declared and the server did not send does.
func (s *flacStream) cutShort() bool {
	_, in, stopped := s.dl.arrived()
	return stopped && s.dl.size > 0 && in < s.dl.size
}

// frameAfter returns where the first whole frame at or after off and
// before end starts, and the frame of sound it starts with. Bytes that
// only look like a frame's header within the audio fail its checksums.
// It reads the song a piece at a time, so the download is locked once per
// piece rather than once per byte.
func (s *flacStream) frameAfter(off, end int64) (int64, int64, bool) {
	if from, _, _ := s.dl.arrived(); off < from {
		// The download starts after off: what came before it was dropped,
		// and is not there to be looked at.
		off = from
	}
	w := window{dl: s.dl, b: s.scan}
	defer func() { s.scan = w.b }()
	for off+2 < end {
		if s.dl.waitFor(off+2) < off+2 {
			return 0, 0, false
		}
		b := w.at(off, int(min(int64(windowPiece), end-off)))
		if len(b) < 2 {
			return 0, 0, false
		}
		for i := 0; i+1 < len(b) && off+int64(i)+2 < end; i++ {
			if b[i] != 0xff || b[i+1]&0xfe != 0xf8 {
				continue
			}
			if at, ok := s.frameAt(off + int64(i)); ok {
				return off + int64(i), at, true
			}
		}
		// A piece may end inside a header: its last byte is looked at again
		// with the piece after it.
		off += int64(len(b)) - 1
	}
	return 0, 0, false
}

// frameAt returns the frame of sound the frame of FLAC at off starts with,
// if a whole one is there. The decoder may panic on bytes that only
// passed the header's checksum.
func (s *flacStream) frameAt(off int64) (at int64, ok bool) {
	defer func() {
		if recover() != nil {
			ok = false
		}
	}()
	if s.lookR == nil {
		s.lookFrom = downloadReader{d: s.dl}
		s.lookR = bufio.NewReaderSize(&s.lookFrom, 16<<10)
	}
	s.lookFrom.pos = off
	s.lookR.Reset(&s.lookFrom)
	f, err := s.parse(s.lookR)
	if err != nil || f.Channels.Count() != int(s.info.NChannels) {
		return 0, false
	}
	return int64(f.SampleNumber()), true
}

func (s *flacStream) Close() error {
	s.once.Do(s.drop)
	return nil
}

func (s *flacStream) source() (*download, *spectrum) { return s.dl, s.spec }

// cut returns why the download was cut off, or nil: the song itself ended.
func (s *flacStream) cut() error { return cmp.Or(s.dl.Err(), s.failure.load()) }
