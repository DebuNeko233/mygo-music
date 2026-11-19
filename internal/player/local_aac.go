package player

import (
	"context"
	"errors"
	"io"
	"sync"

	"github.com/egoist/mygo/audio/aac"
	"github.com/egoist/mygo/audio/mp4"
	faad2 "github.com/llehouerou/go-faad2"
)

var registerLocalAAC sync.Once

// MyGo's small AAC-LC decoder remains the normal path. FAAD2 handles its
// explicitly rejected HE-AAC variants; the shared MP4 parser still owns the
// sample table, priming and seeks.
func registerLocalAACCodec() {
	registerLocalAAC.Do(func() { mp4.RegisterCodec("mp4a", newLocalAACCodec) })
}

func newLocalAACCodec(config []byte) (mp4.Codec, error) {
	base, err := aac.NewCodec(config)
	if err == nil {
		return base, nil
	}
	codec, fallbackErr := newFAADCodec(config)
	if fallbackErr != nil {
		return nil, errors.Join(err, fallbackErr)
	}
	return codec, nil
}

type faadCodec struct {
	config  []byte
	dec     *faad2.Decoder
	rate    int
	scale   int
	packets int
}

func newFAADCodec(config []byte) (*faadCodec, error) {
	c := &faadCodec{config: append([]byte(nil), config...), scale: aacSBRScale(config)}
	if err := c.reset(); err != nil {
		return nil, err
	}
	c.rate = int(c.dec.SampleRate()) * c.scale
	c.packets = aac.FrameLength * c.scale
	if c.rate <= 0 {
		return nil, errors.New("aac: invalid decoder sample rate")
	}
	return c, nil
}

func (c *faadCodec) SampleRate() int   { return c.rate }
func (c *faadCodec) Preroll() int      { return 1 }
func (c *faadCodec) PacketFrames() int { return c.packets }
func (c *faadCodec) Reset()            { _ = c.reset() }
func (c *faadCodec) reset() error {
	if c.dec != nil {
		c.dec.Close(context.Background())
	}
	dec, err := faad2.NewDecoder(context.Background())
	if err != nil {
		return err
	}
	if err := dec.Init(context.Background(), c.config); err != nil {
		dec.Close(context.Background())
		return err
	}
	c.dec = dec
	return nil
}

func (c *faadCodec) Decode(dst []float32, packet []byte) ([]float32, error) {
	pcm, err := c.dec.Decode(context.Background(), packet)
	if err != nil {
		return dst, err
	}
	channels := max(int(c.dec.Channels()), 1)
	for i := 0; i+channels <= len(pcm); i += channels {
		left := pcm[i]
		right := left
		if channels > 1 {
			right = pcm[i+1]
		}
		dst = append(dst, float32(left)/(1<<15), float32(right)/(1<<15))
	}
	return dst, nil
}

// AudioSpecificConfig carries HE-AAC's SBR extension as syncExtensionType
// 0x2b7. The output is twice the base AAC-LC rate; PS changes the channel
// count but not the sample rate. LATM is a different transport, not ASC.
func aacSBRScale(config []byte) int {
	read := func(at, count int) (uint64, bool) {
		if at < 0 || count < 1 || at+count > len(config)*8 {
			return 0, false
		}
		var value uint64
		for i := range count {
			value = value<<1 | uint64(config[(at+i)/8]>>(7-(at+i)%8)&1)
		}
		return value, true
	}
	at := 5
	frequency, ok := read(at, 4)
	at += 4
	if !ok {
		return 1
	}
	if frequency == 15 {
		at += 24
	}
	at += 4 // channel configuration
	extension, ok := read(at, 11)
	if !ok || extension != 0x2b7 {
		return 1
	}
	at += 11
	kind, ok := read(at, 5)
	if !ok || kind != 5 {
		return 1
	}
	at += 5
	present, _ := read(at, 1)
	if present == 1 {
		return 2
	}
	return 1
}

// adtsFAADStream indexes ADTS packets for HE-AAC seeking. It keeps only byte
// offsets, and recreates the decoder for a seek to discard SBR history.
type adtsFAADStream struct {
	dl      *download
	file    io.ReaderAt
	offs    []int64
	sizes   []int
	config  []byte
	dec     *faad2.Decoder
	rate    int
	frames  int64
	pos     int64
	next    int
	skip    int64
	pend    []float32
	spec    *spectrum
	once    sync.Once
	failure decoderFailure
}

