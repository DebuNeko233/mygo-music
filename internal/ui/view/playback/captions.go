/*
呈现当前播客字幕、转写进度与模型提示，连接字幕跳转操作。
*/

package playback

import (
	"time"

	"github.com/egoist/mygo/ui"

	"netease-music-mygo/internal/features/lyrics"
	"netease-music-mygo/internal/features/netease"
	"netease-music-mygo/internal/ui/view/components"
)

// CaptionsPane shows the episode playing's captions as lyrics show, in
// Now Playing: written down on the device as it plays. Without the model,
// it offers to download it.
func (a *View) CaptionsPane(c *ui.Context, tr *netease.Track, h float32) {
	pl := a.Player()
	cs := a.Captions
	if !a.ModelReady() {
		if a.Captions.Model.Checking {
			a.captionsNote(c, "正在检查模型…", "", nil)
			return
		}
		a.ModelCard(c)
		return
	}
	if !a.Config.Captions {
		a.captionsNote(c, "字幕已关闭", "开启字幕", func() {
			a.Config.Captions = true
			a.SaveSettings()
			a.SyncCaptions()
		})
		return
	}
	// Until the player says the episode changed, what the app has is the
	// last one's: none of it shows.
	var lines []lyrics.Line
	if cs.Program == tr.ProgramID {
		lines = cs.Lines
	}
	pos := pl.Position()
	if len(lines) == 0 {
		switch {
		case cs.Program != tr.ProgramID:
			a.captionsNote(c, "正在识别…", "", nil)
		case cs.Failed == cs.Program:
			a.captionsNote(c, "字幕生成失败", "", nil)
		case a.CaptionsWritten():
			a.captionsNote(c, "这期节目没有识别到语音", "", nil)
		default:
			a.captionsNote(c, "正在识别…", "", nil)
		}
		return
	}
	cur := lyrics.Index(lines, pos)
	if pl.Playing() && cur+1 < len(lines) {
		c.After(lines[cur+1].At - pos + 20*time.Millisecond)
	}
	// A line stays lit through a pause, but not where nothing is written
	// down yet: after a seek ahead, until the words there are.
	if cur >= 0 && pos >= lines[cur].End && a.CaptionsWriting(pos) {
		cur = -1
	}
	if a.Reading.Track != tr.Key() {
		a.Reading.Track, a.Reading.At = tr.Key(), -2
		a.LyricScroll, a.LyricGlide = ui.ScrollState{}, lyricGlide{}
	}
	scroll := ui.Scroll(c.Key("captions")).Grow(1).TrackScroll(&a.LyricScroll).Padding(0, 8, h*0.5, 0)
	a.timedLines(c, scroll, lines, cur, LinesStyle{Size: 24, Weight: 700, LineHeight: 1.4, lang: cs.Language}, func() {
		if cs.Work != nil {
			ui.Text(c, "正在识别…").FontSize(14).FontWeight(600).TextColor(White.Alpha(0.35)).Padding(16, 12)
		}
	})
}

// captionsNote says how the captions are, in the middle of the pane, with
// a button if there is something to do.
func (a *View) captionsNote(c *ui.Context, text, button string, act func()) {
	ui.Column(c).Grow(1).Center().Gap(18).Children(func() {
		ui.Text(c, text).FontSize(22).FontWeight(700).TextColor(White.Alpha(0.4))
		if button != "" && components.PillButton(c.Key("act"), nil, button, White.Alpha(0.14), White).Clicked() {
			act()
		}
	})
}
