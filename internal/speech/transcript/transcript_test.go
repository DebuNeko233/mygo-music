/*
验证转写起点、时间范围合并与字幕片段去重。
*/

package transcript

import (
	"netease-music-mygo/internal/speech"
	"slices"
	"testing"
	"time"
)

const sec = time.Second

// Transcribing starts where the listener is, or past what is written down
// there already, and stops where what was written down before begins.
// With all from there on written down, it goes back for what is not:
// an episode played to its end, then given the model, wrote down nothing
// and said it had no speech.
func TestCaptionStart(t *testing.T) {
	spans := []Span{{30 * sec, 60 * sec}, {300 * sec, 400 * sec}}
	for _, c := range []struct {
		pos, from, until time.Duration
		ok               bool
	}{
		{10 * sec, 10 * sec, 30 * sec, true},
		{40 * sec, 60 * sec, 300 * sec, true},
		{100 * sec, 100 * sec, 300 * sec, true},
		{350 * sec, 400 * sec, 0, true},
		{500 * sec, 500 * sec, 0, true},
		{599500 * time.Millisecond, 0, 30 * sec, true},
		{600 * sec, 0, 30 * sec, true},
	} {
		from, until, ok := Start(spans, c.pos, 600*sec)
		if from != c.from || until != c.until || ok != c.ok {
			t.Errorf("at %v: %v to %v, %v; want %v to %v, %v", c.pos, from, until, ok, c.from, c.until, c.ok)
		}
	}
	if from, until, ok := Start([]Span{{0, 60 * sec}}, 600*sec, 600*sec); from != 60*sec || until != 0 || !ok {
		t.Errorf("at the end with the start written down: %v to %v, %v", from, until, ok)
	}
	for _, pos := range []time.Duration{0, 300 * sec, 600 * sec} {
		if _, _, ok := Start([]Span{{0, 600 * sec}}, pos, 600*sec); ok {
			t.Errorf("at %v: an episode written down whole starts again", pos)
		}
	}
}

// Stretches written down merge where they meet, in order.
func TestAddSpan(t *testing.T) {
	spans := AddSpan(nil, Span{100 * sec, 200 * sec})
	spans = AddSpan(spans, Span{0, 50 * sec})
	spans = AddSpan(spans, Span{150 * sec, 250 * sec})
	if want := []Span{{0, 50 * sec}, {100 * sec, 250 * sec}}; !slices.Equal(spans, want) {
		t.Fatalf("%v, want %v", spans, want)
	}
	if spans = AddSpan(spans, Span{50 * sec, 100 * sec}); !slices.Equal(spans, []Span{{0, 250 * sec}}) {
		t.Errorf("%v: the gap filled should leave one Span", spans)
	}
}

// A transcription started again where one was stopped writes down again
// what that one did last: the transcript keeps it once.
func TestAddSegmentOnce(t *testing.T) {
	segs := AddSegment(nil, speech.Segment{Start: 10 * sec, End: 20 * sec, Text: "一"})
	segs = AddSegment(segs, speech.Segment{Start: 30 * sec, End: 40 * sec, Text: "三"})
	segs = AddSegment(segs, speech.Segment{Start: 21 * sec, End: 29 * sec, Text: "二"})
	segs = AddSegment(segs, speech.Segment{Start: 10500 * time.Millisecond, End: 20 * sec, Text: "一"})
	var got []string
	for _, s := range segs {
		got = append(got, s.Text)
	}
	if !slices.Equal(got, []string{"一", "二", "三"}) {
		t.Errorf("%q", got)
	}
}
