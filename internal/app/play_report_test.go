package app

import (
	"errors"
	"slices"
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
		{"too short", netease.Track{ID: 7}, 2 * time.Second, player.MovedOn,
			[]reportedPlay{{id: 7, start: true}}},
		{"episode", netease.Track{ID: 7, ProgramID: 900}, 95 * time.Second, player.MovedOn, nil},
	} {
		t.Run(tc.name, func(t *testing.T) {
			a, sent := reportFixture(t)
			start := player.PlaybackEvent{Track: tc.track, Started: true}
			a.playback(start)
			a.playback(player.PlaybackEvent{Track: tc.track, End: tc.end, Position: tc.at})
			a.playback(player.PlaybackEvent{Track: tc.track, End: tc.end, Position: tc.at})
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
	reportPlay = func(id int64, played time.Duration, end string, source netease.PlaySource) error {
		if len(sent) != 1 || !sent[0].start {
			t.Errorf("play arrived before startplay completed: %+v", sent)
		}
		sent = append(sent, reportedPlay{id: id, played: played, end: end, source: source})
		done <- struct{}{}
		return nil
	}
	t.Cleanup(func() { reportPlayStart = netease.ReportPlayStart; reportPlay = netease.ReportPlay })
	track := netease.Track{ID: 7}
	source := player.PlaySource{Source: "list", ID: 42, Alg: "itembased"}
	a.playback(player.PlaybackEvent{Track: track, Source: source, Started: true})
	a.playback(player.PlaybackEvent{Track: track, Source: source, End: player.MovedOn, Position: 10 * time.Second})
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
	reportPlay = func(id int64, played time.Duration, end string, source netease.PlaySource) error {
		sent <- reportedPlay{id: id, played: played, end: end, source: source}
		return nil
	}
	t.Cleanup(func() { reportPlayStart = netease.ReportPlayStart; reportPlay = netease.ReportPlay })
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
