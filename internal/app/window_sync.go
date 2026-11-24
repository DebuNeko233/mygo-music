package app

import (
	"github.com/egoist/mygo"

	playbackview "netease-music-mygo/internal/desktop/view/playback"
)

// cursorPoint is where the pointer is on the screen.
var cursorPoint = func() mygo.Point { return mygo.Screen.CursorScreenPoint() }

// syncWindows redraws both windows when what plays changed in one of
// them, so the other shows it too.
func (a *app) syncWindows() {
	pl := a.pl
	var id int64
	if tr := pl.Current(); tr != nil {
		id = tr.Key()
	}
	sig := playbackview.PlayerSig{Track: id, Playing: pl.Playing(), Loading: pl.Loading(), Buffering: pl.Buffering(), Shuffle: pl.Shuffle(), Repeat: pl.Repeat(), Volume: a.playbackState.Volume, NowPlaying: a.playbackState.Open, Err: pl.Err()}
	if sig != a.Sig {
		a.Sig = sig
		a.redraw()
		if sig.Playing && !sig.Loading && a.notch != nil {
			a.notch.Greet()
		}
	}
}
