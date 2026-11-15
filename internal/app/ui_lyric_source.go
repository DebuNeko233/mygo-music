package app

import (
	"github.com/egoist/mygo/ui"

	"netease-music-mygo/internal/lyrics"
)

func lyricChoice(c *ui.Context, label, current string, options []lyricOption) (string, bool) {
	names := make([]string, len(options))
	chosen := options[0].label
	for i, option := range options {
		names[i] = option.label
		if option.id == current {
			chosen = option.label
		}
	}
	if ui.Select(c, &chosen, names).Label(label).Width(150).Changed() {
		for _, option := range options {
			if option.label == chosen {
				return option.id, true
			}
		}
	}
	return current, false
}

func (a *app) applyLyricSource() {
	a.settings.saveSoon()
	if a.pl != nil {
		a.bindOfflineLyrics()
		a.pl.ReloadLyrics()
	}
	a.redraw()
}

func (a *app) setSongLyricSource(id int64, source string) {
	if tr := a.pl.Current(); tr == nil || tr.ID != id || tr.ProgramID != 0 {
		return
	}
	if source == "auto" && (a.settings.LyricSource == "" || a.settings.LyricSource == "auto") {
		source = ""
	}
	if a.settings.SongLyricSources[id] == source {
		return
	}
	if source == "" {
		delete(a.settings.SongLyricSources, id)
	} else {
		if a.settings.SongLyricSources == nil {
			a.settings.SongLyricSources = make(map[int64]string)
		}
		a.settings.SongLyricSources[id] = source
	}
	a.applyLyricSource()
}

func (a *app) lyricSourceButton(c *ui.Context, id int64) {
	display, _ := a.pl.LyricsDisplay()
	label := "歌词来源"
	switch display.Source {
	case lyrics.AMLL:
		label = "AMLL"
	case lyrics.NetEase:
		label = "网易云"
	}
	b := ui.ButtonBase(c.Key("lyric-source")).Padding(4, 8).Radius(999).Gap(4).AlignItems(ui.Center).
		FocusRing(false).Label("歌词来源")
	fg := white.Alpha(0.68)
	if b.Hovered() {
		b.Background(white.Alpha(0.08))
		fg = white.Alpha(0.9)
	}
	b.Children(func() {
		ui.Text(c, label).FontSize(13).FontWeight(600).TextColor(fg)
		ui.Icon(c, iconDown).Size(12, 12).TextColor(fg)
	})
	b.Menu(func(m *ui.Menu) {
		override := a.settings.SongLyricSources[id]
		followsAuto := a.settings.LyricSource == "" || a.settings.LyricSource == "auto"
		if !followsAuto && m.Item("跟随设置").Checked(override == "").Chosen() {
			a.setSongLyricSource(id, "")
		}
		for _, option := range lyricSources {
			selected := override == option.id || override == "" && followsAuto && option.id == "auto"
			if m.Item(option.label).Checked(selected).Chosen() {
				a.setSongLyricSource(id, option.id)
			}
		}
	})
}
