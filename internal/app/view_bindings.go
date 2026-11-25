package app

import (
	desktopview "netease-music-mygo/internal/desktop/view"
	artistview "netease-music-mygo/internal/desktop/view/artist"
	"netease-music-mygo/internal/desktop/view/components"
	libraryview "netease-music-mygo/internal/desktop/view/library"
	localview "netease-music-mygo/internal/desktop/view/local"
	"netease-music-mygo/internal/desktop/view/page"
	playbackview "netease-music-mygo/internal/desktop/view/playback"
	searchview "netease-music-mygo/internal/desktop/view/search"
	settingsview "netease-music-mygo/internal/desktop/view/settings"
	"netease-music-mygo/internal/desktop/view/tracklist"
)

type desktopViews struct {
	playback playbackview.View
	page     page.Page
	album    desktopview.Album
	user     desktopview.User
	recent   desktopview.Recent
	search   searchview.View
	artist   artistview.View
	cloud    desktopview.Cloud
	local    localview.View
	podcast  desktopview.Podcast
	artwork  components.Artwork
	cards    desktopview.Cards
	tracks   tracklist.View
	account  desktopview.Account
	library  libraryview.View
	like     components.Like
	songMenu desktopview.SongMenu
	settings settingsview.View
	daily    desktopview.Daily
	radio    desktopview.Radio
	comments desktopview.Comments
}

// Bind once before rendering. Cross-view method values retain pointers to the
// stable fields in desktopViews; none are invoked until every group is bound.
func (a *app) bindDesktopViews() {
	a.bindSharedViews()
	a.bindBrowseViews()
	a.bindLibraryViews()
	a.bindSettingsViews()
	a.bindPlaybackViews()
}
