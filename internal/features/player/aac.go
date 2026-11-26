/*
实现 AAC 与 HE-AAC 解码及 ADTS 流读取，管理解码器回退、定位和资源释放。
*/

package player

import (
	"context"
	"errors"
	"io"
	"sync"
	"sync/atomic"

	"github.com/egoist/mygo/audio/aac"
	"github.com/egoist/mygo/audio/mp4"
	faad2 "github.com/llehouerou/go-faad2"
)

var (
	registerAAC    sync.Once
	aacFactoryMu   sync.RWMutex
	aacFactory     = newAACCodec
	newADTSDecoder = faad2.NewDecoder // tests observe failed-open cleanup
)

// MyGo's small AAC-LC decoder remains the normal path. FAAD2 handles its
// explicitly rejected HE-AAC variants; the shared MP4 parser still owns the
// sample table, priming and seeks.
func registerAACCodec() {
	registerAAC.Do(func() {
		mp4.RegisterCodec("mp4a", func(config []byte) (mp4.Codec, error) {
			aacFactoryMu.RLock()
			factory := aacFactory
			aacFactoryMu.RUnlock()
			return factory(config)
		})
	})
}

func newAACCodec(config []byte) (mp4.Codec, error) {
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
func (c *faadCodec) Close() error {
	if c.dec == nil {
		return nil
	}
	err := c.dec.Close(context.Background())
	c.dec = nil
	return err
}
func (c *faadCodec) reset() error {
	if c.dec != nil {
		c.dec.Close(context.Background())
	}
	dec, err := openFAADDecoder(c.config, faad2.NewDecoder)
	if err != nil {
		return err
	}
	c.dec = dec
	return nil
}

// openFAADDecoder owns a new decoder until its configuration is initialized.
func openFAADDecoder(config []byte, create func(context.Context) (*faad2.Decoder, error)) (*faad2.Decoder, error) {
	ctx := context.Background()
	dec, err := create(ctx)
	if err != nil {
		return nil, err
	}
	if err := dec.Init(ctx, config); err != nil {
		dec.Close(ctx)
		return nil, err
	}
	return dec, nil
}

func (c *faadCodec) Decode(dst []float32, packet []byte) ([]float32, error) {
	pcm, err := c.dec.Decode(context.Background(), packet)
	if err != nil {
		return dst, err
	}
	return adtsPCM(dst, pcm, int(c.dec.Channels())), nil
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

// adtsStream uses the existing AAC-LC codec and HE-AAC fallback with one
// packet index. In-flight sources extend that index only as playback or seeking
// needs it; complete local/cache files can expose their exact length at once.
type adtsStream struct {
	dl           *download
	file         io.ReaderAt
	size         int64
	indexAt      int64
	eof          bool
	offs         []int64
	sizes        []int
	config       []byte
	lc           mp4.Codec
	dec          *faad2.Decoder
	rate         int
	frames       atomic.Int64
	packetFrames int64
	priming      int64
	pos          int64
	next         int
	skip         int64
	header       [7]byte
	packetBuf    []byte
	out          []float32
	pend         []float32
	spec         *spectrum
	drop         func()
	once         sync.Once
	failure      decoderFailure
}

func openAACStreamSpectrum(dl *download, measure bool) (_ *adtsStream, err error) {
	dl.mu.Lock()
	size, complete := dl.length, dl.done
	dl.mu.Unlock()
	s := &adtsStream{dl: dl, file: dl.decoderSource(), size: size, indexAt: id3Size(dl)}
	s.frames.Store(-1)
	if ok, err := s.havePacket(0); err != nil {
		return nil, err
	} else if !ok {
		return nil, errors.New("aac: empty ADTS stream")
	}
	if complete {
		for !s.eof {
			if _, err := s.scanPacket(); err != nil {
				return nil, err
			}
		}
	}
	// An implicit HE-AAC extension may only be rejected when its first packet is
	// decoded. The fallback reuses this packet index instead of scanning again.
	s.lc, err = aac.NewCodec(s.config)
	if err == nil {
		packet, readErr := s.packet(0)
		if readErr != nil {
			return nil, readErr
		}
		s.pend, err = s.decodePacket(packet)
		s.next = 1
	}
	if err != nil || len(s.pend) == 0 {
		s.lc = nil
		if err := s.resetDecoder(0); err != nil {
			return nil, err
		}
	}
	defer func() {
		if err != nil && s.dec != nil {
			s.dec.Close(context.Background())
		}
	}()
	for len(s.pend) == 0 {
		ok, readErr := s.havePacket(s.next)
		if readErr != nil {
			return nil, readErr
		}
		if !ok {
			return nil, errors.New("aac: missing decoded audio")
		}
		packet, readErr := s.packet(s.next)
		if readErr != nil {
			return nil, readErr
		}
		s.next++
		s.pend, err = s.decodePacket(packet)
		if err != nil {
			return nil, err
		}
	}
	if s.lc != nil {
		s.rate = s.lc.SampleRate()
	} else {
		s.rate = len(s.pend) / 2 * int(s.dec.SampleRate()) / aac.FrameLength
	}
	s.packetFrames = int64(len(s.pend) / 2)
	s.priming = int64(s.next - 1)
	if s.packetFrames <= 0 || s.rate <= 0 {
		return nil, errors.New("aac: invalid decoder output")
	}
	s.updateLength()
	if measure {
		s.spec = newSpectrum(s.rate)
	}
	return s, nil
}

// scanPacket extends the shared index by one header, without rereading earlier
// headers or waiting for unrelated packets at the end of an active download.
func (s *adtsStream) scanPacket() (bool, error) {
	if s.eof {
		return false, nil
	}
	if s.size >= 0 && s.indexAt+7 > s.size {
		s.eof = true
		s.updateLength()
		return false, nil
	}
	h := s.header[:]
	n, err := s.file.ReadAt(h, s.indexAt)
	if n < len(h) {
		if err == io.EOF || err == io.ErrUnexpectedEOF {
			s.eof = true
			s.updateLength()
			return false, nil
		}
		return false, cmpOr(err, io.ErrUnexpectedEOF)
	}
	if err != nil {
		return false, err
	}
	// TagLib and other taggers append an ID3v1 tag to ADTS. It is metadata,
	// not a corrupt AAC packet. Accept it only when it is exactly the tail.
	if string(h[:3]) == "TAG" {
		var tail [129]byte
		n, e := s.file.ReadAt(tail[:], s.indexAt)
		if n == 128 && errors.Is(e, io.EOF) {
			s.eof = true
			s.updateLength()
			return false, nil
		}
	}
	length, config, ok := adtsPacket(h[:])
	if !ok {
		return false, io.ErrUnexpectedEOF
	}
	if s.size >= 0 && int64(length) > s.size-s.indexAt {
		s.eof = true
		s.updateLength()
		return false, nil
	}
	if s.config == nil {
		s.config = append([]byte(nil), config[:]...)
	}
	s.offs = append(s.offs, s.indexAt)
	s.sizes = append(s.sizes, length)
	s.indexAt += int64(length)
	if s.size >= 0 && s.indexAt == s.size {
		s.eof = true
		s.updateLength()
	}
	return true, nil
}

func (s *adtsStream) havePacket(i int) (bool, error) {
	for i >= len(s.offs) {
		ok, err := s.scanPacket()
		if err != nil || !ok {
			return false, err
		}
	}
	return true, nil
}

func (s *adtsStream) updateLength() {
	if s.eof && s.packetFrames > 0 {
		s.frames.Store(max(0, int64(len(s.offs))-s.priming) * s.packetFrames)
	}
}

func (s *adtsStream) decodePacket(packet []byte) ([]float32, error) {
	if s.lc != nil {
		var err error
		s.out, err = s.lc.Decode(s.out[:0], packet)
		return s.out, err
	}
	pcm, err := s.dec.Decode(context.Background(), packet)
	s.out = adtsPCM(s.out[:0], pcm, int(s.dec.Channels()))
	return s.out, err
}

func adtsPacket(h []byte) (length int, config [2]byte, ok bool) {
	if len(h) < 7 || h[0] != 0xff || h[1]&0xf6 != 0xf0 {
		return 0, config, false
	}
	frequency := int(h[2] >> 2 & 15)
	channels := int(h[2]&1)<<2 | int(h[3]>>6)
	length = int(h[3]&3)<<11 | int(h[4])<<3 | int(h[5]>>5)
	if frequency >= 13 || channels < 1 || length < 7 || h[1]&1 == 0 && length < 9 {
		return 0, config, false
	}
	profile := uint16(h[2]>>6) + 1
	asc := profile<<11 | uint16(frequency)<<7 | uint16(channels)<<3
	return length, [2]byte{byte(asc >> 8), byte(asc)}, true
}

func (s *adtsStream) resetDecoder(at int) error {
	if s.lc != nil {
		s.lc.Reset()
		s.next, s.pend = at, s.pend[:0]
		return nil
	}
	if s.dec != nil {
		s.dec.Close(context.Background())
	}
	dec, err := openFAADDecoder(s.config, newADTSDecoder)
	if err != nil {
		return err
	}
	s.dec, s.next, s.pend = dec, at, s.pend[:0]
	return nil
}

func (s *adtsStream) packet(i int) ([]byte, error) {
	header := s.header[:]
	if _, err := s.file.ReadAt(header, s.offs[i]); err != nil {
		return nil, err
	}
	n := 7
	if header[1]&1 == 0 {
		n = 9
	}
	need := s.sizes[i] - n
	if cap(s.packetBuf) < need {
		s.packetBuf = make([]byte, need)
	}
	b := s.packetBuf[:need]
	_, err := s.file.ReadAt(b, s.offs[i]+int64(n))
	return b, err
}

func adtsPCM(dst []float32, pcm []int16, channels int) []float32 {
	channels = max(channels, 1)
	out := dst
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

func (s *adtsStream) SampleRate() int { return s.rate }
func (s *adtsStream) Channels() int   { return 2 }
func (s *adtsStream) Len() int64      { return s.frames.Load() }
func (s *adtsStream) Read(dst []float32) (int, error) {
	for len(s.pend) == 0 {
		if frames := s.Len(); frames >= 0 && s.pos >= frames {
			return 0, io.EOF
		}
		ok, err := s.havePacket(s.next)
		if err != nil {
			s.failure.record(err)
			return 0, err
		}
		if !ok {
			return 0, io.EOF
		}
		packet, err := s.packet(s.next)
		if err != nil {
			s.failure.record(err)
			return 0, err
		}
		s.next++
		pcm, err := s.decodePacket(packet)
		if err != nil {
			s.failure.record(err)
			return 0, err
		}
		s.pend = pcm
		drop := min(s.skip, int64(len(s.pend)/2))
		s.skip -= drop
		s.pend = s.pend[drop*2:]
		if len(s.pend) != 0 {
			break
		}
	}
	n := copy(dst[:len(dst)/2*2], s.pend)
	s.pend = s.pend[n:]
	s.spec.feed(s.pos, dst[:n])
	s.pos += int64(n / 2)
	return n, nil
}
func (s *adtsStream) SetPosition(frame int64) error {
	frame = max(frame, 0)
	if frames := s.Len(); frames >= 0 {
		frame = min(frame, frames)
	}
	packet := int(frame / s.packetFrames)
	if _, err := s.havePacket(packet); err != nil {
		s.failure.record(err)
		return err
	}
	if frames := s.Len(); frames >= 0 {
		frame = min(frame, frames)
		if frame == frames {
			s.pos, s.next, s.skip, s.pend = frame, len(s.offs), 0, s.pend[:0]
			s.failure.clear()
			return nil
		}
	}
	packet = int(frame / s.packetFrames)
	from := max(packet-1, 0)
	if err := s.resetDecoder(from); err != nil {
		s.failure.record(err)
		return err
	}
	s.pos, s.skip = frame, frame-int64(from)*s.packetFrames
	s.failure.clear()
	return nil
}
func (s *adtsStream) Close() error {
	s.once.Do(func() {
		if s.dec != nil {
			s.dec.Close(context.Background())
		}
		if s.drop != nil {
			s.drop()
		} else {
			s.dl.Close()
		}
	})
	return nil
}
func (s *adtsStream) source() (*download, *spectrum) { return s.dl, s.spec }
func (s *adtsStream) cut() error                     { return cmpOr(s.dl.Err(), s.failure.load()) }
