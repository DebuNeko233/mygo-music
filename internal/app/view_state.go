package app

import (
	"time"

	"github.com/egoist/mygo/ui"
)

// mainViewState belongs to the main window. It never owns playback or page data.
type mainViewState struct {
	wasBehind bool

	pal  palette
	pics map[string]*backdropEntry // shared by the home and playing backdrops

	accountOpen bool // the dropdown of the account row shows
	intro       descriptionDialog
	toast       string // a message to show once

	dailySpin spinState // the record on the daily card
	dailyBars barState  // the bars on its play button

	trackList      ui.ListState
	trackRowsCache trackRowsCache
	localView      localViewState
	trackArt       trackArtworkState

	pageScroll ui.ScrollState
	navScroll  ui.ScrollState
	scrolled   route // the page trackList and pageScroll are of

	locateViewport                                 trackViewport
	mineCollapsed, savedCollapsed, albumsCollapsed bool
	searchBox                                      ui.Handle

	npOpen         bool      // Now Playing as it last showed: open or not
	npChangedAt    time.Time // when it last opened or closed
	npPointer      ui.Point
	npPointerUntil time.Time

	lyricScroll                    ui.ScrollState
	lyricGlide                     lyricGlide
	lyricResizeAnchor              lyricReadingAnchor
	lyricBlurPass                  *ui.Canvas
	paintedLyric                   paintedLyric
	playbackFont                   lyricFontSelection
	timedList                      timedListLayout
	lyricViewportW, lyricViewportH float32 // last viewport followed; resize can reflow lines
	lyricWindowW, lyricWindowH     float32 // window size whose reflow has been scheduled

	queueList ui.ListState
	queueAt   int // the row of the queue pane last scrolled to, -1 while it is hidden

	commentScroll ui.ScrollState
	tabCount      rollState // the count on the comments' tab, as it rolls
	stageWords    stageWordLayout

	volumeOpen       bool    // the volume popover of the player bar shows
	volumeScroll     float64 // precise scroll DIPs saved toward the next ten-percent step
	volumeScrollDown bool    // the direction those DIPs were going

	rowBars               barState
	underTop, underBottom bool

	barScrub              scrubState
	barCover              coverFadeState
	stageCover, wallCover coverFadeState
	npScrub               scrubState

	backdropCache backdropCache // the backdrop of Now Playing, as last painted
	npSpectrum    lyricSpectrum // the filled frequency spectrum behind the lyrics
	npArt         *artwork      // the cover whose colors Now Playing shows
	npCoverState  coverState    // the cover of Now Playing, as it is tilted and flicked
	wallState     wallState     // the wall of the queue's covers, as it is dragged
	quiet         quietState    // the quiet parts of the song playing, for its hot comments

	sig playerSig
}

// settingsViewState contains only interaction state of the settings window.
type settingsViewState struct {
	recording         string
	recordingNote     string
	downloadList      ui.ListState
	settingsSearchBox ui.Handle
	settingsUnderTop  bool
}
