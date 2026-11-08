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
