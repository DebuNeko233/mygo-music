package app

import (
	"fmt"
	"math"
	"strings"
	"testing"
	"time"

	"github.com/egoist/mygo/ui"

	"netease-music-mygo/internal/lyrics"
	"netease-music-mygo/internal/speech"
)

// A long translated song builds only nearby controls after a distant seek and resize.
func TestLongLyricsWindow(t *testing.T) {
	lines := make([]lyrics.Line, 600)
	for i := range lines {
		lines[i] = lyrics.Line{At: time.Second + time.Duration(i)*200*time.Millisecond, Text: fmt.Sprintf("第 %d 句", i), Translation: strings.Repeat("这句歌词翻译会根据窗口宽度换行", 3)}
	}
	a, tt := shortLyricsApp(lines, 480, 520)
	a.pl.Hold(lines[480].At)
	tt.Frame()
	tt.Advance(time.Second)
	check := func(height float32) {
		t.Helper()
		active, ok := tt.AccessByRole(ui.RoleButton, lines[480].Text)
		if !ok || active.Offscreen || math.Abs(float64(active.Bounds.Y-height*0.3)) > 1 {
			t.Fatalf("seek lost the reading position: %+v", active)
		}
		if _, ok := tt.Access(lines[0].Text); ok {
			t.Fatal("built a row outside the buffered viewport")
		}
	}
	check(520)
	tt.SetSize(300, 400)
	tt.Advance(time.Second)
	check(400)
	if err := tt.Click(lines[481].Text); err != nil || a.pl.Position() != lines[481].At {
		t.Fatalf("a wrapped visible row could not seek: %v, position %v", err, a.pl.Position())
	}
	last := lines[len(lines)-1]
	a.pl.Seek(last.At)
	tt.Frame()
	tt.Advance(time.Second)
	if got, ok := tt.AccessByRole(ui.RoleButton, last.Text); !ok || got.Offscreen || got.Bounds.Y > 400*0.3+1 {
		t.Fatalf("last line could not reach the reading position: %+v", got)
	}
	// Back before the first lyric: its translation must already have room
	// after reflow, even though there is no active line to measure.
	a.pl.Seek(0)
	tt.Frame()
	tt.Advance(time.Second)
	if got, ok := tt.AccessByRole(ui.RoleButton, lines[0].Text); !ok || got.Offscreen || a.lyricScroll.Y != 0 || got.Bounds.H < 80 {
		t.Fatalf("backward seek lost the first row: %+v", got)
	}
}

// A long transcript can seek to a distant caption and still click the nearby lines.
func TestLongCaptionsWindow(t *testing.T) {
	a := captionsTestApp(t, true)
	cs := &a.captions
	cs.program = 42
	cs.tr = &transcript{Spans: []span{{0, 600 * sec}}}
	for i := range 300 {
		cs.lines = append(cs.lines, lyrics.Line{At: time.Duration(i) * sec, Text: fmt.Sprintf("第 %d 句字幕", i)})
	}
	tt := ui.NewTester(func(c *ui.Context) { a.captionsPane(c, a.pl.Current(), 520) }, 440, 520)
	a.pl.Seek(250 * sec)
	tt.Frame()
	tt.Advance(time.Second)
	if got, ok := tt.Access("第 250 句字幕"); !ok || got.Offscreen {
		t.Fatalf("target caption not visible: %+v", got)
	}
	if _, ok := tt.Access("第 10 句字幕"); ok {
		t.Fatal("built a distant caption")
	}
	if err := tt.Click("第 251 句字幕"); err != nil || a.pl.Position() != 251*sec {
		t.Fatalf("caption could not seek: %v, position %v", err, a.pl.Position())
	}
}

