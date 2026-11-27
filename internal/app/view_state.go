/*
保存主窗口与设置窗口的控件状态，独立于共享业务模型。
*/

package app

import (
	desktopview "netease-music-mygo/internal/ui/view"
	artcache "netease-music-mygo/internal/ui/view/artwork"
	"netease-music-mygo/internal/ui/view/components"
	playbackview "netease-music-mygo/internal/ui/view/playback"
	settingsview "netease-music-mygo/internal/ui/view/settings"
	"netease-music-mygo/internal/ui/view/shell"
	"netease-music-mygo/internal/ui/view/tracklist"
)

// mainViewState belongs to the main window. It never owns playback or page data.
type mainViewState struct {
	wasBehind bool

	pal                           components.Palette
	pics                          *artcache.PictureCache
	homeBackdrop, playingBackdrop artcache.PictureSlot

	intro desktopview.Description
	toast string // a message to show once

	tracks    tracklist.State
	localView localViewState
	trackArt  trackArtworkState

	shell       shell.State
	scrolled    route // the page trackList and shell.PageScroll are of
	resetScroll bool  // reset at the next build, never while a list builds

	playbackview.State
}

// settingsViewState contains the settings window widgets.
type settingsViewState = settingsview.State
