package app

import (
	"errors"
	"slices"
	"sync"
	"testing"
	"time"

	"netease-music-mygo/internal/netease"
	"netease-music-mygo/internal/player"
)

type reportedPlay struct {
	id     int64
	played time.Duration
	end    string
	source netease.PlaySource
	start  bool
}

// Playback transitions, not frames or position samples, decide when a play
// starts and ends; pause/resume and seeking keep the same session.
func TestPlayReportsFollowPlaybackSessions(t *testing.T) {
	for _, tc := range []struct {
		name  string
		track netease.Track
		at    time.Duration
		end   player.PlayEnd
		want  []reportedPlay
	}{
		{"moved on", netease.Track{ID: 7}, 95 * time.Second, player.MovedOn,
			[]reportedPlay{{id: 7, start: true}, {id: 7, played: 95 * time.Second, end: "ui"}}},
		{"played through", netease.Track{ID: 7}, 199 * time.Second, player.Playthrough,
			[]reportedPlay{{id: 7, start: true}, {id: 7, played: 199 * time.Second, end: "playend"}}},
		{"interrupted", netease.Track{ID: 7}, 30 * time.Second, player.Interrupted,
			[]reportedPlay{{id: 7, start: true}, {id: 7, played: 30 * time.Second, end: "interrupt"}}},
		{"decode failure", netease.Track{ID: 7}, 30 * time.Second, player.Exception,
			[]reportedPlay{{id: 7, start: true}, {id: 7, played: 30 * time.Second, end: "exception"}}},
		{"too short", netease.Track{ID: 7}, 29 * time.Second, player.MovedOn,
			[]reportedPlay{{id: 7, start: true}}},
		{"episode", netease.Track{ID: 7, ProgramID: 900}, 95 * time.Second, player.MovedOn, nil},
	} {
		t.Run(tc.name, func(t *testing.T) {
			a, sent := reportFixture(t)
			start := player.PlaybackEvent{Track: tc.track, Started: true}
			a.playback(start)
			a.playback(player.PlaybackEvent{Track: tc.track, End: tc.end, Played: tc.at})
			a.playback(player.PlaybackEvent{Track: tc.track, End: tc.end, Played: tc.at})
			if got := waitReports(t, sent, len(tc.want)); !slices.Equal(got, tc.want) {
				t.Errorf("reported %+v, want %+v", got, tc.want)
			}
		})
	}
}

// A restored paused song and a pause/resume send nothing; no hidden-window or
// view frame is needed for a real transition to reach the same reporter.
func TestPausedRestoreAndResumeDoNotCreateAReport(t *testing.T) {
	a, sent := reportFixture(t)
	track := netease.Track{ID: 7}
	// Restore and pause/resume intentionally produce no PlaybackEvent.
	if got := drainReports(sent); len(got) != 0 {
		t.Fatalf("paused restore sent %+v", got)
	}
	a.playback(player.PlaybackEvent{Track: track, Started: true})
	if got := waitReports(t, sent, 1); len(got) != 1 || !got[0].start {
		t.Fatalf("actual resume sent %+v", got)
	}
}

// Requests run in playback order even when startplay fails; the failure must
// not drop or reorder the following final play log.
func TestPlayReportsAreSerialAfterAStartFailure(t *testing.T) {
	a := newTestApp()
	a.profile = &netease.Profile{}
	var sent []reportedPlay
	done := make(chan struct{}, 2)
	reportPlayStart = func(id int64, source netease.PlaySource) error {
		sent = append(sent, reportedPlay{id: id, source: source, start: true})
		done <- struct{}{}
		return errors.New("offline")
	}
	reportDawnStart = func(track netease.Track, bitrate int, level string, source netease.PlaySource) error {
		return nil
	}
	reportDawnEnd = func(track netease.Track, played time.Duration, end string, bitrate int, level string, source netease.PlaySource) error {
		if len(sent) != 1 || !sent[0].start {
			t.Errorf("play arrived before startplay completed: %+v", sent)
		}
		sent = append(sent, reportedPlay{id: track.ID, played: played, end: end, source: source})
		done <- struct{}{}
		return nil
	}
	t.Cleanup(func() {
		reportPlayStart = netease.ReportPlayStart
		reportDawnStart = netease.ReportDawnStart
		reportDawnEnd = netease.ReportDawnEnd
	})
	track := netease.Track{ID: 7}
	source := player.PlaySource{Source: "list", ID: 42, Alg: "itembased"}
	a.playback(player.PlaybackEvent{Track: track, Source: source, Started: true})
	a.playback(player.PlaybackEvent{Track: track, Source: source, End: player.MovedOn, Played: 30 * time.Second})
	for range 2 {
		select {
		case <-done:
		case <-time.After(2 * time.Second):
			t.Fatal("reports did not finish")
		}
	}
	if len(sent) != 2 || sent[1].source.Source != "list" || sent[1].source.ID != 42 || sent[1].source.Alg != "itembased" {
		t.Fatalf("reports %+v", sent)
	}
	a.flushPlayReports()
}

