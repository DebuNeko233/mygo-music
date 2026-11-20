package player

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/egoist/mygo/audio"
	"github.com/egoist/mygo/audio/aac"
	"github.com/egoist/mygo/audio/mp4"
	"github.com/jfreymuth/oggvorbis"
)

// Fixtures for compressed formats were generated from a two-second 440 Hz
// sine wave with FFmpeg's libvorbis, AAC and ALAC encoders. HE-AAC comes from
// FFmpeg's public ct_nero-heaac.mp4 sample, trimmed without re-encoding.
func TestLocalPCMFormatsDecodeAndSeek(t *testing.T) {
	for _, test := range []struct {
		name, ext string
		makeFile  func(t *testing.T) []byte
	}{
		{name: "WAV PCM16", ext: "wav", makeFile: func(t *testing.T) []byte {
			return makeWAV(1, 16, 8000, func(frame int) int16 { return int16(frame*97%30000 - 15000) })
		}},
		{name: "WAV float", ext: "wav", makeFile: func(t *testing.T) []byte {
			return makeWAV(3, 32, 8000, func(frame int) int16 { return int16(frame*97%30000 - 15000) })
		}},
		{name: "Ogg Vorbis", ext: "ogg", makeFile: func(t *testing.T) []byte {
			return testAudio(t, "seek.ogg")
		}},
		{name: "AAC ADTS", ext: "aac", makeFile: func(t *testing.T) []byte {
			return testAudio(t, "seek.aac")
		}},
		{name: "AAC M4A", ext: "m4a", makeFile: func(t *testing.T) []byte {
			return testAudio(t, "seek.m4a")
		}},
		{name: "ALAC M4A", ext: "m4a", makeFile: func(t *testing.T) []byte {
			return testAudio(t, "seek-alac.m4a")
		}},
		{name: "HE-AAC M4A", ext: "m4a", makeFile: func(t *testing.T) []byte {
			return testAudio(t, "seek-he.m4a")
		}},
		{name: "HE-AAC ADTS", ext: "aac", makeFile: func(t *testing.T) []byte {
			return testAudio(t, "seek-he.aac")
		}},
		{name: "AAC 3GPP", ext: "m4a", makeFile: func(t *testing.T) []byte {
			return testAudio(t, "seek.3gp")
		}},
		{name: "AAC 3GPP2", ext: "m4a", makeFile: func(t *testing.T) []byte {
			return testAudio(t, "seek.3g2")
		}},
	} {
		t.Run(test.name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "local."+test.ext)
			if err := os.WriteFile(path, test.makeFile(t), 0600); err != nil {
				t.Fatal(err)
			}
			prepared, err := PrepareLocalAudio(path)
			if err != nil {
				t.Fatal(err)
			}
			defer prepared.Close()
			if prepared.Duration <= 0 || prepared.info.Type != test.ext {
				t.Fatalf("duration %v, type %s", prepared.Duration, prepared.info.Type)
			}
			s := prepared.stream
			const position = 101
			if position >= s.Len() {
				t.Fatalf("fixture has only %d frames", s.Len())
			}
			if err := s.SetPosition(position); err != nil {
				t.Fatal(err)
			}
			buf := make([]float32, 512)
			n, err := s.Read(buf)
			if n == 0 || err != nil {
				t.Fatalf("read after seek: %d, %v", n, err)
			}
			if err := s.SetPosition(s.Len()); err != nil {
				t.Fatal(err)
			}
			if n, err := s.Read(buf); n != 0 || err != io.EOF {
				t.Fatalf("end of stream: %d, %v", n, err)
			}
		})
	}
}

// A truncated tail must be excluded from the seekable packets. Indexing its
// offset but not its payload used to make an end-of-file seek decode an empty packet.
func TestLocalHEAACTruncatedTail(t *testing.T) {
	data := testAudio(t, "seek-he.aac")
	path := filepath.Join(t.TempDir(), "local.aac")
	if err := os.WriteFile(path, data[:len(data)-5], 0600); err != nil {
		t.Fatal(err)
	}
	prepared, err := PrepareLocalAudio(path)
	if err != nil {
		t.Fatal(err)
	}
	defer prepared.Close()
	s := prepared.stream
	if err := s.SetPosition(s.Len()); err != nil {
		t.Fatal(err)
	}
	if n, err := s.Read(make([]float32, 512)); n != 0 || err != io.EOF {
		t.Fatalf("read at end: %d, %v", n, err)
	}
	if err := s.SetPosition(s.Len() - 4096); err != nil {
		t.Fatal(err)
	}
	if n, err := s.Read(make([]float32, 512)); n == 0 || err != nil {
		t.Fatalf("read final complete packet: %d, %v", n, err)
	}
}

