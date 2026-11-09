package app

import (
	"fmt"
	"math"
	"strings"
	"testing"
	"time"

	"github.com/egoist/mygo/ui"

	"netease-music-mygo/internal/lyrics"
	"netease-music-mygo/internal/netease"
)

func shortLyricsApp(lines []lyrics.Line, w, h int) (*app, *ui.Tester) {
	a := newTestApp()
	a.settings = defaultSettings()
	a.settings.HotComments = false
	a.pl.Cue([]netease.Track{{ID: 1, Duration: 200000}})
	a.pl.SetLyrics(lines)
	a.pl.Hold(0)
	tt := ui.NewTesterWith(func(c *ui.Context) {
		_, height := c.Size()
		ui.Column(c).Fill().Children(func() { a.lyricsPane(c, a.pl.Current(), height) })
	}, w, h, ui.TesterOptions{Preferences: ui.Preferences{ReduceMotion: true}})
	return a, tt
}

func checkShortLyricsCenter(t *testing.T, tt *ui.Tester, a *app, lines []lyrics.Line, h int) {
	t.Helper()
	first, ok := tt.Find(lines[0].Text)
	if !ok {
		t.Fatal("first lyric missing")
	}
	last, ok := tt.Find(lines[len(lines)-1].Text)
	if !ok {
		t.Fatal("last lyric missing")
	}
	if center := (first.Y + last.Y + last.H) / 2; math.Abs(float64(center-float32(h)/2)) > 2 {
		t.Fatalf("lyrics centered at %.1f, want %.1f", center, float32(h)/2)
	}
	if a.lyricScroll.MaxY > 1 || a.lyricScroll.Y != 0 {
		t.Fatalf("short lyrics still scroll: %+v", a.lyricScroll)
	}
}

// A timed instrumental notice and short lyrics started at the top; center
// their whole measured block, including wrapped text and translations.
func TestShortLyricsCenterAndSeek(t *testing.T) {
	for _, lines := range [][]lyrics.Line{
		{{At: 10 * time.Second, Text: "纯音乐，请欣赏"}},
		{{Text: "第一句"}, {At: 10 * time.Second, Text: "第二句"}, {At: 20 * time.Second, Text: "第三句"}},
		{{Text: "春日影", Translation: strings.Repeat("这是一句会换行的翻译", 3)}, {At: 10 * time.Second, Text: "次の歌詞", Translation: "下一句歌词"}},
	} {
		t.Run(lines[0].Text, func(t *testing.T) {
			a, tt := shortLyricsApp(lines, 420, 600)
			settleTallLyrics(tt)
			checkShortLyricsCenter(t, tt, a, lines, 600)
			last := lines[len(lines)-1]
			if err := tt.Click(last.Text); err != nil {
				t.Fatal(err)
			}
			settleTallLyrics(tt)
			if a.pl.Position() != last.At {
				t.Fatalf("click sought to %v, want %v", a.pl.Position(), last.At)
			}
			checkShortLyricsCenter(t, tt, a, lines, 600)
		})
	}
}

// A small number of long translated lines may overflow after wrapping;
// resizing must switch between centering and following, without stale offsets.
func TestShortLyricsReflowAndReplacement(t *testing.T) {
	lines := make([]lyrics.Line, 4)
	for i := range lines {
		lines[i] = lyrics.Line{At: time.Duration(i) * 10 * time.Second, Text: fmt.Sprintf("%d %s", i, strings.Repeat("歌词需要按实际高度判断", 4)), Translation: strings.Repeat("翻译也占用高度", 3)}
	}
	a, tt := shortLyricsApp(lines, 800, 900)
	settleTallLyrics(tt)
	checkShortLyricsCenter(t, tt, a, lines, 900)
	tt.SetSize(260, 300)
	tt.Advance(2 * time.Second)
	if a.lyricScroll.MaxY <= 0 {
		t.Fatal("wrapped lyrics did not become scrollable")
	}
	a.pl.Hold(lines[len(lines)-1].At)
	settleTallLyrics(tt)
	last, ok := tt.Find(lines[len(lines)-1].Text)
	if !ok || last.Y < -1 || last.Y > 300*0.3+2 {
		t.Fatalf("last line not followed: %+v", last)
	}
	tt.SetSize(800, 900)
	tt.Advance(2 * time.Second)
	checkShortLyricsCenter(t, tt, a, lines, 900)
	// Metadata may replace lyrics while the track and current index stay the same.
	a.pl.Hold(0)
	a.pl.SetLyrics([]lyrics.Line{{Text: "替换后的单行歌词"}})
	tt.Frame()
	tt.Advance(2 * time.Second)
	replaced, _ := a.pl.Lyrics()
	checkShortLyricsCenter(t, tt, a, replaced, 900)
}