// Quitting waits for the final play request instead of abandoning it in the
// asynchronous reporting queue during process teardown.
func TestFlushPlayReportsCompletesTheFinalRequest(t *testing.T) {
	a := newTestApp()
	a.profile = &netease.Profile{}
	done := make(chan struct{})
	reportPlayStart = func(id int64, source netease.PlaySource) error { return nil }
	reportDawnStart = func(track netease.Track, bitrate int, level string, source netease.PlaySource) error { return nil }
	reportDawnEnd = func(track netease.Track, played time.Duration, end string, bitrate int, level string, source netease.PlaySource) error {
		close(done)
		return nil
	}
	t.Cleanup(func() {
		reportPlayStart = netease.ReportPlayStart
		reportDawnStart = netease.ReportDawnStart
		reportDawnEnd = netease.ReportDawnEnd
	})
	track := netease.Track{ID: 7}
	a.playback(player.PlaybackEvent{Track: track, Started: true})
	a.playback(player.PlaybackEvent{Track: track, End: player.Playthrough, Played: 30 * time.Second})
	finished := make(chan struct{})
	go func() { a.flushPlayReports(); close(finished) }()
	select {
	case <-finished:
	case <-time.After(2 * time.Second):
		t.Fatal("quit did not finish the play report")
	}
	select {
	case <-done:
	default:
		t.Fatal("final play report was dropped")
	}
	// A new login starts a fresh queue after the previous identity drained.
	a.playback(player.PlaybackEvent{Track: track, Started: true})
	a.flushPlayReports()
}

// A slow start request cannot delay later input, and closing its queue cannot
// panic when that request subsequently finishes.
func TestPlayReportsDoNotBlockInputOrCloseDuringShutdown(t *testing.T) {
	a := newTestApp()
	a.profile = &netease.Profile{}
	started := make(chan struct{})
	release := make(chan struct{})
	requestFinished := make(chan struct{})
	oldDawnEnd := reportDawnEnd
	oldEnd := make(chan struct{})
	var endOnce sync.Once
	reportDawnEnd = func(track netease.Track, played time.Duration, end string, bitrate int, level string, source netease.PlaySource) error {
		endOnce.Do(func() { close(oldEnd) })
		return oldDawnEnd(track, played, end, bitrate, level, source)
	}
	var once sync.Once
	reportPlayStart = func(id int64, source netease.PlaySource) error {
		once.Do(func() { close(started) })
		<-release
		return nil
	}
	reportDawnStart = func(track netease.Track, bitrate int, level string, source netease.PlaySource) error {
		select {
		case <-requestFinished:
		default:
			close(requestFinished)
		}
		return nil
	}
	t.Cleanup(func() {
		reportPlayStart = netease.ReportPlayStart
		reportDawnStart = netease.ReportDawnStart
		reportDawnEnd = netease.ReportDawnEnd
	})
	track := netease.Track{ID: 7}
	a.playback(player.PlaybackEvent{Track: track, Started: true})
	<-started
	for range 64 {
		a.playback(player.PlaybackEvent{Track: track, End: player.MovedOn, Played: 30 * time.Second})
		a.playback(player.PlaybackEvent{Track: track, Started: true})
	}
	finished := make(chan struct{})
	go func() { a.flushPlayReports(); close(finished) }()
	select {
	case <-finished:
	case <-time.After(3 * time.Second):
		t.Fatal("flush did not respect its deadline")
	}
	close(release)
	<-requestFinished
	// The old worker's final queued report observes the end stub before cleanup.
	<-oldEnd
	a.playback(player.PlaybackEvent{Track: track, Started: true})
	a.flushPlayReports()
	if a.reportQueue != nil || a.reportDone != nil {
		t.Fatal("the queue outlived the flush that ended it")
	}
	// An event after the flush starts a queue of its own instead of sending on
	// the closed one.
	a.playback(player.PlaybackEvent{Track: track, End: player.MovedOn, Played: 30 * time.Second})
	if a.reportQueue == nil {
		t.Fatal("the event after the flush went nowhere")
	}
	a.flushPlayReports()
}