func makeWAV(format uint16, bits int, rate int, sample func(int) int16) []byte {
	const frames = 1000
	channels, block := 2, 2*(bits/8)
	var pcm bytes.Buffer
	for i := range frames {
		for range channels {
			v := sample(i)
			switch {
			case format == 3:
				binary.Write(&pcm, binary.LittleEndian, float32(v)/(1<<15))
			case bits == 16:
				binary.Write(&pcm, binary.LittleEndian, v)
			}
		}
	}
	var b bytes.Buffer
	b.WriteString("RIFF")
	binary.Write(&b, binary.LittleEndian, uint32(36+pcm.Len()))
	b.WriteString("WAVEfmt ")
	binary.Write(&b, binary.LittleEndian, uint32(16))
	binary.Write(&b, binary.LittleEndian, format)
	binary.Write(&b, binary.LittleEndian, uint16(channels))
	binary.Write(&b, binary.LittleEndian, uint32(rate))
	binary.Write(&b, binary.LittleEndian, uint32(rate*block))
	binary.Write(&b, binary.LittleEndian, uint16(block))
	binary.Write(&b, binary.LittleEndian, uint16(bits))
	b.WriteString("data")
	binary.Write(&b, binary.LittleEndian, uint32(pcm.Len()))
	b.Write(pcm.Bytes())
	return b.Bytes()
}

func testAudio(t *testing.T, name string) []byte {
	t.Helper()
	b, err := os.ReadFile(filepath.Join("testdata", name))
	if err != nil {
		t.Fatal(err)
	}
	return b
}

// WAV keeps its exact PCM samples when the caller reads a small buffer.
func TestLocalWAVMatchesPCMSamples(t *testing.T) {
	want := makeWAV(1, 16, 8000, func(frame int) int16 { return int16(frame*97%30000 - 15000) })
	path := filepath.Join(t.TempDir(), "local.wav")
	if err := os.WriteFile(path, want, 0600); err != nil {
		t.Fatal(err)
	}
	_, s, err := openLocal(path)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	if s.Len() != 1000 || time.Duration(s.Len())*time.Second/time.Duration(s.SampleRate()) != time.Second/8 {
		t.Fatalf("length %d at %d", s.Len(), s.SampleRate())
	}
	got := make([]float32, 6)
	n, err := s.Read(got)
	if n != len(got) || err != nil {
		t.Fatal(n, err)
	}
	for i := range 3 {
		v := int16(i*97%30000 - 15000)
		if got[2*i] != float32(v)/(1<<15) || got[2*i+1] != got[2*i] {
			t.Fatalf("sample %d: %v", i, got)
		}
	}
}