// Resizing after manual scrolling rewraps the visible captions around the same row.
func TestCaptionResizeKeepsManualPosition(t *testing.T) {
	a := captionsTestApp(t, true)
	cs := &a.captions
	cs.program = 42
	cs.tr = &transcript{Spans: []span{{0, 600 * sec}}}
	for i := range 600 {
		cs.lines = append(cs.lines, lyrics.Line{
			At:   time.Duration(i) * sec,
			Text: fmt.Sprintf("第 %d 句字幕，缩窄窗口后应该可以继续阅读这段需要换行的内容", i),
		})
	}
	a.pl.Seek(250 * sec)
	tt := ui.NewTester(func(c *ui.Context) {
		_, h := c.Size()
		a.captionsPane(c, a.pl.Current(), h)
	}, 520, 460)
	tt.Scroll(200, 220, 0, 900)
	tt.Advance(time.Second)
	// Capture the row crossing the top of the pane, not a row tied to playback.
	var anchor ui.AccessInfo
	for _, row := range tt.AccessAll() {
		if row.Role == ui.RoleButton && !row.Offscreen && row.Bounds.Y <= 0 && row.Bounds.Y+row.Bounds.H > 0 {
			anchor = row
			break
		}
	}
	if anchor.Name == "" || anchor.Name == cs.lines[250].Text {
		t.Fatalf("no manually scrolled anchor: %+v", anchor)
	}
	for _, width := range []int{470, 420, 350, 285} {
		tt.SetSize(width, 460)
		tt.Advance(16 * time.Millisecond)
	}
	tt.Advance(time.Second)
	after, ok := tt.AccessByRole(ui.RoleButton, anchor.Name)
	if !ok || after.Offscreen || math.Abs(float64(after.Bounds.Y-anchor.Bounds.Y)) > 2 || after.Bounds.H <= anchor.Bounds.H {
		t.Fatalf("resize lost the anchor or failed to rewrap it: %+v -> %+v", anchor.Bounds, after.Bounds)
	}
}

// While reading ahead, new captions keep the scrolled row reachable, even as the list starts windowing.
func TestCaptionAppendKeepsManualPosition(t *testing.T) {
	for _, count := range []int{timedListThreshold - 1, 300} {
		t.Run(fmt.Sprint(count), func(t *testing.T) {
			a := captionsTestApp(t, true)
			work := &captionWork{program: 42}
			cs := &a.captions
			cs.program, cs.tr, cs.work = 42, &transcript{}, work
			add := func(i int) {
				at := time.Duration(i) * time.Second
				a.captionSaid(work, speech.Segment{Start: at, End: at + 500*time.Millisecond, Text: fmt.Sprintf("Caption %03d", i)})
			}
			for i := range count {
				add(i)
			}
			a.pl.Seek(20 * time.Second)
			tt := ui.NewTester(func(c *ui.Context) { a.captionsPane(c, a.pl.Current(), 460) }, 440, 460)
			tt.Advance(2 * time.Second)
			anchor := func() ui.Rect {
				t.Helper()
				row, ok := tt.AccessByRole(ui.RoleButton, "Caption 024")
				if !ok || row.Offscreen {
					t.Fatalf("lost the row being read: %+v", row)
				}
				return row.Bounds
			}
			before := anchor()
			tt.Scroll(200, 220, 0, 100)
			tt.Advance(time.Second)
			scrolled := anchor()
			if before.Y-scrolled.Y < 75 {
				t.Fatalf("fixture did not scroll: %.1f -> %.1f", before.Y, scrolled.Y)
			}
			add(count)
			tt.Frame()
			tt.Advance(time.Second)
			if after := anchor(); math.Abs(float64(after.Y-scrolled.Y)) > 2 {
				t.Fatalf("appending moved the row being read: %.1f -> %.1f", scrolled.Y, after.Y)
			}
			if err := tt.Click("Caption 024"); err != nil || a.pl.Position() != 24*time.Second {
				t.Fatalf("scrolled caption could not seek: %v, position %v", err, a.pl.Position())
			}
		})
	}
}

// Caption language follows new text and out-of-order segments while duplicate segments leave the list alone.
func TestCaptionLanguageAfterAppendAndInsert(t *testing.T) {
	a := newTestApp()
	work := &captionWork{program: 42}
	a.captions = captionState{program: 42, tr: &transcript{}, work: work}
	english := speech.Segment{Start: 5 * sec, End: 7 * sec, Text: "First caption"}
	japanese := speech.Segment{Start: 8 * sec, End: 10 * sec, Text: "次のことば"}
	a.captionSaid(work, english)
	if a.captions.language != "" || len(a.captions.lines) != 1 {
		t.Fatal("first caption did not keep the default language")
	}
	a.captionSaid(work, japanese)
	a.captionSaid(work, japanese)
	if got := a.captions; got.language != "ja" || len(got.lines) != 2 {
		t.Fatalf("append or duplicate corrupted transcript: %q, %v", got.language, got.lines)
	}
	a.captionSaid(work, speech.Segment{Start: sec, End: 3 * sec, Text: "Earlier"})
	if got := a.captions.lines; len(got) != 3 || got[0].Text != "Earlier" || a.captions.language != "ja" {
		t.Fatalf("out-of-order insert corrupted transcript: %v", got)
	}
}
