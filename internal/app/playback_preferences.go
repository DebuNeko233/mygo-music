package app

import (
	playbackview "netease-music-mygo/internal/desktop/view/playback"
)

// lyricsPaneChosen is the pane of lyrics the listener chose last: what
// the app starts on, before the player is made. Now Playing shows an
// episode's in place of either.
func (a *app) LyricsPaneChosen() int {
	return playbackview.LyricsPaneChosen(a.settings.LyricsStage)
}
