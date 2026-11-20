package speech

import (
	"math"
	"slices"
	"testing"
)

// tone is seconds of a sine of freq Hz at rate, in stereo.
func tone(freq float64, rate int, seconds float64) []float32 {
	n := int(seconds * float64(rate))
	out := make([]float32, 2*n)
	for i := range n {
		v := float32(0.5 * math.Sin(2*math.Pi*freq*float64(i)/float64(rate)))
		out[2*i], out[2*i+1] = v, v
	}
	return out
}

// rms is the loudness of x, but for its first and last tenth, where the
// filter starts and stops.
func rms(x []float32) float64 {
	x = x[len(x)/10 : len(x)*9/10]
	var sum float64
	for _, v := range x {
		sum += float64(v) * float64(v)
	}
	return math.Sqrt(sum / float64(len(x)))
}

// A song's rate comes down to the models' 16 kHz in mono: speech's
// frequencies as loud as they were, those above 8 kHz gone rather than
// folded down among them.
func TestResample(t *testing.T) {
	for _, rate := range []int{44100, 48000, 16000} {
		out := newResampler(rate, 2).push(nil, tone(1000, rate, 1))
		if n := len(out); math.Abs(float64(n-SampleRate)) > 50 {
			t.Errorf("%d Hz: %d samples for a second", rate, n)
		}
		if got := rms(out); math.Abs(got-0.5/math.Sqrt2) > 0.01 {
			t.Errorf("%d Hz: a 1 kHz tone at %.3f, want %.3f", rate, got, 0.5/math.Sqrt2)
		}
		if rate == SampleRate {
			continue
		}
		if got := rms(newResampler(rate, 2).push(nil, tone(12000, rate, 1))); got > 0.01 {
			t.Errorf("%d Hz: a 12 kHz tone folds down at %.3f", rate, got)
		}
	}
}

// Audio resamples the same in chunks as at once, as it decodes.
func TestResampleChunks(t *testing.T) {
	in := tone(440, 44100, 0.5)
	whole := newResampler(44100, 2).push(nil, in)
	r := newResampler(44100, 2)
	var parts []float32
	for i := 0; i < len(in); i += 2 * 1001 {
		parts = r.push(parts, in[i:min(i+2*1001, len(in))])
	}
	if len(parts) != len(whole) {
		t.Fatalf("%d samples in chunks, %d at once", len(parts), len(whole))
	}
	for i := range whole {
		if math.Abs(float64(parts[i]-whole[i])) > 1e-6 {
			t.Fatalf("sample %d: %v in chunks, %v at once", i, parts[i], whole[i])
		}
	}
}

// The cached prototype produces exactly the original taps at every phase.
func TestResamplePrototypeMatchesReference(t *testing.T) {
	for _, rate := range []int{8000, 16000, 22050, 32000, 44100, 48000} {
		in := tone(440, rate, 0.1)
		mono := make([]float32, 0, len(in)/2)
		for i := 0; i < len(in); i += 2 {
			mono = append(mono, (in[i]+in[i+1])/2)
		}
		r := newResampler(rate, 2)
		r.buf = mono
		want := referenceResample(r)
		got := newResampler(rate, 2).push(nil, in)
		if !slices.Equal(got, want) {
			for i := range want {
				if got[i] != want[i] {
					t.Fatalf("%d Hz: sample %d: got %v, want %v", rate, i, got[i], want[i])
				}
			}
		}
	}
}

// referenceResample evaluates the original windowed sinc at every output.
func referenceResample(r *resampler) []float32 {
	var out []float32
	for {
		center := int64(math.Floor(r.t))
		if center+int64(r.half) >= r.base+int64(len(r.buf)) {
			return out
		}
		var acc, norm float64
		for j := center - int64(r.half) + 1; j <= center+int64(r.half); j++ {
			w := r.tap(r.t - float64(j))
			norm += w
			if k := j - r.base; k >= 0 {
				acc += w * float64(r.buf[k])
			}
		}
		out = append(out, float32(acc/norm))
		r.t += r.step
	}
}

// Measure the resampling work for one second of stereo input.
func BenchmarkResample(b *testing.B) {
	in := tone(440, 48000, 1)
	b.ReportAllocs()
	for b.Loop() {
		newResampler(48000, 2).push(nil, in)
	}
}
