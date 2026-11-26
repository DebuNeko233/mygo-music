/*
验证长歌词和字幕仅构建附近行，并在模式切换、缩放和追加时保留阅读位置。
*/

package playback

import (
	"fmt"
	"math"
	"strings"
	"testing"
	"time"

	"github.com/egoist/mygo/ui"

	"netease-music-mygo/internal/features/lyrics"
	"netease-music-mygo/internal/features/player"
	"netease-music-mygo/internal/features/speech"
	captionstore "netease-music-mygo/internal/features/speech/transcript"
	captionsvm "netease-music-mygo/internal/ui/viewmodel/captions"
)

// A long translated song builds only nearby controls after a distant seek and resize.
func TestLongLyricsWindow(t *testing.T) {
	lines := make([]lyrics.Line, 600)
	for i := range lines {
		lines[i] = lyrics.Line{At: time.Second + time.Duration(i)*200*time.Millisecond, Text: fmt.Sprintf("第 %d 句", i), Translation: strings.Repeat("这句歌词翻译会根据窗口宽度换行", 3)}
	}
	a, tt := lyricPaneView(t, lines, 480, 520)
	player.Hold(a.Player(), lines[480].At)
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
	if err := tt.Click(lines[481].Text); err != nil || a.Player().Position() != lines[481].At {
		t.Fatalf("a wrapped visible row could not seek: %v, position %v", err, a.Player().Position())
	}
	last := lines[len(lines)-1]
	a.Player().Seek(last.At)
	tt.Frame()
	tt.Advance(time.Second)
	if got, ok := tt.AccessByRole(ui.RoleButton, last.Text); !ok || got.Offscreen || got.Bounds.Y > 400*0.3+1 {
		t.Fatalf("last line could not reach the reading position: %+v", got)
	}
	// Back before the first lyric: its translation must already have room
	// after reflow, even though there is no active line to measure.
	a.Player().Seek(0)
	tt.Frame()
	tt.Advance(time.Second)
	if got, ok := tt.AccessByRole(ui.RoleButton, lines[0].Text); !ok || got.Offscreen || a.LyricScroll.Y != 0 || got.Bounds.H < 80 {
		t.Fatalf("backward seek lost the first row: %+v", got)
	}
}

// Toggling immersive mode preserves the reading position in a virtualized lyric list.
func TestLongLyricsKeepAnchorWhenTogglingImmersive(t *testing.T) {
	lines := make([]lyrics.Line, 100)
	for i := range lines {
		lines[i] = lyrics.Line{At: time.Duration(i) * time.Second, Text: fmt.Sprintf("Line %d", i)}
	}
	const height = 240
	a, tt := lyricPaneView(t, lines, 200, height)
	active := lines[len(lines)/2]
	player.Hold(a.Player(), active.At)
	for _, immersive := range []bool{false, true, false} {
		a.Config.LyricEdgeFade = immersive
		tt.Frame()
		tt.Advance(time.Second)
		row, ok := tt.AccessByRole(ui.RoleButton, active.Text)
		if !ok || row.Offscreen || Abs32(row.Bounds.Y-height*0.3) > 2 {
			t.Fatalf("immersive=%v: active lyric lost its reading position: %+v", immersive, row)
		}
	}
}

