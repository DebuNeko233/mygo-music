package speech

import "math"

// A resampler turns interleaved audio of a few channels at one rate into
// mono at SampleRate, as the models hear it: averaged, then filtered of
// what is above half the new rate, which would alias, by a windowed sinc.
// It keeps what it needs of one chunk for the next, so chunks resample as
// the whole would.
type resampler struct {
	ch    int
	step  float64 // the input's samples to each of the output's
	fc    float64 // the cutoff, in cycles per input sample
	half  int     // the filter's half-width, in input samples
	buf   []float32
	base  int64   // the input sample buf[0] is
	t     float64 // where the next output sample is, in input samples
	phase int
	taps  [][]float64 // one filter for each phase of the rate ratio
	fracs []float64   // the fractional position each filter was calculated at
}

func newResampler(rate, channels int) *resampler {
	if rate <= 0 {
		rate = SampleRate // Fallback to avoid division by zero
	}
	if channels <= 0 {
		channels = 1
	}
	step := float64(rate) / SampleRate
	r := &resampler{ch: channels, step: step}
	// Just under the output's Nyquist frequency, with eight of its
	// periods on either side.
	r.fc = 0.45 / max(step, 1)
	r.half = int(math.Ceil(8 * max(step, 1)))
	// Each fractional phase has its own prototype. Some rational rates
	// acquire floating-point rounding as their positions accumulate; those
	// taps are refreshed when the fractional position changes.
	phases := SampleRate / gcd(rate, SampleRate)
	r.taps = make([][]float64, phases)
	r.fracs = make([]float64, phases)
	return r
}

// push resamples in, appending what it can of the output to out.
func (r *resampler) push(out, in []float32) []float32 {
	for i := 0; i+r.ch <= len(in); i += r.ch {
		var sum float32
		for c := range r.ch {
			sum += in[i+c]
		}
		r.buf = append(r.buf, sum/float32(r.ch))
	}
	for {
		center := int64(math.Floor(r.t))
		if center+int64(r.half) >= r.base+int64(len(r.buf)) {
			break
		}
		var acc, norm float64
		taps := r.taps[r.phase]
		// Floating-point accumulation can slightly shift a repeating phase.
		// Refresh its taps then, retaining the original arithmetic exactly.
		frac := r.t - float64(center)
		if taps == nil || frac != r.fracs[r.phase] {
			if taps == nil {
				taps = make([]float64, r.half*2)
			}
			for k := range taps {
				j := center - int64(r.half) + 1 + int64(k)
				taps[k] = r.tap(r.t - float64(j))
			}
			r.taps[r.phase] = taps
			r.fracs[r.phase] = frac
		}
		for k, w := range taps {
			j := center - int64(r.half) + 1 + int64(k)
			norm += w
			if k := j - r.base; k >= 0 {
				acc += w * float64(r.buf[k])
			}
		}
		out = append(out, float32(acc/norm))
		r.t += r.step
		r.phase = (r.phase + 1) % len(r.taps)
	}
	// Drop what no output to come reaches.
	if drop := int64(math.Floor(r.t)) - int64(r.half) - r.base; drop > 0 {
		drop = min(drop, int64(len(r.buf)))
		r.buf = r.buf[:copy(r.buf, r.buf[drop:])]
		r.base += drop
	}
	return out
}

// tap is the filter's weight x input samples from where it samples.
func (r *resampler) tap(x float64) float64 {
	n := float64(r.half)
	if x <= -n || x >= n {
		return 0
	}
	s := 2 * r.fc
	if x != 0 {
		s = math.Sin(2*math.Pi*r.fc*x) / (math.Pi * x)
	}
	// Blackman's window.
	p := math.Pi * (x/n + 1)
	return s * (0.42 - 0.5*math.Cos(p) + 0.08*math.Cos(2*p))
}

func gcd(a, b int) int {
	for b != 0 {
		a, b = b, a%b
	}
	return a
}
