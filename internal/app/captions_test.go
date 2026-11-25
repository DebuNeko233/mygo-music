/*
验证字幕异步保存、取消隔离、模型下载和点击跳转，不阻塞界面线程。
*/

package app

import (
	"context"
	"encoding/json"
	"os"
	"slices"
	"sync"
	"testing"
	"time"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"

	playbackview "netease-music-mygo/internal/desktop/view/playback"
	"netease-music-mygo/internal/netease"
	"netease-music-mygo/internal/speech"
	captionsvm "netease-music-mygo/internal/viewmodel/captions"
)

const sec = time.Second

func isolateTranscripts(t *testing.T) {
	t.Helper()
	oldCache, err := mygo.App.Path(mygo.PathCache)
	if err != nil {
		t.Fatal(err)
	}
	mygo.App.SetPath(mygo.PathCache, t.TempDir())
	t.Cleanup(func() { mygo.App.SetPath(mygo.PathCache, oldCache) })
}

// A late save still writes its own episode and cannot overwrite newer captions for that episode.
func TestTranscriptSavesOutOfOrder(t *testing.T) {
	isolateTranscripts(t)
	a := &app{}
	snapshot := func(program int64, end time.Duration) func() {
		a.captions.Program = program
		a.captions.Transcript = &transcript{Model: speech.ModelName, Spans: []span{{0, end}}}
		write := a.transcriptWrite()
		if write == nil {
			t.Fatal("no transcript write")
		}
		return write
	}
	old := snapshot(1, sec)
	first := snapshot(1, 2*sec)
	second := snapshot(2, 3*sec)
	second()
	first()
	old()

	for program, end := range map[int64]time.Duration{1: 2 * sec, 2: 3 * sec} {
		path, err := transcriptPath(program)
		if err != nil {
			t.Fatal(err)
		}
		b, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		var got transcript
		if err := json.Unmarshal(b, &got); err != nil {
			t.Fatal(err)
		}
		if !slices.Equal(got.Spans, []span{{0, end}}) {
			t.Errorf("episode %d saved %v, want coverage through %v", program, got.Spans, end)
		}
	}
}

// Returning to an episode before its save lands loads the latest queued captions.
func TestTranscriptReloadWhileSavePending(t *testing.T) {
	isolateTranscripts(t)
	a := newTestApp()
	posts := testPosts(a)
	a.pl.Cue([]netease.Track{{ID: 5, ProgramID: 42}})
	a.captions.Program = 42
	latest := &transcript{Model: speech.ModelName, Spans: []span{{0, sec}}}
	a.captions.Transcript = latest
	write := a.transcriptWrite()
	if write == nil {
		t.Fatal("no transcript write")
	}
	defer write()

	a.captions.Transcript = nil
	a.captions.Load(42, a.captionServices())
	refreshPost(t, posts)
	if got := a.captions.Transcript; got == nil || !slices.Equal(got.Spans, latest.Spans) {
		t.Fatalf("loaded %+v, want pending coverage %v", got, latest.Spans)
	}
}

// A cancelled transcription's queued callbacks must not write into its
// replacement for the same episode.
func TestCancelledCaptionWorkIsIgnored(t *testing.T) {
	old := &captionsvm.CaptionWork{Program: 42}
	current := &captionsvm.CaptionWork{Program: 42, From: 10 * sec}
	a := newTestApp()
	a.captions.Program = 42
	a.captions.Transcript = &transcript{}
	a.captions.Work = current
	a.captions.Said(old, speech.Segment{Start: 0, End: sec, Text: "旧"})
	a.captions.Heard(old, sec, a.captionServices())
	if len(a.captions.Transcript.Segments) != 0 || len(a.captions.Transcript.Spans) != 0 {
		t.Fatalf("cancelled work changed the transcript: %+v", a.captions.Transcript)
	}
	a.captions.Said(current, speech.Segment{Start: 10 * sec, End: 11 * sec, Text: "新"})
	a.captions.Heard(current, 11*sec, a.captionServices())
	if len(a.captions.Transcript.Segments) != 1 || len(a.captions.Transcript.Spans) != 1 {
		t.Fatalf("current work did not write: %+v", a.captions.Transcript)
	}
}

// captionsTestApp plays an episode in Now Playing, with the libraries in
// the app and the model installed or not.
func captionsTestApp(t *testing.T, installed bool) *app {
	isolateUserData(t)
	oldAvail, oldInstalled := captionsAvailable, modelInstalled
	t.Cleanup(func() { captionsAvailable, modelInstalled = oldAvail, oldInstalled })
	captionsAvailable = func() bool { return true }
	modelInstalled = func(string) bool { return installed }
	a := newTestApp()
	// The look for the model runs on a goroutine of its own; these tests
	// have its answer already.
	a.captions.Model.Checked, a.captions.Model.Installed = true, installed
	a.settings = defaultSettings()
	a.pl.Cue([]netease.Track{{ID: 7, Name: "510 泰国侨批沉浮录", ProgramID: 42, Duration: 600000, Description: "下南洋"}})
	a.playbackState.Open = true
	return a
}

