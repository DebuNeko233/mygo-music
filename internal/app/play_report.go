package app

import "netease-music-mygo/internal/netease"

// reportPlay tells NetEase what the listener played.
var reportPlay func(id int64) error = netease.ReportPlay

// notePlay reports the song that began playing, which NetEase files under
// what the listener played lately and counts as their listening, as its own
// clients do. A podcast episode is not a song, and one play is reported once.
func (a *app) notePlay() {
	tr := a.pl.Current()
	if tr == nil || tr.ProgramID != 0 || a.profile == nil || tr.ID == a.reportedTrack {
		return
	}
	a.reportedTrack = tr.ID
	id := tr.ID
	go func() { _ = reportPlay(id) }()
}
