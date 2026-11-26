/*
按实际播放会话串行上报收听记录，处理账号切换和退出时的最终提交。
*/

package app

import (
	"log"
	"time"

	"netease-music-mygo/internal/features/netease"
	"netease-music-mygo/internal/features/player"
)

// playWorth is how long a song must be heard before NetEase's desktop count.
const playWorth = 30 * time.Second

type playReporter interface {
	PlayStart(int64, netease.PlaySource) error
	DawnStart(netease.Track, int, string, netease.PlaySource) error
	DawnEnd(netease.Track, time.Duration, string, int, string, netease.PlaySource) error
}

// heardPlay is the one actual audio session waiting for its final play log.
type heardPlay struct {
	id     int64
	active bool
}

// playback receives the player's explicit transitions, never a UI-frame sample.
// Restoring paused, pausing and seeking emit no transition, so none can create
// or discard a report. These callbacks run on the app's main thread.
func (a *app) playback(event player.PlaybackEvent) {
	if event.Track.IsLocal() {
		return
	}
	if event.Started {
		if event.Track.ProgramID != 0 || a.account.Profile == nil {
			return
		}
		source := netease.PlaySource{Source: event.Source.Source, ID: event.Source.ID, Alg: event.Source.Alg}
		track := event.Track
		bitrate, level := event.Bitrate, event.Level
		a.heard = heardPlay{id: track.ID, active: true}
		a.enqueueReport(func(reporter playReporter) {
			if err := reporter.PlayStart(track.ID, source); err != nil {
				log.Printf("play report: recent play %d: %v", track.ID, err)
			}
			if err := reporter.DawnStart(track, bitrate, level, source); err != nil {
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
	a.enqueueReport(func(reporter playReporter) {
		if err := reporter.DawnEnd(track, played, end, bitrate, level, source); err != nil {
			log.Printf("play report: end %d: %v", event.Track.ID, err)
		}
	})
}

// enqueueReport keeps each session's startplay before its play, and different
// sessions in playback order. A failed request is logged rather than lost silently.
func (a *app) enqueueReport(send func(playReporter)) {
	if a.reportQueue == nil {
		var reporter playReporter
		if a.newPlayReporter == nil {
			reporter = netease.NewPlayReporter()
		} else {
			reporter = a.newPlayReporter()
		}
		queue := make(chan func(playReporter), 64)
		done := make(chan struct{})
		a.reportQueue, a.reportDone = queue, done
		go func() {
			defer close(done)
			for send := range queue {
				send(reporter)
			}
		}()
	}
	select {
	case a.reportQueue <- send:
	default:
		// Reporting must never stall input or rendering. Ordinary listening
		// cannot fill this queue; a stalled network must not change that.
		log.Printf("play report: queue full; dropping report")
	}
}

// stopPlayReports detaches the current login's queue. Its worker drains
// independently, using the login captured when the queue was started.
func (a *app) stopPlayReports() <-chan struct{} {
	a.pl.Finish()
	queue, done := a.reportQueue, a.reportDone
	if queue != nil {
		a.reportQueue, a.reportDone = nil, nil
		close(queue)
	}
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
