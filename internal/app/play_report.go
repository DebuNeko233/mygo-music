package app

import (
	"log"
	"sync"
	"time"

	"netease-music-mygo/internal/netease"
	"netease-music-mygo/internal/player"
)

// playWorth is how long a song must be heard before NetEase's desktop count.
const playWorth = 30 * time.Second

var reportPlayStart func(int64, netease.PlaySource) error = netease.ReportPlayStart
var reportDawnStart = netease.ReportDawnStart
var reportDawnEnd = netease.ReportDawnEnd

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
		if a.reportRestore != nil {
			a.reportRestore()
			a.reportRestore = nil
		}
		if event.Track.ProgramID != 0 || a.profile == nil {
			return
		}
		source := netease.PlaySource{Source: event.Source.Source, ID: event.Source.ID, Alg: event.Source.Alg}
		track := event.Track
		bitrate, level := event.Bitrate, event.Level
		a.heard = heardPlay{id: track.ID, active: true}
		a.reportPlayStopped()
		a.enqueueReport(func() {
			if err := reportPlayStart(track.ID, source); err != nil {
				log.Printf("play report: recent play %d: %v", track.ID, err)
			}
			if err := reportDawnStart(track, bitrate, level, source); err != nil {
				log.Printf("play report: start %d: %v", track.ID, err)
			}
		})
		return
	}
	if !a.heard.active || a.heard.id != event.Track.ID {
		return
	}
	a.heard.active = false
	if event.Played < playWorth {
		return
	}
	track := event.Track
	source := netease.PlaySource{Source: event.Source.Source, ID: event.Source.ID, Alg: event.Source.Alg}
	end := string(event.End)
	played, bitrate, level := event.Played, event.Bitrate, event.Level
	a.enqueueReport(func() {
		if err := reportDawnEnd(track, played, end, bitrate, level, source); err != nil {
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
	queue := a.reportQueue
	if queue == nil {
		return
	}
	select {
	case queue <- send:
	default:
		// Reporting must never stall input or rendering. Ordinary listening
		// cannot fill this queue; a stalled network must not change that.
		log.Printf("play report: queue full; dropping report")
	}
}

// reportPlayStopped closes a session on account changes and application exit.
func (a *app) reportPlayStopped() { a.pl.Finish() }

// flushPlayReports finishes the ordered start/end requests before the process
// exits or the account changes. A later login creates a fresh queue.
func (a *app) flushPlayReports() {
	a.reportPlayStopped()
	if a.reportQueue != nil {
		close(a.reportQueue)
		select {
		case <-a.reportDone:
		case <-time.After(2 * time.Second):
			log.Printf("play report: final requests did not finish before account change")
			if a.reportRestore == nil {
				a.reportRestore = netease.SnapshotPlayReports()
			}
		}
		a.reportQueue = nil
		a.reportDone = nil
		a.reportOnce = sync.Once{}
	}
}
