package app

import (
	"netease-music-mygo/internal/netease"
	searchvm "netease-music-mygo/internal/viewmodel/search"
)

func (a *app) searchTracks() searchvm.TrackSearchList {
	switch a.nav.Current.Page {
	case pagePlaylist:
		if pl := a.library.Playlists[a.nav.Current.ID]; pl != nil {
			return searchvm.TrackSearchList{Tracks: pl.Tracks, Playlist: pl, Revision: a.library.Refresh.Revision, Label: "当前歌单", Owner: a.nav.Current.ID}
		}
	case pageCloud:
		if a.account.Profile != nil {
			return searchvm.TrackSearchList{Tracks: a.cloud.Tracks, Revision: a.library.Refresh.Revision, Label: "网盘", Owner: a.listenerID()}
		}
	case pageLocal:
		return searchvm.TrackSearchList{Tracks: a.local.Tracks, Revision: a.local.Revision, Label: "本地"}
	}
	return searchvm.TrackSearchList{}
}

func (a *app) trackSuggestions(q string) []searchvm.TrackSuggestion {
	return a.searches.TrackSuggestions(q, a.nav.Current, a.listenerID(), a.playbackState.Open, a.searchTracks(), a.local.Missing)
}

func (a *app) chooseTrackSuggestion(key, q string) bool {
	list := a.searchTracks()
	i, handled := a.searches.ResolveTrackSuggestion(key, q, a.nav.Current, a.listenerID(), a.playbackState.Open, list, a.local.Missing)
	if i >= 0 {
		a.rememberSearch(q)
		a.dismissSuggestions()
		a.playTrackQueue(list.Tracks, i)
		a.views.tracks.Reveal(i)
	}
	return handled
}

func (a *app) suggestionPlayable(tr *netease.Track) bool {
	return searchvm.TrackAvailable(tr, a.local.Missing)
}
