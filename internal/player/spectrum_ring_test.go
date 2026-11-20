package player

import (
	"math"
	"testing"
	"time"
)

// The spectrum keeps the last seconds decoded, however long the audio: hours
// in, it measures what plays, forgets what played long ago, and measures again
// after a seek back.
func TestSpectrumKeepsRecentChunks(t *testing.T) {
	const rate = 44100
	s := newSpectrum(rate)
	if keep := time.Duration(len(s.ring)) * fftSize * time.Second / rate; keep < spectrumKeep || keep > spectrumKeep+time.Second {
		t.Fatalf("keeps %v of chunks, want about %v", keep, spectrumKeep)
	}
	tone := func(at int64, frames int) {
		stereo := make([]float32, frames*2)
		for i := range frames {
			v := float32(0.5 * math.Sin(2*math.Pi*440*float64(at+int64(i))/rate))
			stereo[2*i], stereo[2*i+1] = v, v
		}
		s.feed(at, stereo)
	}
	measured := func(at time.Duration) bool {
		_, ok := s.at(int64(at.Seconds() * rate))
		return ok
	}

	// Ten hours in, as an audiobook seeks to where it was left.
	start := int64(10 * 60 * 60 * rate)
	tone(start, 30*rate)
	end := time.Duration(start/rate)*time.Second + 30*time.Second
	if !measured(end - time.Second) {
		t.Error("a second before the end decoded isn't measured")
	}
	if measured(end - 25*time.Second) {
		t.Error("25 seconds before the end decoded is still kept")
	}
	if measured(end + time.Second) {
		t.Error("what isn't decoded yet is measured")
	}
	if !measured(end - spectrumKeep + time.Second) {
		t.Errorf("%v before the end decoded isn't kept", spectrumKeep-time.Second)
	}

	// A seek back decodes, and measures, again.
	back := time.Duration(start/rate)*time.Second + 2*time.Second
	tone(int64(back.Seconds()*rate), rate)
	if !measured(back + 500*time.Millisecond) {
		t.Error("what plays after a seek back isn't measured")
	}
}

// Unknown-length streams keep measuring, with a bounded history at both
// ordinary and high sample rates, without sampling process-wide allocations.
func TestSpectrumUnknownLength(t *testing.T) {
	for _, rate := range []int{44100, 192000} {
		s := newSpectrum(rate)
		if keep := time.Duration(len(s.ring)) * fftSize * time.Second / time.Duration(rate); keep < spectrumKeep || keep > spectrumKeep+time.Second {
			t.Fatalf("%dHz spectrum keeps %v, want about %v", rate, keep, spectrumKeep)
		}
		// With no known duration, measurement must continue beyond two chunks.
		for at := int64(0); at < int64(rate); at += fftSize {
			s.feed(at, make([]float32, fftSize*2))
		}
		if _, ok := s.at(int64(rate / 2)); !ok {
			t.Fatal("unknown-length stream stopped measuring")
		}
	}
}

// Until the detailed display is requested, analysis skips its 64 bands and their layout.
func TestSpectrumDetailIsOnDemand(t *testing.T) {
	s := newSpectrum(44100)
	if s.detailEnabled() {
		t.Fatal("a new spectrum already has detail enabled")
	}
	stereo := make([]float32, fftSize*2)
	s.feed(0, stereo)
	m, ok := s.at(0)
	if !ok {
		t.Fatal("the coarse measurement is missing")
	}
	for _, level := range m.detail {
		if level != 0 {
			t.Fatal("an unrequested detailed spectrum measured its bands")
		}
	}
	s.enableDetail()
	if !s.detailEnabled() {
		t.Fatal("enabling detail did not initialize its layout")
	}
	s.feed(fftSize, stereo)
	m, ok = s.at(fftSize)
	if !ok {
		t.Fatal("the detailed measurement is missing")
	}
	for i := range fftSize {
		stereo[2*i], stereo[2*i+1] = 0.5, 0.5
	}
	s.feed(2*fftSize, stereo)
	m, ok = s.at(2 * fftSize)
	if !ok {
		t.Fatal("the enabled measurement is missing")
	}
	for _, level := range m.detail {
		if level != 0 {
			return
		}
	}
	t.Fatal("enabling detail did not measure the 64 bands")
}