// WAVE_FORMAT_EXTENSIBLE PCM scales 24 valid bits in a 32-bit container by
// their valid range. Ignoring wValidBitsPerSample used to attenuate 0.5 to 1/512.
func TestLocalWAVExtensible24In32(t *testing.T) {
	var pcm bytes.Buffer
	for _, sample := range []int32{0x40000000, 0x7fffff00, -0x40000000, -0x80000000} {
		binary.Write(&pcm, binary.LittleEndian, sample)
		binary.Write(&pcm, binary.LittleEndian, sample)
	}
	var b bytes.Buffer
	b.WriteString("RIFF")
	binary.Write(&b, binary.LittleEndian, uint32(4+8+40+8+pcm.Len()))
	b.WriteString("WAVEfmt ")
	binary.Write(&b, binary.LittleEndian, uint32(40))
	binary.Write(&b, binary.LittleEndian, uint16(0xfffe))
	binary.Write(&b, binary.LittleEndian, uint16(2))
	binary.Write(&b, binary.LittleEndian, uint32(48000))
	binary.Write(&b, binary.LittleEndian, uint32(48000*8))
	binary.Write(&b, binary.LittleEndian, uint16(8))
	binary.Write(&b, binary.LittleEndian, uint16(32))
	binary.Write(&b, binary.LittleEndian, uint16(22))
	binary.Write(&b, binary.LittleEndian, uint16(24))
	binary.Write(&b, binary.LittleEndian, uint32(3))
	b.Write([]byte{1, 0, 0, 0, 0, 0, 0x10, 0, 0x80, 0, 0, 0xaa, 0, 0x38, 0x9b, 0x71})
	b.WriteString("data")
	binary.Write(&b, binary.LittleEndian, uint32(pcm.Len()))
	b.Write(pcm.Bytes())

	path := filepath.Join(t.TempDir(), "local.wav")
	if err := os.WriteFile(path, b.Bytes(), 0600); err != nil {
		t.Fatal(err)
	}
	_, s, err := openLocal(path)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	got := make([]float32, 8)
	n, err := s.Read(got)
	if n != len(got) || err != nil {
		t.Fatal(n, err)
	}
	want := []float32{0.5, 0.5, float32(0x7fffff) / (1 << 23), float32(0x7fffff) / (1 << 23), -0.5, -0.5, -1, -1}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("sample %d = %v, want %v; all samples %v", i, got[i], want[i], got)
		}
	}
}

// Decoder wrappers propagate sample-accurate seeks for the framework's AAC decoders.
func TestLocalDecoderStreamMatchesDirectDecoder(t *testing.T) {
	for _, name := range []string{"seek.aac", "seek.m4a", "seek-alac.m4a"} {
		t.Run(name, func(t *testing.T) {
			b := testAudio(t, name)
			path := filepath.Join(t.TempDir(), name)
			if err := os.WriteFile(path, b, 0600); err != nil {
				t.Fatal(err)
			}
			_, s, err := openLocal(path)
			if err != nil {
				t.Fatal(err)
			}
			defer s.Close()
			var direct audio.Decoder
			switch name {
			case "seek.aac":
				direct, err = aac.NewADTSDecoder(bytes.NewReader(b))
			default:
				direct, err = mp4.NewDecoder(bytes.NewReader(b))
			}
			if err != nil {
				t.Fatal(err)
			}
			const position = 4000
			if direct.Len() <= position {
				t.Skip(fmt.Sprintf("fixture too short: %d", direct.Len()))
			}
			if err := direct.SetPosition(position); err != nil {
				t.Fatal(err)
			}
			want := make([]float32, 4096)
			n, err := direct.Read(want)
			if err != nil {
				t.Fatal(err)
			}
			if err := s.SetPosition(position); err != nil {
				t.Fatal(err)
			}
			got := make([]float32, n)
			m, err := s.Read(got)
			if m != n || err != nil || maxDiff(got, want[:n]) != 0 {
				t.Fatalf("wrapped seek differs: %d/%d, %v, %g", m, n, err, maxDiff(got, want[:n]))
			}
		})
	}
}

// Ogg streams preserve the decoder's exact sample position across a seek.
func TestLocalOggMatchesDirectDecoder(t *testing.T) {
	b := testAudio(t, "seek.ogg")
	direct, err := oggvorbis.NewReader(bytes.NewReader(b))
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), "seek.ogg")
	if err := os.WriteFile(path, b, 0600); err != nil {
		t.Fatal(err)
	}
	_, s, err := openLocal(path)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	const position = 12000
	if direct.Length() <= position {
		t.Skip("fixture too short")
	}
	if err := direct.SetPosition(position); err != nil {
		t.Fatal(err)
	}
	want := make([]float32, 4096)
	n, err := direct.Read(want)
	if err != nil {
		t.Fatal(err)
	}
	if err := s.SetPosition(position); err != nil {
		t.Fatal(err)
	}
	got := make([]float32, n*2/direct.Channels())
	m, err := s.Read(got)
	if m != len(got) || err != nil {
		t.Fatal(m, err)
	}
	for i := range m / 2 {
		if got[2*i] != want[i*direct.Channels()] {
			t.Fatalf("sample %d: %g != %g", i, got[2*i], want[i*direct.Channels()])
		}
	}
}
