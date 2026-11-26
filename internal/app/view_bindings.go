/*
持有并一次性连接各功能视图，确保跨视图回调引用稳定的视图实例。
*/

package app

import (
	desktopview "netease-music-mygo/internal/ui/view"
	artistview "netease-music-mygo/internal/ui/view/artist"
	"netease-music-mygo/internal/ui/view/components"
	libraryview "netease-music-mygo/internal/ui/view/library"
	localview "netease-music-mygo/internal/ui/view/local"
	"netease-music-mygo/internal/ui/view/page"
	playbackview "netease-music-mygo/internal/ui/view/playback"
	searchview "netease-music-mygo/internal/ui/view/search"
	settingsview "netease-music-mygo/internal/ui/view/settings"
	"netease-music-mygo/internal/ui/view/tracklist"
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
