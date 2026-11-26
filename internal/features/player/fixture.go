/*
提供视图测试所需的播放器状态注入方法，直接设置队列、歌词和音频而不发起加载。
*/

package player

import (
	"time"

	"github.com/egoist/mygo/audio"

	"netease-music-mygo/internal/features/lyrics"
	"netease-music-mygo/internal/features/netease"
)

// These set a player up as it would be after loading, without loading
// anything, for the tests of the views that show it. Keep these as package
// functions: methods of the production Player can be retained by reflection,
// whereas uncalled fixture functions are omitted from the application binary.

// Cue makes tracks the queue, in order, with the first current.
func Cue(p *Player, tracks []netease.Track) {
	p.queueRevision++
	p.step = 0
	p.queue = tracks
	p.order = make([]int, len(tracks))
	for i := range p.order {
		p.order[i] = i
	}
	p.at = 0
	p.track = &p.queue[0]
}

// SetLyrics gives the current track lines, as loaded.
func SetLyrics(p *Player, lines []lyrics.Line) {
	display := lyrics.NewDisplay(lines)
	p.setLyrics(display.SungLines(), display)
}

// Hold makes the player play at pos with nothing to play: its position
// stays there.
func Hold(p *Player, pos time.Duration) {
	p.pending = pos
	p.playing = true
}

// Attach makes out the current track's audio, playing.
func Attach(p *Player, out *audio.Player) {
	p.out = out
	p.playing = true
}

// SetStream makes st what the current track streams.
func SetStream(p *Player, st *netease.Stream) { p.info = st }

// RefreshLyrics applies parsed replacement lyrics without moving playback.
func RefreshLyrics(p *Player, id int64, original, translation, words string) {
	lines, display := lyrics.BuildDisplay(original, translation, words)
	p.applyRefreshedLyrics(id, lines, display)
}
