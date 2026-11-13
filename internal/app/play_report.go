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
		// The worker keeps its own queue and done channel: a later session
		// takes the fields, and the one it ends must still finish.
		queue := make(chan func(), 64)
		done := make(chan struct{})
		a.reportQueue, a.reportDone = queue, done
		go func() {
			for send := range queue {
				send()
			}
			close(done)
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

// stopPlayReports ends the session's queue without waiting for it, and returns
// the channel the queue closes when its requests are done, or nil when there
// was nothing to send. Its caller runs on the thread that draws: the requests
// finish on the goroutine that sends them, under the cookies the account had
// while it was signed in, so neither signing in nor signing out waits for the
// network. A later login creates a fresh queue.
func (a *app) stopPlayReports() <-chan struct{} {
	a.reportPlayStopped()
	queue, done := a.reportQueue, a.reportDone
	if queue == nil {
		return nil
	}
	a.reportQueue = nil
	a.reportDone = nil
	a.reportOnce = sync.Once{}
	// The queue outlives the account that filled it: it sends with that
	// account's cookies, not with those of whoever signs in next.
	if a.reportRestore == nil {
		a.reportRestore = netease.SnapshotPlayReports()
	}
	close(queue)
	go func() {
		<-done
		a.post(func() {
			if a.reportRestore != nil {
				a.reportRestore()
				a.reportRestore = nil
			}
		})
	}()
	return done
}

// flushPlayReports finishes the ordered start/end requests before the process
// exits. By then every window is closed: waiting here holds up the exit, not
// the drawing.
func (a *app) flushPlayReports() {
	if done := a.stopPlayReports(); done != nil {
		select {
		case <-done:
		case <-time.After(2 * time.Second):
			log.Printf("play report: final requests did not finish before the exit")
		}
	}
}