// Virtualized lyric rows size only the word text they can display, while captions still size their word highlight.
func TestTimedListMeasuresOnlyRenderedWordText(t *testing.T) {
	const width = 120
	estimated := lyrics.Line{
		Text:  "Short line",
		Words: []lyrics.Word{{Text: strings.Repeat("Word text wraps. ", 6)}},
	}
	for _, tc := range []struct {
		name                                    string
		lyric, immersive, timed, wantWordHeight bool
	}{
		{"ordinary LRC", true, false, false, false},
		{"immersive LRC", true, true, false, false},
		{"ordinary YRC", true, false, true, true},
		{"immersive YRC", true, true, true, true},
		{"podcast captions", false, false, false, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			line := estimated
			line.Timed = tc.timed
			rows := []lyrics.Line{line}
			ui.NewTester(func(c *ui.Context) {
				st := LinesStyle{Size: 22, Weight: 600, LineHeight: 1.25, CenterShort: tc.lyric, Immersive: tc.immersive}
				var layout TimedListLayout
				layout.Reset(c, rows, width, st, 0)
				layout.Measure(rows, 0, len(rows), st)
				text := line.Text
				if tc.wantWordHeight {
					text = line.Words[0].Text
				}
				_, height := ShapeLyric(text, LyricFont(c, st), width, c.Direction()).Size()
				want := height + 2*TimedLineInsetY + TimedLineGap
				if got := layout.Offsets[1]; Abs32(got-want) > 0.5 {
					t.Fatalf("row height = %.1f, want %.1f for the displayed text", got, want)
				}
			}, 200, 220)
		})
	}
}

// A long transcript can seek to a distant caption and still click the nearby lines.
func TestLongCaptionsWindow(t *testing.T) {
	a := captionView(t)
	cs := a.Captions
	cs.Program = 42
	cs.Transcript = &captionstore.Transcript{Spans: []captionstore.Span{{0, 600 * time.Second}}}
	for i := range 300 {
		cs.Lines = append(cs.Lines, lyrics.Line{At: time.Duration(i) * time.Second, Text: fmt.Sprintf("第 %d 句字幕", i)})
	}
	tt := ui.NewTester(func(c *ui.Context) { a.CaptionsPane(c, a.Player().Current(), 520) }, 440, 520)
	a.Player().Seek(250 * time.Second)
	tt.Frame()
	tt.Advance(time.Second)
	if got, ok := tt.Access("第 250 句字幕"); !ok || got.Offscreen {
		t.Fatalf("target caption not visible: %+v", got)
	}
	if _, ok := tt.Access("第 10 句字幕"); ok {
		t.Fatal("built a distant caption")
	}
	if err := tt.Click("第 251 句字幕"); err != nil || a.Player().Position() != 251*time.Second {
		t.Fatalf("caption could not seek: %v, position %v", err, a.Player().Position())
	}
}

// Resizing after manual scrolling rewraps the visible captions around the same row.
func TestCaptionResizeKeepsManualPosition(t *testing.T) {
	a := captionView(t)
	cs := a.Captions
	cs.Program = 42
	cs.Transcript = &captionstore.Transcript{Spans: []captionstore.Span{{0, 600 * time.Second}}}
	for i := range 600 {
		cs.Lines = append(cs.Lines, lyrics.Line{
			At:   time.Duration(i) * time.Second,
			Text: fmt.Sprintf("第 %d 句字幕，缩窄窗口后应该可以继续阅读这段需要换行的内容", i),
		})
	}
	a.Player().Seek(250 * time.Second)
	tt := ui.NewTester(func(c *ui.Context) {
		_, h := c.Size()
		a.CaptionsPane(c, a.Player().Current(), h)
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
	if anchor.Name == "" || anchor.Name == cs.Lines[250].Text {
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
	for _, count := range []int{TimedListThreshold - 1, 300} {
		t.Run(fmt.Sprint(count), func(t *testing.T) {
			a := captionView(t)
			work := &captionsvm.CaptionWork{Program: 42}
			cs := a.Captions
			cs.Program, cs.Transcript, cs.Work = 42, &captionstore.Transcript{}, work
			add := func(i int) {
				at := time.Duration(i) * time.Second
				a.Captions.Said(work, speech.Segment{Start: at, End: at + 500*time.Millisecond, Text: fmt.Sprintf("Caption %03d", i)})
			}
			for i := range count {
				add(i)
			}
			a.Player().Seek(20 * time.Second)
			tt := ui.NewTester(func(c *ui.Context) { a.CaptionsPane(c, a.Player().Current(), 460) }, 440, 460)
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
			if err := tt.Click("Caption 024"); err != nil || a.Player().Position() != 24*time.Second {
				t.Fatalf("scrolled caption could not seek: %v, position %v", err, a.Player().Position())
			}
		})
	}
}
