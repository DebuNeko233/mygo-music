package app

import (
	"netease-music-mygo/internal/netease"
)

// openPage opens a page in the main window, from wherever a link to it
// shows: over Now Playing, which closes, or the notch.
func (a *app) openPage(r route) {
	a.open(r)
	a.showMain()
	a.redraw()
}

// openArtist opens an artist's page in the main window.
func (a *app) openArtist(id int64) { a.openPage(route{Page: pageArtist, ID: id}) }

// These load an artist, their songs, albums and events from NetEase, and
// follow them; tests replace them.
var (
	artistDetail      = netease.ArtistDetail
	artistLatestSongs = netease.ArtistLatestSongs
	albumsBy          = netease.ArtistAlbums
	accountEvents     = netease.Events
	followArtist      = netease.FollowArtist
)
