/*
协调页面歌曲入队、播放顺序和播放来源，并连接应用与系统音量。
*/

package app

import (
	"netease-music-mygo/internal/features/mixvol"
	"netease-music-mygo/internal/features/netease"
	"netease-music-mygo/internal/features/player"
	"netease-music-mygo/internal/ui/view/tracklist"
	playbackvm "netease-music-mygo/internal/ui/viewmodel/playback"
)

// playPageTracks starts an album or playlist at its first playable song in order.
func (a *app) playPageTracks(tracks []netease.Track) {
	a.playPageTracksMode(tracks, false)
}

// Artist playback pins the first playable song even when the rest is shuffled.
func (a *app) playPageTracksMode(tracks []netease.Track, shuffle bool) {
	if a.pl.Shuffle() != shuffle {
		a.pl.ToggleShuffle()
	}
	a.playPageQueue(tracks, tracklist.FirstPlayable(tracks))
}

// playPageQueue preserves the shuffle preference and the explicitly chosen song.
func (a *app) playPageQueue(tracks []netease.Track, at int) {
	a.pl.PlayQueueFrom(tracks, at, a.playSource())
}

// Whole-list shuffle may start anywhere, unlike an explicitly chosen song.
func (a *app) shufflePageTracks(tracks []netease.Track) {
	a.pl.PlayShuffledFrom(tracks, a.playSource())
}

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

// Keep a queue started from an incomplete cloud listing attached to that load.
// Other sources still use the player's normal snapshot queue.
func (a *app) playTrackQueue(tracks []netease.Track, at int) {
	a.cloud.FollowQueue(0)
	a.playPageQueue(tracks, at)
	if a.nav.Current.Page == pageCloud {
		a.cloud.FollowQueue(a.pl.QueueRevision())
	}
}

func (a *app) volumeServices() playbackvm.VolumeServices {
	return playbackvm.VolumeServices{Player: a.pl, Mixer: a.mix}
}

func (a *app) setVolume(v float64) { a.playbackState.SetVolume(v, a.volumeServices()) }

func (a *app) syncVolume() { a.playbackState.SyncVolume(a.volumeServices()) }

func (a *app) systemVolume(s mixvol.State) { a.playbackState.SystemVolume(s, a.volumeServices()) }
