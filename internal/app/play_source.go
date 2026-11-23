package app

import "netease-music-mygo/internal/player"

// playSource is the list the listener chose a queue from, as NetEase names it.
// An unknown page leaves the source out rather than inventing an attribution.
func (a *app) playSource() player.PlaySource {
	switch a.nav.Current.Page {
	case pagePlaylist:
		return player.PlaySource{Source: "list", ID: a.nav.Current.ID}
	case pageAlbum:
		return player.PlaySource{Source: "album", ID: a.nav.Current.ID}
	case pageArtist:
		return player.PlaySource{Source: "artist", ID: a.nav.Current.ID}
	case pageSearch:
		return player.PlaySource{Source: "search"}
	case pageDaily:
		return player.PlaySource{Source: "dailySongRecommend"}
	default:
		return player.PlaySource{}
	}
}