// A started event must not finish the new session through a stale player pointer.
func TestStartedSessionSurvivesUntilItsEnd(t *testing.T) {
	a, sent := reportFixture(t)
	a.pl = player.New(a.post)
	track := netease.Track{ID: 7}
	a.playback(player.PlaybackEvent{Track: track, Started: true})
	if !a.heard.active || a.heard.id != track.ID {
		t.Fatalf("new session was closed on start: %+v", a.heard)
	}
	a.playback(player.PlaybackEvent{Track: track, End: player.Playthrough, Played: 35 * time.Second})
	if got := waitReports(t, sent, 2); len(got) != 2 || got[1].played != 35*time.Second {
		t.Fatalf("new session sent %+v", got)
	}
}

// Signing in does not wait for the reports the old account still owes: they
// send on under its own cookies, so a stuck request cannot hold up the window.
func TestAccountChangeDoesNotWaitForItsReports(t *testing.T) {
	a := newTestApp()
	a.profile = &netease.Profile{}
	release := make(chan struct{})
	letGo := sync.OnceFunc(func() { close(release) })
	started := make(chan struct{})
	var once sync.Once
	reportPlayStart = func(id int64, source netease.PlaySource) error {
		once.Do(func() { close(started) })
		<-release
		return nil
	}
	reportDawnStart = func(track netease.Track, bitrate int, level string, source netease.PlaySource) error { return nil }
	reportDawnEnd = func(track netease.Track, played time.Duration, end string, bitrate int, level string, source netease.PlaySource) error {
		return nil
	}
	t.Cleanup(func() {
		letGo()
		reportPlayStart = netease.ReportPlayStart
		reportDawnStart = netease.ReportDawnStart
		reportDawnEnd = netease.ReportDawnEnd
	})
	track := netease.Track{ID: 7}
	a.playback(player.PlaybackEvent{Track: track, Started: true})
	<-started

	drained := make(chan (<-chan struct{}), 1)
	go func() { drained <- a.stopPlayReports() }()
	var done <-chan struct{}
	select {
	case done = <-drained:
	case <-time.After(time.Second):
		t.Fatal("the account change waited for the reports")
	}
	if a.reportRestore == nil {
		t.Error("the queued reports lost the cookies of the account that queued them")
	}
	letGo()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("the queued reports did not finish")
	}
	a.flushKept()
	if a.reportRestore != nil {
		t.Error("the signed-in account's cookies never came back")
	}
}

func reportFixture(t *testing.T) (*app, chan reportedPlay) {
	t.Helper()
	a := newTestApp()
	a.profile = &netease.Profile{}
	sent := make(chan reportedPlay, 16)
	reportPlayStart = func(id int64, source netease.PlaySource) error {
		sent <- reportedPlay{id: id, source: source, start: true}
		return nil
	}
	reportDawnStart = func(track netease.Track, bitrate int, level string, source netease.PlaySource) error { return nil }
	reportDawnEnd = func(track netease.Track, played time.Duration, end string, bitrate int, level string, source netease.PlaySource) error {
		sent <- reportedPlay{id: track.ID, played: played, end: end, source: source}
		return nil
	}
	t.Cleanup(func() {
		a.flushPlayReports()
		reportPlayStart = netease.ReportPlayStart
		reportDawnStart = netease.ReportDawnStart
		reportDawnEnd = netease.ReportDawnEnd
	})
	return a, sent
}

func waitReports(t *testing.T, ch chan reportedPlay, n int) []reportedPlay {
	t.Helper()
	var got []reportedPlay
	for len(got) < n {
		select {
		case report := <-ch:
			got = append(got, report)
		case <-time.After(2 * time.Second):
			t.Fatalf("received %+v, want %d reports", got, n)
		}
	}
	return got
}

func drainReports(ch chan reportedPlay) []reportedPlay {
	var got []reportedPlay
	for {
		select {
		case report := <-ch:
			got = append(got, report)
		default:
			return got
		}
	}
}
