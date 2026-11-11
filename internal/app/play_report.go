package app

import (
	"log"
	"sync"
	"time"

	"netease-music-mygo/internal/netease"
	"netease-music-mygo/internal/player"
)

// playWorth is how long a song must be heard before NetEase counts it as played.
const playWorth = 3 * time.Second

var reportPlayStart func(int64, netease.PlaySource) error = netease.ReportPlayStart
var reportPlay func(int64, time.Duration, string, netease.PlaySource) error = netease.ReportPlay

// heardPlay is the one actual audio session waiting for its final play log.
type heardPlay struct {
	id     int64
	active bool
}

// playback receives the player's explicit transitions, never a UI-frame sample.
// Restoring paused, pausing and seeking emit no transition, so none can create
// or discard a report. These callbacks run on the app's main thread.
func (a *app) playback(event player.PlaybackEvent) {
	if event.Started {
		if event.Track.ProgramID != 0 || a.profile == nil {
			return
		}
		source := netease.PlaySource{Source: event.Source.Source, ID: event.Source.ID, Alg: event.Source.Alg}
		a.heard = heardPlay{id: event.Track.ID, active: true}
		a.enqueueReport(func() {
			if err := reportPlayStart(event.Track.ID, source); err != nil {
				log.Printf("play report: start %d: %v", event.Track.ID, err)
			}
		})
		return
	}
	if !a.heard.active || a.heard.id != event.Track.ID {
		return
	}
	a.heard.active = false
	if event.Position < playWorth {
		return
	}
	source := netease.PlaySource{Source: event.Source.Source, ID: event.Source.ID, Alg: event.Source.Alg}
	end := string(event.End)
	a.enqueueReport(func() {
		if err := reportPlay(event.Track.ID, event.Position, end, source); err != nil {
			log.Printf("play report: end %d: %v", event.Track.ID, err)
		}
	})
}

// enqueueReport keeps each session's startplay before its play, and different
// sessions in playback order. A failed request is logged rather than lost silently.
func (a *app) enqueueReport(send func()) {
	a.reportOnce.Do(func() {
		a.reportQueue = make(chan func(), 64)
		a.reportDone = make(chan struct{})
		go func() {
			for send := range a.reportQueue {
				send()
			}
			close(a.reportDone)
		}()
	})
	a.reportQueue <- send
}

// reportPlayStopped closes a session on account changes and application exit.
func (a *app) reportPlayStopped() { a.pl.Finish() }

// flushPlayReports finishes the ordered start/end requests before the process
// exits or the account changes. A later login creates a fresh queue.
func (a *app) flushPlayReports() {
	a.reportPlayStopped()
	if a.reportQueue != nil {
		close(a.reportQueue)
		<-a.reportDone
		a.reportQueue = nil
		a.reportDone = nil
		a.reportOnce = sync.Once{}
	}
}
