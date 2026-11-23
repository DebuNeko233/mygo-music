package app

import (
	"netease-music-mygo/internal/netease"
	"netease-music-mygo/internal/viewmodel"
)

func (a *app) searchTracks() viewmodel.TrackSearchList {
	switch a.nav.Current.Page {
	case pagePlaylist:
		if pl := a.library.Playlists[a.nav.Current.ID]; pl != nil {
			return viewmodel.TrackSearchList{Tracks: pl.Tracks, Playlist: pl, Revision: a.library.Refresh.Revision, Label: "当前歌单", Owner: a.nav.Current.ID}
		}
	case pageCloud:
		if a.account.Profile != nil {
			return viewmodel.TrackSearchList{Tracks: a.cloud.Tracks, Revision: a.library.Refresh.Revision, Label: "网盘", Owner: a.listenerID()}
		}
	case pageLocal:
		return viewmodel.TrackSearchList{Tracks: a.local.Tracks, Revision: a.local.Revision, Label: "本地"}
	}
	return viewmodel.TrackSearchList{}
}

func (a *app) trackSuggestions(q string) []viewmodel.TrackSuggestion {
	return a.searches.TrackSuggestions(q, a.nav.Current, a.listenerID(), a.nowPlaying, a.searchTracks(), a.local.Missing)
}
func (a *app) chooseTrackSuggestion(key, q string) bool {
	list := a.searchTracks()
	i, handled := a.searches.ResolveTrackSuggestion(key, q, a.nav.Current, a.listenerID(), a.nowPlaying, list, a.local.Missing)
	if i >= 0 {
		a.rememberSearch(q)
		a.dismissSuggestions()
		a.playTrackQueue(list.Tracks, i)
		a.revealTrack(i)
	}
	return handled
}
func (a *app) suggestionPlayable(tr *netease.Track) bool {
	return viewmodel.TrackAvailable(tr, a.local.Missing)
}
