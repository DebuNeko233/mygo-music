package player

import (
	"errors"
	"io"
	"os"
	"path/filepath"
	"slices"
	"testing"
)

// PCM conversion appends stereo samples to the caller's existing output,
// duplicates mono input, and ignores an incomplete final multichannel frame.
func TestADTSPMAppendsStereo(t *testing.T) {
	prefix := []float32{0.25, -0.25}
	for _, test := range []struct {
		channels int
		pcm      []int16
		want     []float32
	}{
		{channels: 0, pcm: []int16{-32768, 16384}, want: []float32{-1, -1, 0.5, 0.5}},
		{channels: 1, pcm: []int16{-32768, 16384}, want: []float32{-1, -1, 0.5, 0.5}},
		{channels: 2, pcm: []int16{-32768, 16384, -1, 0, 12345}, want: []float32{-1, 0.5, float32(-1) / (1 << 15), 0}},
	} {
		got := adtsPCM(slices.Clone(prefix), test.pcm, test.channels)
		if !slices.Equal(got[:2], prefix) || !slices.Equal(got[2:], test.want) {
			t.Errorf("channels=%d: got %v, want %v", test.channels, got, append(prefix, test.want...))
		}
	}
}

// The FAAD2 MP4 codec uses the same append conversion as the ADTS stream.
func TestFAADCodecAppendsPCM(t *testing.T) {
	config := testADTSPackets(t)
	codec, err := newFAADCodec(config)
	if err != nil {
		t.Fatal(err)
	}
	defer codec.Close()
	pcm, err := codec.dec.Decode(t.Context(), testADTSPacket(t, 0))
	if err != nil {
		t.Fatal(err)
	}
	codec.Reset()
	prefix := []float32{0.25, -0.25}
	got, err := codec.Decode(slices.Clone(prefix), testADTSPacket(t, 0))
	if err != nil {
		t.Fatal(err)
	}
	want := adtsPCM(nil, pcm, int(codec.dec.Channels()))
	if !slices.Equal(got[:2], prefix) || !slices.Equal(got[2:], want) {
		t.Fatal("codec did not append the packet's converted PCM")
	}
}

// Pending decoded samples survive a short read and a seek clears them before
// decoding at its requested position, including a seek back to the start.
func TestADTSPendingAndSeek(t *testing.T) {
	path := filepath.Join(t.TempDir(), "seek-he.aac")
	if err := os.WriteFile(path, testAudio(t, "seek-he.aac"), 0600); err != nil {
		t.Fatal(err)
	}
	_, raw, err := openLocal(path)
	if err != nil {
		t.Fatal(err)
	}
	s := raw.(*adtsFAADStream)
	defer s.Close()
	whole := readADTS(t, s, 4096)
	if err := s.SetPosition(0); err != nil {
		t.Fatal(err)
	}
	buf := make([]float32, 2)
	var short []float32
	for i := 0; i < 5; i++ {
		n, err := s.Read(buf)
		if err != nil {
			t.Fatal(err)
		}
		short = append(short, buf[:n]...)
	}
	if !slices.Equal(short, whole[:len(short)]) {
		t.Fatal("short reads changed pending PCM")
	}
	for _, frame := range []int64{1, 1023, 1024, 4097, s.Len() / 2, s.Len()} {
		if err := s.SetPosition(frame); err != nil {
			t.Fatal(err)
		}
		got := readADTS(t, s, 333)
		if err := s.SetPosition(frame); err != nil {
			t.Fatal(err)
		}
		again := readADTS(t, s, 333)
		if !slices.Equal(got, again) {
			t.Errorf("seek to %d changed the decoded PCM", frame)
		}
		if s.pos != frame+int64(len(got))/2 {
			t.Errorf("seek to %d: position %d after %d samples", frame, s.pos, len(got))
		}
	}
}

func readADTS(t *testing.T, s *adtsFAADStream, size int) []float32 {
	t.Helper()
	var out []float32
	buf := make([]float32, size)
	for {
		n, err := s.Read(buf)
		out = append(out, buf[:n]...)
		if errors.Is(err, io.EOF) {
			return out
		}
		if err != nil {
			t.Fatal(err)
		}
	}
}

func testADTSPackets(t *testing.T) []byte {
	t.Helper()
	b := testAudio(t, "seek-he.aac")
	_, config, ok := adtsPacket(b)
	if !ok {
		t.Fatal("invalid test ADTS header")
	}
	return config
}

func testADTSPacket(t *testing.T, i int) []byte {
	t.Helper()
	b := testAudio(t, "seek-he.aac")
	at := 0
	for {
		length, _, ok := adtsPacket(b[at:])
		if !ok || at+length > len(b) {
			t.Fatal("invalid test ADTS packet")
		}
		if i == 0 {
			return b[at+7 : at+length]
		}
		at += length
		i--
	}
}

func BenchmarkADTSPCM(b *testing.B) {
	pcm := make([]int16, 2048)
	for i := range pcm {
		pcm[i] = int16(i*7919 - 32768)
	}
	for _, channels := range []int{1, 2} {
		b.Run(map[int]string{1: "mono", 2: "stereo"}[channels], func(b *testing.B) {
			dst := make([]float32, 0, len(pcm)/channels*2)
			b.ReportAllocs()
			b.ResetTimer()
			for b.Loop() {
				dst = adtsPCM(dst[:0], pcm, channels)
			}
		})
	}
}

func BenchmarkFAADCodecDecode(b *testing.B) {
	data := mustReadFile(b, "testdata/seek-he.aac")
	_, config, ok := adtsPacket(data)
	if !ok {
		b.Fatal("invalid test ADTS header")
	}
	codec, err := newFAADCodec(config)
	if err != nil {
		b.Fatal(err)
	}
	defer codec.Close()
	length, _, _ := adtsPacket(data)
	packet := data[7:length]
	dst := make([]float32, 0, 4096)
	b.ReportAllocs()
	b.ResetTimer()
	for b.Loop() {
		dst, err = codec.Decode(dst[:0], packet)
		if err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkADTSStreamRead(b *testing.B) {
	data := mustReadFile(b, "testdata/seek-he.aac")
	b.ReportAllocs()
	b.ResetTimer()
	for b.Loop() {
		s, err := openBaselineADTS(b, data)
		if err != nil {
			b.Fatal(err)
		}
		buf := make([]float32, 4096)
		for {
			_, err := s.Read(buf)
			if errors.Is(err, io.EOF) {
				break
			}
			if err != nil {
				b.Fatal(err)
			}
		}
		s.Close()
	}
}

func mustReadFile(b *testing.B, path string) []byte {
	b.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		b.Fatal(err)
	}
	return data
}
