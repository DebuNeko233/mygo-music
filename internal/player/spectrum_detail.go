/*
计算 64 频段的 mel 频谱布局与高频补偿，供细节可视化使用。
*/

package player

import "math"

// The 64-band spectrum uses a mel scale and 4.5 dB/oct tilt.
// The tilt keeps the previous gain at 1 kHz.
func detailLayout(rate int, edges, tilt []float64) {
	binHz := float64(rate) / fftSize
	low, high := max(40.0, binHz), min(14000.0, float64(rate)/2)
	mel := func(hz float64) float64 { return math.Log1p(hz / 700) }
	lo, hi := mel(low), mel(high)
	pivot := (mel(detailTiltHz)-lo)/(hi-lo) - 0.5/DetailBands
	pivotGain := 1 + 4*min(float64(DetailBands-1)/DetailBands, max(0, pivot))
	frequency := func(band float64) float64 {
		return 700 * math.Expm1(lo+(hi-lo)*band/float64(len(tilt)))
	}
	for i := range edges {
		edges[i] = frequency(float64(i)) / binHz
	}
	for i := range tilt {
		center := frequency(float64(i) + 0.5)
		tilt[i] = pivotGain * math.Pow(10, detailTiltDB*math.Log2(center/detailTiltHz)/20)
	}
}

func measureDetail(dst []float32, sums, edges, tilt []float64) {
	between := func(at float64) float64 {
		i := int(at)
		return sums[i] + (sums[i+1]-sums[i])*(at-float64(i))
	}
	before := between(edges[0])
	for b := range dst {
		after := between(edges[b+1])
		dst[b] = normalizedBand(after-before, edges[b+1]-edges[b], tilt[b])
		before = after
	}
}
