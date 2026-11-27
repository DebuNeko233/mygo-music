/*
验证纯净歌词派生数据随歌词、曲目和本地资料替换，切换展示不影响播放。
*/

package player

import (
	"reflect"
	"testing"
	"time"

	"netease-music-mygo/internal/features/netease"
)

// A presentation switch borrows stable prepared data, preserves the full source
// and playhead, and refreshes immediately when replacement lyrics arrive.
func TestPureLyricPresentationLifecycle(t *testing.T) {
	p := New(func(func()) {})
	defer p.Close()
	Cue(p, []netease.Track{{ID: 7, Name: "夜航", Artists: []netease.Artist{{Name: "Alice"}}}})
	RefreshLyrics(p, 7, "[00:00]夜航 - Alice\n[00:01]作词：甲\n[00:20]唱出明天", "[00:20]A new tomorrow", "")
	Hold(p, 21*time.Second)
	original, loaded := p.LyricsDisplay()
	before := append([]netease.Track(nil), p.Queue()...)
	seek, position := p.SeekRevision(), p.Position()
	full, pure := p.PresentLyrics(false), p.PresentLyrics(true)
	if !loaded || !pure.Loaded || len(full.Display.Lines) != 3 || len(pure.Lines) != 1 || pure.Lines[0].Text != "唱出明天" || pure.Lines[0].Translation != "A new tomorrow" {
		t.Fatal("wrong prepared lyric variants")
	}
	for range 10 {
		p.PresentLyrics(false)
		if next := p.PresentLyrics(true); &next.Lines[0] != &pure.Lines[0] {
			t.Fatal("reading a presentation rebuilt its rows")
		}
	}
	if p.SeekRevision() != seek || p.Position() != position || !reflect.DeepEqual(before, p.Queue()) || !reflect.DeepEqual(original, p.PresentLyrics(false).Display) {
		t.Fatal("choosing a lyric presentation affected playback or its source")
	}
	var selected LyricPresentation
	if n := testing.AllocsPerRun(100, func() { selected = p.PresentLyrics(true) }); n != 0 {
		t.Fatalf("per-frame lyric selection allocated: %v", n)
	}
	if &selected.Lines[0] != &pure.Lines[0] {
		t.Fatal("allocation check did not read the prepared presentation")
	}
	RefreshLyrics(p, 7, "[00:00]制作人：甲\n[00:30]新的正文", "", "")
	if next := p.PresentLyrics(true); len(next.Lines) != 1 || next.Lines[0].Text != "新的正文" || &next.Lines[0] == &pure.Lines[0] {
		t.Fatal("a refreshed source retained the old pure projection")
	}
}

// Changing current local metadata invalidates title evidence without changing
// the lyric source or applying one song's pure result to the next song.
func TestPureLyricPresentationLocalMetadata(t *testing.T) {
	p := New(func(func()) {})
	defer p.Close()
	Cue(p, []netease.Track{{LocalID: 1, Name: "旧歌名", Artists: []netease.Artist{{Name: "Alice"}}}})
	RefreshLyrics(p, 0, "[00:00]新歌名 - Alice\n[00:20]唱出明天", "", "")
	if len(p.PresentLyrics(true).Lines) != 2 {
		t.Fatal("an unconfirmed title was removed")
	}
	p.UpdateLocalTracks([]netease.Track{{LocalID: 1, Name: "新歌名", Artists: []netease.Artist{{Name: "Alice"}}}})
	if len(p.PresentLyrics(true).Lines) != 1 {
		t.Fatal("local metadata change did not refresh the title evidence")
	}
	Cue(p, []netease.Track{{ID: 8, Name: "另一首"}})
	RefreshLyrics(p, 8, "[00:05]另一首的正文", "", "")
	if next := p.PresentLyrics(true); len(next.Lines) != 1 || next.Lines[0].Text != "另一首的正文" {
		t.Fatal("the previous song's filtered rows survived source replacement")
	}
}

// Selecting another song clears both presentations before its asynchronous
// lyrics can arrive, including when loading is deferred by a cache move.
func TestPureLyricPresentationClearedOnSelection(t *testing.T) {
	p := New(func(func()) {})
	defer p.Close()
	p.cacheChanging = true
	p.PlayQueue([]netease.Track{{ID: 7}, {ID: 8}}, 0)
	RefreshLyrics(p, 7, "[00:00]作词：甲\n[00:20]男：唱出明天", "", "")
	if len(p.PresentLyrics(true).Lines) != 1 {
		t.Fatal("missing initial projection")
	}
	p.Next()
	for _, pure := range []bool{false, true} {
		content := p.PresentLyrics(pure)
		if p.Current().ID != 8 || content.Loaded || len(content.Lines) != 0 || len(content.Display.Lines) != 0 {
			t.Fatal("previous lyrics survived the next selection")
		}
	}
	RefreshLyrics(p, 7, "[00:20]旧请求", "", "")
	if p.PresentLyrics(true).Loaded {
		t.Fatal("an old song response restored the previous presentation")
	}
}
