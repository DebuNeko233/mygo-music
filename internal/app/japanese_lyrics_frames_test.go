package app

import (
	"github.com/egoist/mygo/ui"
	"image/png"
	"netease-music-mygo/internal/lyrics"
	"netease-music-mygo/internal/netease"
	"os"
	"path/filepath"
	"testing"
	"time"
)

// With NM_FRAMES set, inspect Japanese glyphs beside Chinese translations in
// every lyrics surface, including the desktop's selected fonts and outline.
func TestJapaneseLyricsFrames(t *testing.T) {
	dir := os.Getenv("NM_FRAMES")
	if dir == "" {
		t.Skip()
	}
	isolateUserData(t)
	a := newTestApp()
	a.settings = defaultSettings()
	a.pl.Cue([]netease.Track{{ID: 1, Name: "日本語の歌", Duration: 120000}})
	lines := []lyrics.Line{
		{At: 0, End: 10 * time.Second, Text: "春日影", Translation: "春日的光影"},
		{At: 10 * time.Second, End: 20 * time.Second, Text: "憧れていた世界へ", Translation: "走向憧憬的世界"},
		{At: 20 * time.Second, End: 30 * time.Second, Text: "夢に見ていた日々", Translation: "梦中见过的日子"},
	}
	for i := range lines {
		lines[i].Words = lyrics.SpreadWords(lines[i].Text, lines[i].At, lines[i].End)
	}
	a.pl.SetLyrics(lines)
	a.pl.Hold(12 * time.Second)
	a.nowPlaying = true
	d := &deskLyrics{a: a}
	a.lyrics = d
	a.settings.LyricTrans = true
	a.settings.LyricOutlineWidth = 1.2
	for _, surface := range []string{"lyrics", "stage", "wall", "desktop"} {
		view := a.view
		switch surface {
		case "lyrics":
			a.npPane = paneLyrics
		case "stage":
			a.npPane = paneStage
		case "wall":
			a.npPane = paneWall
		case "desktop":
			view = d.view
		}
		w, h := 1100, 760
		if surface == "desktop" {
			w, h = 1000, 280
		}
		tt := ui.NewTester(view, w, h)
		tt.SetLocale("zh-CN")
		for range 3 {
			tt.Advance(time.Second)
			tt.Frame()
		}
		f, e := os.Create(filepath.Join(dir, "japanese-"+surface+".png"))
		if e != nil {
			t.Fatal(e)
		}
		e = png.Encode(f, tt.Image())
		f.Close()
		if e != nil {
			t.Fatal(e)
		}
	}
}