// Without the model, the episode's captions offer to download it, and
// 下载模型 starts the download.
func TestDownloadModelFromCaptions(t *testing.T) {
	a := captionsTestApp(t, false)
	started := make(chan struct{}, 1)
	old := downloadModel
	defer func() { downloadModel = old }()
	downloadModel = func(ctx context.Context, dir string, progress func(int64, int64)) error {
		started <- struct{}{}
		<-ctx.Done()
		return ctx.Err()
	}
	tt := ui.NewTester(a.view, 1200, 800)
	tt.Frame()
	tt.Frame()
	if a.playbackState.Pane != playbackview.PaneProgram {
		t.Fatalf("pane %d: an episode without captions opens on 简介", a.playbackState.Pane)
	}
	if err := tt.Click("字幕"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	if err := tt.Click("下载模型"); err != nil {
		t.Fatal(err)
	}
	select {
	case <-started:
	case <-time.After(2 * time.Second):
		t.Fatal("the download did not start")
	}
	tt.Frame()
	if !a.captions.Model.Downloading {
		t.Fatal("not downloading")
	}
	if err := tt.Click("取消"); err != nil {
		t.Fatal(err)
	}
}

// With the model, the episode opens on its captions, and a click on a
// line seeks to it, as a line of lyrics does.
func TestCaptionClickSeeks(t *testing.T) {
	a := captionsTestApp(t, true)
	segs := []speech.Segment{
		{Start: 12 * sec, End: 16 * sec, Text: "有些话题是绕不开的。"},
		{Start: 17 * sec, End: 20 * sec, Text: "比如说，我的婚姻结束了。"},
	}
	a.captions.Program = 42
	a.captions.Transcript = &transcript{Model: speech.ModelName, Segments: segs, Spans: []span{{0, 20 * sec}}}
	a.captions.Lines = speech.Lines(segs)
	tt := ui.NewTester(a.view, 1200, 800)
	tt.Frame()
	tt.Frame()
	if a.playbackState.Pane != playbackview.PaneCaptions {
		t.Fatalf("pane %d: an episode with captions opens on 字幕", a.playbackState.Pane)
	}
	if err := tt.Click("比如说，我的婚姻结束了"); err != nil {
		t.Fatal(err)
	}
	if got := a.pl.Position(); got != 17*sec {
		t.Errorf("at %v, want 17s", got)
	}
}

// Until an episode's transcription starts, its captions say they are on
// their way. Its transcript loaded before its audio opened, they said
// there was no speech, until the transcription started; and before the
// player told the app the episode changed, they showed the last one's, or
// after a song, that they had failed.
func TestCaptionsOnTheirWay(t *testing.T) {
	a := captionsTestApp(t, true)
	tt := ui.NewTester(a.view, 1200, 800)
	for _, c := range []struct {
		name  string
		state captionsvm.Captions
	}{
		{"after a song", captionsvm.Captions{}},
		{"after another episode", captionsvm.Captions{Program: 41, Transcript: &transcript{Spans: []span{{0, 600 * sec}}}, Lines: speech.Lines([]speech.Segment{{Start: 0, End: sec, Text: "上一期"}})}},
		{"loaded, the audio not yet", captionsvm.Captions{Program: 42, Transcript: &transcript{Model: speech.ModelName}}},
	} {
		a.captions = c.state
		a.captions.Model = captionsvm.CaptionModel{Checked: true, Installed: true}
		tt.Frame()
		tt.Frame()
		if !tt.HasText("正在识别…") || tt.HasText("上一期") || tt.HasText("这期节目没有识别到语音") || tt.HasText("字幕生成失败") {
			t.Errorf("%s: %q", c.name, tt.Texts())
		}
	}
	// Written down whole, with nothing said, there is no speech.
	a.captions = captionsvm.Captions{Program: 42, Transcript: &transcript{Spans: []span{{0, 600 * sec}}}, Model: captionsvm.CaptionModel{Checked: true, Installed: true}}
	tt.Frame()
	if !tt.HasText("这期节目没有识别到语音") {
		t.Errorf("written down whole: %q", tt.Texts())
	}
}

// Looking for the model reads a gigabyte of it, so it must not happen on the
// thread that draws: the answer arrives in a frame of its own.
func TestModelCheckDoesNotHoldTheDrawingThread(t *testing.T) {
	isolateUserData(t)
	oldAvail, oldInstalled := captionsAvailable, modelInstalled
	t.Cleanup(func() { captionsAvailable, modelInstalled = oldAvail, oldInstalled })
	captionsAvailable = func() bool { return true }
	release := make(chan struct{})
	letGo := sync.OnceFunc(func() { close(release) })
	t.Cleanup(letGo)
	looking := make(chan struct{})
	var once sync.Once
	modelInstalled = func(string) bool {
		once.Do(func() { close(looking) })
		<-release
		return true
	}
	a := newTestApp()
	posts := testPosts(a)
	ready := make(chan bool, 1)
	go func() { ready <- a.modelReady() }()
	select {
	case got := <-ready:
		if got {
			t.Fatal("the model was reported installed before it was looked at")
		}
	case <-time.After(2 * time.Second):
		t.Fatal("looking for the model held the thread that draws")
	}
	select {
	case <-looking:
	case <-time.After(2 * time.Second):
		t.Fatal("the model was never looked at")
	}
	letGo()
	refreshPost(t, posts)
	if !a.modelReady() {
		t.Fatal("the answer never came back")
	}
}
