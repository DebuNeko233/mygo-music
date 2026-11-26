/*
对解码音频执行频谱分析，缓存近期频段结果供播放器按时间查询。
*/

package player

import (
	"math"
	"math/bits"
	"math/cmplx"
	"sync"
	"time"
)

// The spectrum of a song, measured in chunks of fftSize frames as it
// decodes, a second ahead of what plays: the visualizers look up the
// chunk playing. It keeps only the chunks of the last spectrumKeep
// decoded, in a ring, so that an audiobook of hours holds no more than
// a song does; a seek decodes, and so measures, again from where it lands.

const (
	fftBits      = 10
	fftSize      = 1 << fftBits
	Bands        = 24
	DetailBands  = 64
	spectrumKeep = 10 * time.Second
	detailTiltDB = 4.5 // dB per octave
	detailTiltHz = 1000
)

type spectrum struct {
	mu     sync.RWMutex
	ring   []measured // chunk c in ring[c%len(ring)], if it's still there
	window [fftSize]float32
	acc    []float32 // mono samples of the chunk being filled
	accAt  int64     // its first frame
	edges  [Bands + 1]int
	detail [DetailBands + 1]float64
	tilt   [DetailBands]float64
	fft    [fftSize]complex128
}

// measured is the bands of a chunk, and which chunk: -1 for none yet.
type measured struct {
	chunk  int64
	bands  [Bands]float32
	detail [DetailBands]float32
}

func newSpectrum(rate int) *spectrum {
	n := max(16, int(int64(rate)*int64(spectrumKeep/time.Second)/fftSize)+1)
	s := &spectrum{ring: make([]measured, n), acc: make([]float32, 0, fftSize), accAt: -1}
	for i := range s.ring {
		s.ring[i].chunk = -1
	}
	for i := range s.window {
		s.window[i] = float32(0.5 - 0.5*math.Cos(2*math.Pi*float64(i)/float64(fftSize-1)))
	}
	// Bands spaced evenly in pitch, from 40 Hz to 14 kHz.
	lo, hi := math.Log(40), math.Log(14000)
	for i := range s.edges {
		f := math.Exp(lo + (hi-lo)*float64(i)/Bands)
		s.edges[i] = max(1, min(fftSize/2-1, int(f*fftSize/float64(rate))))
	}
	for i := 1; i < len(s.edges); i++ {
		s.edges[i] = max(s.edges[i], s.edges[i-1]+1)
	}
	detailLayout(rate, s.detail[:], s.tilt[:])
	return s
}

// feed takes stereo samples decoded from frame at, unless the stream
// measures no spectrum.
func (s *spectrum) feed(at int64, stereo []float32) {
	if s == nil {
		return
	}
	if s.accAt < 0 || at != s.accAt+int64(len(s.acc)) {
		// The start, or a seek: begin a chunk at a chunk's boundary.
		s.acc = s.acc[:0]
		skip := (fftSize - at%fftSize) % fftSize
		if int(skip)*2 >= len(stereo) {
			s.accAt = -1
			return
		}
		stereo = stereo[skip*2:]
		at += skip
		s.accAt = at
	}
	for i := 0; i+1 < len(stereo); i += 2 {
		s.acc = append(s.acc, (stereo[i]+stereo[i+1])/2)
		if len(s.acc) == fftSize {
			s.analyze(s.accAt / fftSize)
			s.accAt += fftSize
			s.acc = s.acc[:0]
		}
	}
}

func (s *spectrum) analyze(chunk int64) {
	if chunk < 0 {
		return
	}
	spectrumFFT.transform(&s.fft, (*[fftSize]float32)(s.acc), &s.window)
	// Both displays reuse the same transform and magnitudes. A prefix
	// sum lets the finer buckets split FFT bins at fractional boundaries.
	var sums [fftSize + 1]float64
	end := max(s.edges[Bands], int(math.Ceil(s.detail[DetailBands])))
	for k := range end {
		sums[k+1] = sums[k] + cmplx.Abs(s.fft[k])
	}
	m := measured{chunk: chunk}
	for b := range Bands {
		from, to := s.edges[b], s.edges[b+1]
		// Higher bands carry less energy: tilt them up, as the ear hears.
		m.bands[b] = normalizedBand(sums[to]-sums[from], float64(to-from), 1+float64(b)/6)
	}
	// Decode ahead for both displays: enabling detail at the first draw
	// leaves the audio already buffered without it until playback catches up.
	measureDetail(m.detail[:], sums[:], s.detail[:], s.tilt[:])
	s.mu.Lock()
	s.ring[chunk%int64(len(s.ring))] = m
	s.mu.Unlock()
}

func normalizedBand(sum, width, tilt float64) float32 {
	mag := sum / width / (fftSize / 4) * tilt
	db := 20 * math.Log10(mag+1e-9)
	return float32(max(0, min(1, (db+60)/52)))
}

// at returns the measurement covering frame, or false if it is absent.
func (s *spectrum) at(frame int64) (measured, bool) {
	if s == nil || frame < 0 {
		return measured{}, false
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	c := frame / fftSize
	m := s.ring[c%int64(len(s.ring))]
	if m.chunk != c {
		return measured{}, false
	}
	return m, true
}

// All decoders use the same FFT length. Share the tables across streams.
type spectrumFFTPlan struct {
	order   [fftSize]uint16
	weights [fftSize - 1]complex128
}

var spectrumFFT = newSpectrumFFTPlan()

func newSpectrumFFTPlan() spectrumFFTPlan {
	var plan spectrumFFTPlan
	for i := range plan.order {
		plan.order[i] = bits.Reverse16(uint16(i)) >> (16 - fftBits)
	}
	for half := 1; half < fftSize; half *= 2 {
		step := cmplx.Exp(complex(0, -math.Pi/float64(half)))
		weight := complex(1, 0)
		// The stage uses half weights, after the half-1 from earlier stages.
		for i := range half {
			plan.weights[half-1+i] = weight
			weight *= step
		}
	}
	return plan
}

func (plan *spectrumFFTPlan) transform(dst *[fftSize]complex128, samples, window *[fftSize]float32) {
	// Load directly in bit-reversed order, ready for the first stage.
	for i, sample := range samples {
		dst[plan.order[i]] = complex(float64(sample*window[i]), 0)
	}
	for half := 1; half < fftSize; half *= 2 {
		size := 2 * half
		weights := plan.weights[half-1 : size-1]
		for start := 0; start < fftSize; start += size {
			left, right := dst[start:start+half], dst[start+half:start+size]
			for i, w := range weights {
				a, b := left[i], right[i]*w
				left[i], right[i] = a+b, a-b
			}
		}
	}
}
