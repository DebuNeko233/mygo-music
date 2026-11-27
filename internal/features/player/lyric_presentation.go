/*
保存当前歌词的纯净展示派生结果，与原始歌词同步替换，不持有历史歌曲或界面偏好。
*/

package player

import "netease-music-mygo/internal/features/lyrics"

// LyricPresentation borrows immutable rows and prepared scripts. Views choose
// a mode; the player retains the original source and only one current projection.
type LyricPresentation struct {
	Lines                             []lyrics.Line
	Display                           lyrics.Display
	Loaded                            bool
	OriginalScript, TranslationScript lyrics.Script
}

func (p *Player) preparePureLyrics() {
	var info lyrics.SongInfo
	if p.track != nil {
		info.Title = p.track.Name
		for _, artist := range p.track.Artists {
			info.Artists = append(info.Artists, artist.Name)
		}
	}
	p.pureLyricDisplay = p.lyricDisplay.Pure(info)
	p.pureLyricScript = lyrics.OriginalScript(p.pureLyricDisplay.Lines)
	p.pureLyricTranslationScript = lyrics.TranslationScript(p.pureLyricDisplay.Lines)
}

// PresentLyrics selects prepared data without requests, source edits, seeks or
// per-frame filtering. Selection does not change the player's singing timeline.
func (p *Player) PresentLyrics(pure bool) LyricPresentation {
	if pure {
		return LyricPresentation{p.pureLyricDisplay.Lines, p.pureLyricDisplay, p.lyricsLoaded, p.pureLyricScript, p.pureLyricTranslationScript}
	}
	return LyricPresentation{p.lyrics, p.lyricDisplay, p.lyricsLoaded, p.lyricScript, p.lyricTranslationScript}
}
