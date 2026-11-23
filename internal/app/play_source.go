package app

import "netease-music-mygo/internal/player"

// playSource is the list the listener chose a queue from, as NetEase names it.
// An unknown page leaves the source out rather than inventing an attribution.
func (a *app) playSource() player.PlaySource {
	switch a.route.Page {
	case pagePlaylist:
		return player.PlaySource{Source: "list", ID: a.route.ID}
	case pageAlbum:
		return player.PlaySource{Source: "album", ID: a.route.ID}
	case pageArtist:
		return player.PlaySource{Source: "artist", ID: a.route.ID}
	case pageSearch:
		return player.PlaySource{Source: "search"}
	case pageDaily:
		return player.PlaySource{Source: "dailySongRecommend"}
	default:
		return player.PlaySource{}
	}
}
