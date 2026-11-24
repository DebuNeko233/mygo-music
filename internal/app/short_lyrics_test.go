package app

import (
	"fmt"
	"math"
	"strings"
	"testing"
	"time"

	"github.com/egoist/mygo/ui"

	playbackview "netease-music-mygo/internal/desktop/view/playback"
	"netease-music-mygo/internal/lyrics"
	"netease-music-mygo/internal/netease"
)

func shortLyricsApp(lines []lyrics.Line, w, h int) (*app, *ui.Tester) {
	a := newTestApp()
	a.settings = defaultSettings()
	a.settings.LyricEdgeFade = false
	a.settings.HotComments = false
	a.pl.Cue([]netease.Track{{ID: 1, Duration: 200000}})
	a.pl.SetLyrics(lines)
	a.pl.Hold(0)
	tt := ui.NewTesterWith(func(c *ui.Context) {
		_, height := c.Size()
		ui.Column(c).Fill().Children(func() { a.views.playback.LyricsPane(c, a.pl.Current(), height, ui.Rect{}, 0) })
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
	// Find returns painted bounds. Undo the inactive rows' emphasis scale
	// to check the layout, including when font metrics make a row taller.
	top, bottom := first.Y, last.Y+last.H
	const unscale = (1/playbackview.InactiveTimedLyricScale - 1) / 2
	if a.lyricReading.At != 0 {
		top -= first.H * unscale
	}
	if a.lyricReading.At != len(lines)-1 {
		bottom += last.H * unscale
	}
	if center := (top + bottom) / 2; math.Abs(float64(center-float32(h)/2)) > 2 {
		t.Fatalf("lyrics centered at %.1f, want %.1f", center, float32(h)/2)
	}
	if a.LyricScroll.MaxY > 1 || a.LyricScroll.Y != 0 {
		t.Fatalf("short lyrics still scroll: %+v", a.LyricScroll)
	}
}

// A timed instrumental notice and short lyrics started at the top; center
// their whole measured block, including wrapped text and translations.
func TestShortLyricsCenterAndSeek(t *testing.T) {
	for _, lines := range [][]lyrics.Line{
		{{At: 10 * time.Second, Text: "A one-line song"}},
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
	if a.LyricScroll.MaxY <= 0 {
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

// Empty lyric responses and explicit missing-lyric notices show different messages in both modes.
func TestNowPlayingEmptyLyricNotice(t *testing.T) {
	for _, immersive := range []bool{false, true} {
		t.Run(fmt.Sprint(immersive), func(t *testing.T) {
			a, tt := shortLyricsApp(nil, 320, 240)
			a.settings.LyricEdgeFade = immersive
			for _, tc := range []struct{ source, want, other string }{
				{"", lyrics.InstrumentalText, lyrics.UnavailableText},
				{"[00:00.00]暂无歌词", lyrics.UnavailableText, lyrics.InstrumentalText},
				{"暂无歌词", lyrics.UnavailableText, lyrics.InstrumentalText},
				{"暂无歌词\n[00:01]作曲：A", lyrics.UnavailableText, lyrics.InstrumentalText},
				{"", lyrics.InstrumentalText, lyrics.UnavailableText},
			} {
				a.pl.RefreshLyrics(1, tc.source, "", "")
				tt.Frame()
				if !tt.HasText(tc.want) || tt.HasText(tc.other) {
					t.Fatalf("lyrics source %q: expected %q alone, got %q", tc.source, tc.want, tt.Texts())
				}
			}
		})
	}
}

// Static metadata centers or scrolls manually in either lyric mode, regardless of its timestamps.
func TestNowPlayingStaticLyricMetadata(t *testing.T) {
	for _, immersive := range []bool{false, true} {
		t.Run(fmt.Sprint(immersive), func(t *testing.T) {
			const w, h = 480, 360
			a, tt := shortLyricsApp(nil, w, h)
			a.settings.LyricEdgeFade = immersive
			a.pl.RefreshLyrics(1, "[00:00.00]作曲 : A\n[00:05.00]纯音乐，请欣赏\n[01:47.50]指挥 Conductor : C", "", "")
			tt.Frame()
			first, ok := tt.Find(lyrics.InstrumentalText)
			last, lastOK := tt.Find("指挥 Conductor : C")
			if !ok || !lastOK || first.Y >= last.Y || playbackview.Abs32((first.Y+last.Y+last.H)/2-h/2) > 3 {
				t.Fatalf("static credits did not center together: %v, %v", first, last)
			}
			if _, ok := tt.AccessByRole(ui.RoleButton, "作曲 : A"); ok {
				t.Fatal("credit is a seek target")
			}
			tt.SetSize(w, 96)
			before, _ := tt.Find("指挥 Conductor : C")
			tt.Scroll(160, 70, 0, 40)
			scrolled, _ := tt.Find("指挥 Conductor : C")
			if scrolled.Y >= before.Y {
				t.Fatalf("credits did not scroll: %v -> %v", before, scrolled)
			}
			a.pl.Hold(110 * time.Second)
			tt.Frame()
			held, _ := tt.Find("指挥 Conductor : C")
			if playbackview.Abs32(held.Y-scrolled.Y) > 1 {
				t.Fatalf("static block followed playback: %v -> %v", scrolled, held)
			}
		})
	}
}

// Long multilingual credits wrap in a narrow pane and the last entry remains reachable.
func TestLongMultilingualCreditsCanScrollToEnd(t *testing.T) {
	const w, h = 230, 164
	const last = "マスタリング：最後の担当者"
	a, tt := shortLyricsApp(nil, w, h)
	long := "Lead Guitar: " + strings.Repeat("Long Performer Name / ", 6)
	a.pl.RefreshLyrics(1, "[00:00.00]和声编写：A\n[00:01.00]"+long+
		"\n[00:02.00]作曲／編曲：C\n[01:30.00]"+last, "", "")
	tt.Frame()
	if title, ok := tt.Find(lyrics.InstrumentalText); !ok || title.Y < 16 || title.Y+title.H >= h {
		t.Fatalf("overflowing credits must start with the notice in view: %+v", title)
	}
	if a.LyricScroll.MaxY < 90 || a.LyricScroll.MaxX > 0 {
		t.Fatalf("long credit did not wrap vertically: %+v", a.LyricScroll)
	}
	tt.Scroll(w/2, h/2, 0, 1500)
	tt.Advance(time.Second)
	end, ok := tt.Find(last)
	if !ok || end.Y < 0 || end.Y+end.H > h+1 {
		t.Fatalf("last credit was clipped at the end of the scroll: %+v", end)
	}
	a.pl.Cue([]netease.Track{{ID: 2}})
	a.pl.SetLyrics(nil)
	tt.Frame()
	title, ok := tt.Find(lyrics.InstrumentalText)
	if !ok || a.LyricScroll.Y != 0 || playbackview.Abs32(title.Y+title.H/2-h/2) > 2 {
		t.Fatalf("next track inherited the scroll position: %+v, offset %.1f", title, a.LyricScroll.Y)
	}
}

// Switching tracks must never paint a short lyric block at the top while measuring its new viewport.
func TestShortLyricsDoNotFlashAtTop(t *testing.T) {
	for _, tc := range []struct {
		name, first, last string
		lines             []lyrics.Line
	}{
		{"empty", lyrics.InstrumentalText, lyrics.InstrumentalText, nil},
		{"timed instrumental notice", lyrics.InstrumentalText, lyrics.InstrumentalText, []lyrics.Line{{At: 5 * time.Second, Text: lyrics.InstrumentalText}}},
		{"credits only", lyrics.InstrumentalText, "作曲：A", []lyrics.Line{{At: time.Second, Text: "作曲：A"}}},
		{"sung line", "A short sung line", "A short sung line", []lyrics.Line{{Text: "A short sung line"}}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			for _, immersive := range []bool{false, true} {
				t.Run(fmt.Sprint(immersive), func(t *testing.T) {
					checkLyricSwitchPaintsCentered(t, tc.lines, tc.first, tc.last, immersive)
				})
			}
		})
	}
}

func checkLyricSwitchPaintsCentered(t *testing.T, lines []lyrics.Line, first, last string, immersive bool) {
	t.Helper()
	const w, h = 300, 360
	a := newTestApp()
	a.settings.LyricEdgeFade = immersive
	a.pl.Cue([]netease.Track{{ID: 1}})
	a.pl.SetLyrics(nil)
	var tt *ui.Tester
	checkTop := func() {
		t.Helper()
		frame := tt.Image()
		for y := 0; y < h/4; y++ {
			for x := 0; x < w; x++ {
				if frame.RGBAAt(x, y).R != 0 {
					t.Fatal("the lyric painted at the top during the track switch")
				}
			}
		}
	}
	tt = ui.NewTester(func(c *ui.Context) {
		// Inspect the previous paint before the tester settles the next build.
		if tt != nil {
			checkTop()
		}
		c.Root().Background(ui.RGB(0, 0, 0))
		ui.Column(c).Fill().Children(func() { a.views.playback.LyricsPane(c, a.pl.Current(), h, ui.Rect{}, 0) })
	}, w, h)

	a.pl.Cue([]netease.Track{{ID: 2}})
	a.pl.SetLyrics(lines)
	tt.Frame()
	checkTop()
	top, topOK := tt.Find(first)
	bottom, bottomOK := tt.Find(last)
	if !topOK || !bottomOK || playbackview.Abs32((top.Y+bottom.Y+bottom.H)/2-h/2) > 2 {
		t.Fatalf("the lyric block did not appear centered: %+v, %+v", top, bottom)
	}
}

// Revalidating the current track resets a manual reading position from the previous lyrics.
func TestImmersiveLyricsFollowAfterRefresh(t *testing.T) {
	lines := make([]lyrics.Line, 16)
	for i := range lines {
		lines[i] = lyrics.Line{At: time.Duration(i) * time.Second, Text: fmt.Sprintf("Before %02d", i)}
	}
	a, tt := shortLyricsApp(lines, 240, 180)
	a.settings.LyricEdgeFade = true
	a.pl.Hold(8 * time.Second)
	tt.Frame()
	tt.Scroll(160, 100, 0, 240)
	if a.lyricReading.BrowseUntil.IsZero() {
		t.Fatal("manual scrolling did not enter browsing mode")
	}
	for i := range lines {
		lines[i].Text = fmt.Sprintf("After %02d", i)
	}
	a.pl.SetLyrics(lines)
	tt.Frame()
	row, ok := tt.AccessByRole(ui.RoleButton, lines[8].Text)
	if !ok || row.Offscreen || !a.lyricReading.BrowseUntil.IsZero() || a.lyricReading.At != 8 {
		t.Fatalf("refreshed lyrics did not follow the current line: row=%+v, index=%d", row, a.lyricReading.At)
	}
}
