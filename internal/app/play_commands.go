package app

import "netease-music-mygo/internal/netease"

// playPageTracks starts an album or playlist at its first playable song in order.
func (a *app) playPageTracks(tracks []netease.Track) {
	a.playPageTracksMode(tracks, false)
}

// Artist playback pins the first playable song even when the rest is shuffled.
func (a *app) playPageTracksMode(tracks []netease.Track, shuffle bool) {
	if a.pl.Shuffle() != shuffle {
		a.pl.ToggleShuffle()
	}
	a.playPageQueue(tracks, firstPlayable(tracks))
}

// playPageQueue preserves the shuffle preference and the explicitly chosen song.
func (a *app) playPageQueue(tracks []netease.Track, at int) {
	a.pl.PlayQueueFrom(tracks, at, a.playSource())
}

// Whole-list shuffle may start anywhere, unlike an explicitly chosen song.
func (a *app) shufflePageTracks(tracks []netease.Track) {
	a.pl.PlayShuffledFrom(tracks, a.playSource())
}
