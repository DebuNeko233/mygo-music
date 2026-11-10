package app

import (
	"time"

	"netease-music-mygo/internal/netease"
)

// playWorth is how long a song must be heard before NetEase counts it as
// played.
const playWorth = 3 * time.Second

// playSlack is how near a song's end counts as hearing it through.
const playSlack = time.Second

// reportPlayStart tells NetEase that a song began playing.
var reportPlayStart func(id int64) error = netease.ReportPlayStart

// reportPlay tells NetEase how a song was heard: which one, for how long, and
// how it ended.
var reportPlay func(id int64, played time.Duration, end string) error = netease.ReportPlay

// heardPlay is the song playing and the position it reached, reported once
// the listener leaves it: NetEase's own players report a play when a song
// ends, with the position it reached, not when it starts.
type heardPlay struct {
	id   int64
	at   time.Duration
	sent bool // whether this play was reported
}

// notePlay follows what plays and tells NetEase when a song starts, then how
// that play went once playback leaves it, which NetEase files under what they
// played lately and counts as their listening, as its own clients do. It runs
// every frame, so the position a song was left at is known when another takes
// its place. A podcast episode is not a song, and neither is one heard for
// three seconds.
func (a *app) notePlay() {
	tr := a.pl.Current()
	if tr == nil || tr.ProgramID != 0 || a.profile == nil {
		a.closePlay(netease.Interrupted)
		return
	}
	pos := a.pl.Position()
	switch {
	case tr.ID != a.heard.id:
		a.closePlay(netease.MovedOn)
		a.heard = heardPlay{id: tr.ID}
		a.startPlay(tr.ID)
	case pos < a.heard.at-playSlack:
		// The song started over, so the play before it stopped where it was.
		a.closePlay(a.endOf())
		a.heard = heardPlay{id: tr.ID}
		a.startPlay(tr.ID)
	}
	if pos > a.heard.at {
		a.heard.at = pos
	}
	if !a.pl.Playing() && !a.pl.Loading() {
		a.closePlay(a.endOf())
	}
}

// endOf names how a play ended by the position it reached.
func (a *app) endOf() string {
	if length := a.pl.Duration(); length > 0 && a.heard.at >= length-playSlack {
		return netease.Playthrough
	}
	return netease.Interrupted
}

// startPlay tells NetEase that a song began, as its own players do when one
// song gives way to another.
func (a *app) startPlay(id int64) {
	go func() { _ = reportPlayStart(id) }()
}

// closePlay reports the song that stopped playing, one report to a play.
func (a *app) closePlay(end string) {
	if a.heard.id == 0 || a.heard.sent {
		return
	}
	a.heard.sent = true
	if a.heard.at < playWorth {
		return
	}
	id, played := a.heard.id, a.heard.at
	go func() { _ = reportPlay(id, played, end) }()
}
