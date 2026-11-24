package playback

import (
	"time"

	"github.com/egoist/mygo/ui"

	"netease-music-mygo/internal/desktop/view/components"
)

// State belongs to the main playback view; it holds no duplicate player data.
type State struct {
	NPOpen         bool      // Now Playing as it last showed: open or not
	NPChangedAt    time.Time // when it last opened or closed
	NPPointer      ui.Point
	NPPointerUntil time.Time

	LyricScroll                    ui.ScrollState
	LyricGlide                     lyricGlide
	LyricResizeAnchor              lyricReadingAnchor
	LyricBlurPass                  *ui.Canvas
	PaintedLyric                   paintedLyric
	PlaybackFont                   lyricFontSelection
	TimedList                      TimedListLayout
	LyricViewportW, LyricViewportH float32 // last viewport followed; resize can reflow lines
	LyricWindowW, LyricWindowH     float32 // window size whose reflow has been scheduled

	QueueList ui.ListState
	QueueAt   int // the row of the queue pane last scrolled to, -1 while it is hidden

	TabCount   rollState // the count on the comments' tab, as it rolls
	StageWords stageWordLayout

	VolumeOpen       bool    // the volume popover of the player bar shows
	VolumeScroll     float64 // precise scroll DIPs saved toward the next ten-percent step
	VolumeScrollDown bool    // the direction those DIPs were going

	RowBars components.BarState

	BarScrub              scrubState
	BarCover              coverFadeState
	StageCover, WallCover coverFadeState
	NPScrub               scrubState

	BackdropCache BackdropCache    // the backdrop of Now Playing, as last painted
	NPSpectrum    LyricSpectrum    // the filled frequency spectrum behind the lyrics
	NPArt         components.Image // the cover whose colors Now Playing shows
	NPCoverState  coverState       // the cover of Now Playing, as it is tilted and flicked
	WallState     wallState        // the wall of the queue's covers, as it is dragged
	Quiet         quietState       // the quiet parts of the song playing, for its hot comments

	Sig PlayerSig
}