// The detailed spectrum resolves two tones that occupy the same original band.
func TestSpectrumDetailSeparatesTones(t *testing.T) {
	s := newSpectrum(44100)
	s.enableDetail()
	peak := func(bands []float32) int {
		best := 0
		for i, v := range bands {
			if v > bands[best] {
				best = i
			}
		}
		return best
	}
	var coarse, detail [2]int
	for chunk, bin := range []int{170, 195} {
		stereo := make([]float32, fftSize*2)
		for i := range fftSize {
			v := float32(0.10 * math.Sin(2*math.Pi*float64(bin*i)/fftSize))
			stereo[2*i], stereo[2*i+1] = v, v
		}
		at := int64(chunk * fftSize)
		s.feed(at, stereo)
		m, ok := s.at(at)
		if !ok {
			t.Fatal("the measured chunk is missing")
		}
		coarse[chunk], detail[chunk] = peak(m.bands[:]), peak(m.detail[:])
	}
	if coarse[0] != coarse[1] || detail[0] >= detail[1] {
		t.Errorf("coarse peaks %v and detailed peaks %v did not resolve as expected", coarse, detail)
	}
}

// The detailed display adds 4.5 dB per octave across its band centers and keeps the old gain at 1 kHz.
func TestSpectrumDetailTilt(t *testing.T) {
	const rate = 44100
	s := newSpectrum(rate)
	s.enableDetail()
	center := func(i int) float64 {
		from, to := s.detail[i]*rate/fftSize, s.detail[i+1]*rate/fftSize
		return math.Sqrt((700+from)*(700+to)) - 700
	}
	for _, pair := range [][2]int{{4, 17}, {16, 32}, {32, 52}, {48, 62}} {
		from, to := pair[0], pair[1]
		dbPerOctave := 20 * math.Log10(s.tilt[to]/s.tilt[from]) / math.Log2(center(to)/center(from))
		if math.Abs(dbPerOctave-4.5) > 0.01 {
			t.Errorf("bands %d–%d tilt by %.2f dB/oct, want 4.5", from, to, dbPerOctave)
		}
	}
	// 1 kHz falls between these centers. Interpolate the old band gain
	// in the same scale used to space the bands.
	for hi := 1; hi < DetailBands; hi++ {
		if center(hi) < 1000 {
			continue
		}
		low := hi - 1
		mel := func(f float64) float64 { return math.Log1p(f / 700) }
		mix := (mel(1000) - mel(center(low))) / (mel(center(hi)) - mel(center(low)))
		old := 1 + 4*(float64(low)+mix)/DetailBands
		got := s.tilt[hi] / math.Pow(10, 4.5*math.Log2(center(hi)/1000)/20)
		if math.Abs(got-old) > 0.01 {
			t.Errorf("1 kHz gain = %.3f, want the previous %.3f", got, old)
		}
		return
	}
	t.Fatal("the detailed spectrum did not reach 1 kHz")
}

func BenchmarkSpectrumDetailOff(b *testing.B) {
	s := newSpectrum(44100)
	s.acc = s.acc[:fftSize]
	b.ReportAllocs()
	for b.Loop() {
		s.analyze(0)
	}
}

func BenchmarkSpectrumDetailOn(b *testing.B) {
	s := newSpectrum(44100)
	s.enableDetail()
	s.acc = s.acc[:fftSize]
	b.ReportAllocs()
	for b.Loop() {
		s.analyze(0)
	}
}

func BenchmarkNewSpectrum44100(b *testing.B) {
	b.ReportAllocs()
	for b.Loop() {
		newSpectrum(44100)
	}
}
