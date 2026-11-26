/*
保存字幕文本与已覆盖时间段，提供不可变写入快照和增量转写范围计算。
*/

package transcript

import (
	"encoding/json"
	"os"
	"path/filepath"
	"slices"
	"sync"
	"sync/atomic"
	"time"

	"netease-music-mygo/internal/speech"
)

type Span [2]time.Duration
type Transcript struct {
	Model    string           `json:"model"`
	Segments []speech.Segment `json:"segments"`
	Spans    []Span           `json:"spans"`
}
type snapshot struct{ data []byte }

var (
	mu      sync.Mutex
	asked   atomic.Uint64
	landed  = map[string]uint64{}
	pending sync.Map
)

// Reader captures queued data before background I/O, so returning to an episode sees its pending save.
func Reader(path string) func() *Transcript {
	queued, _ := pending.Load(path)
	return func() *Transcript {
		var b []byte
		if queued != nil {
			b = queued.(*snapshot).data
		} else {
			b, _ = os.ReadFile(path)
		}
		tr := new(Transcript)
		if json.Unmarshal(b, tr) != nil || tr.Model != speech.ModelName {
			tr = &Transcript{Model: speech.ModelName}
		}
		return tr
	}
}

// Snapshot marshals on the owner thread; the returned write only touches immutable bytes.
func Snapshot(path string, tr *Transcript) func() {
	b, err := json.Marshal(tr)
	if err != nil {
		return nil
	}
	seq := asked.Add(1)
	queued := &snapshot{data: b}
	pending.Store(path, queued)
	return func() {
		defer pending.CompareAndDelete(path, queued)
		mu.Lock()
		defer mu.Unlock()
		if seq <= landed[path] {
			return
		}
		if os.MkdirAll(filepath.Dir(path), 0755) != nil {
			return
		}
		tmp := path + ".tmp"
		if os.WriteFile(tmp, b, 0644) == nil && os.Rename(tmp, path) == nil {
			landed[path] = seq
		}
	}
}

// Start returns where to write down from for a listener at pos of
// an episode length long, of which spans are written down: pos, or the
// end of the span it is in, or once all from there on is written down,
// the first of what is not before it; and up to where: the start of the
// next span, or 0 for the end. It reports false once all of it is
// written down.
func Start(spans []Span, pos, length time.Duration) (from, until time.Duration, ok bool) {
	if from, until, ok = After(spans, pos, length); ok || pos == 0 {
		return from, until, ok
	}
	return After(spans, 0, length)
}

// After is Start for what is from pos on alone.
func After(spans []Span, pos, length time.Duration) (from, until time.Duration, ok bool) {
	from = pos
	if end, in := Covered(spans, pos); in {
		from = end
	}
	if length > 0 && from >= length-time.Second {
		return 0, 0, false
	}
	for _, s := range spans {
		if s[0] > from {
			return from, s[0], true
		}
	}
	return from, 0, true
}

// Covered returns the end of the span holding t, if one does.
func Covered(spans []Span, t time.Duration) (time.Duration, bool) {
	for _, s := range spans {
		if s[0] <= t && t < s[1] {
			return s[1], true
		}
	}
	return 0, false
}

// AddSpan adds s to spans, sorted, merging those it meets.
func AddSpan(spans []Span, s Span) []Span {
	if s[1] <= s[0] {
		return spans
	}
	out := make([]Span, 0, len(spans)+1)
	for _, o := range spans {
		if o[1] < s[0] || o[0] > s[1] {
			out = append(out, o)
			continue
		}
		s = Span{min(s[0], o[0]), max(s[1], o[1])}
	}
	out = append(out, s)
	slices.SortFunc(out, func(x, y Span) int { return int(x[0] - y[0]) })
	return out
}

// AddSegment adds s to segs, sorted, unless it is mostly over one there.
func AddSegment(segs []speech.Segment, s speech.Segment) []speech.Segment {
	i, _ := slices.BinarySearchFunc(segs, s.Start, func(x speech.Segment, t time.Duration) int { return int(x.Start - t) })
	for _, o := range segs[max(i-1, 0):min(i+1, len(segs))] {
		if overlap := min(o.End, s.End) - max(o.Start, s.Start); overlap > (s.End-s.Start)/2 {
			return segs
		}
	}
	return slices.Insert(segs, i, s)
}