func openFAADADTS(dl *download) (*adtsFAADStream, error) {
	dl.mu.Lock()
	file := dl.file
	dl.mu.Unlock()
	if file == nil {
		return nil, io.ErrClosedPipe
	}
	s := &adtsFAADStream{dl: dl, file: file}
	var h [7]byte
	for off := id3Size(dl); off+7 <= dl.size; {
		if _, err := file.ReadAt(h[:], off); err != nil {
			return nil, err
		}
		length, config, ok := adtsPacket(h[:])
		if !ok {
			return nil, io.ErrUnexpectedEOF
		}
		if s.config == nil {
			s.config = config
		}
		s.offs = append(s.offs, off)
		if off+int64(length) > dl.size {
			break
		}
		s.sizes = append(s.sizes, length)
		off += int64(length)
	}
	if len(s.offs) == 0 {
		return nil, errors.New("aac: empty ADTS stream")
	}
	if err := s.resetDecoder(0); err != nil {
		return nil, err
	}
	// ADTS has no explicit SBR flag. Decode the first packet to discover
	// FAAD2's upsampled output rate before exposing the stream's length.
	packet, err := s.packet(0)
	if err != nil {
		return nil, err
	}
	pcm, err := s.dec.Decode(context.Background(), packet)
	if err != nil {
		return nil, err
	}
	s.rate = int(s.dec.SampleRate())
	s.next = 1
	s.pend = adtsPCM(pcm, int(s.dec.Channels()))
	if n := len(pcm) / max(int(s.dec.Channels()), 1); n > 0 && s.rate > 0 {
		s.rate = n * s.rate / aac.FrameLength
	}
	if s.rate <= 0 {
		return nil, errors.New("aac: invalid HE-AAC output rate")
	}
	s.frames = int64(len(s.offs)) * aac.FrameLength * int64(s.rate/int(s.dec.SampleRate()))
	s.spec = newSpectrum(s.rate)
	return s, nil
}

func adtsPacket(h []byte) (length int, config []byte, ok bool) {
	if len(h) < 7 || h[0] != 0xff || h[1]&0xf6 != 0xf0 {
		return 0, nil, false
	}
	frequency := int(h[2] >> 2 & 15)
	channels := int(h[2]&1)<<2 | int(h[3]>>6)
	length = int(h[3]&3)<<11 | int(h[4])<<3 | int(h[5]>>5)
	if frequency >= 13 || channels < 1 || channels > 2 || length < 7 {
		return 0, nil, false
	}
	profile := uint16(h[2]>>6) + 1
	asc := profile<<11 | uint16(frequency)<<7 | uint16(channels)<<3
	return length, []byte{byte(asc >> 8), byte(asc)}, true
}

func (s *adtsFAADStream) resetDecoder(at int) error {
	if s.dec != nil {
		s.dec.Close(context.Background())
	}
	dec, err := faad2.NewDecoder(context.Background())
	if err != nil {
		return err
	}
	if err := dec.Init(context.Background(), s.config); err != nil {
		dec.Close(context.Background())
		return err
	}
	s.dec, s.next, s.pend = dec, at, nil
	return nil
}

func (s *adtsFAADStream) packet(i int) ([]byte, error) {
	header := make([]byte, 7)
	if _, err := s.file.ReadAt(header, s.offs[i]); err != nil {
		return nil, err
	}
	n := 7
	if header[1]&1 == 0 {
		n = 9
	}
	b := make([]byte, s.sizes[i]-n)
	_, err := s.file.ReadAt(b, s.offs[i]+int64(n))
	return b, err
}

func adtsPCM(pcm []int16, channels int) []float32 {
	channels = max(channels, 1)
	out := make([]float32, 0, len(pcm)/channels*2)
	for i := 0; i+channels <= len(pcm); i += channels {
		left := pcm[i]
		right := left
		if channels > 1 {
			right = pcm[i+1]
		}
		out = append(out, float32(left)/(1<<15), float32(right)/(1<<15))
	}
	return out
}

func (s *adtsFAADStream) SampleRate() int { return s.rate }
func (s *adtsFAADStream) Channels() int   { return 2 }
func (s *adtsFAADStream) Len() int64      { return s.frames }
func (s *adtsFAADStream) Read(dst []float32) (int, error) {
	for len(s.pend) == 0 {
		if s.next >= len(s.offs) {
			return 0, io.EOF
		}
		packet, err := s.packet(s.next)
		if err != nil {
			s.failure.record(err)
			return 0, err
		}
		s.next++
		pcm, err := s.dec.Decode(context.Background(), packet)
		if err != nil {
			s.failure.record(err)
			return 0, err
		}
		s.pend = adtsPCM(pcm, int(s.dec.Channels()))
		drop := min(s.skip, int64(len(s.pend)/2))
		s.skip -= drop
		s.pend = s.pend[drop*2:]
	}
	n := copy(dst[:len(dst)/2*2], s.pend)
	s.pend = s.pend[n:]
	s.spec.feed(s.pos, dst[:n])
	s.pos += int64(n / 2)
	return n, nil
}
func (s *adtsFAADStream) SetPosition(frame int64) error {
	frame = min(max(frame, 0), s.frames)
	packet := int(frame / aac.FrameLength)
	from := max(packet-1, 0)
	if err := s.resetDecoder(from); err != nil {
		return err
	}
	s.pos, s.skip = frame, frame-int64(from)*aac.FrameLength
	s.failure.clear()
	return nil
}
func (s *adtsFAADStream) Close() error {
	s.once.Do(func() {
		if s.dec != nil {
			s.dec.Close(context.Background())
		}
		s.dl.Close()
	})
	return nil
}
func (s *adtsFAADStream) source() (*download, *spectrum) { return s.dl, s.spec }
func (s *adtsFAADStream) cut() error                     { return cmpOr(s.dl.Err(), s.failure.load()) }
