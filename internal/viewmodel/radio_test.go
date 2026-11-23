package viewmodel

import (
	"errors"
	"testing"

	"netease-music-mygo/internal/netease"
	"netease-music-mygo/internal/player"
)

// A-B-A mode changes reject the first A response before the latest A has finished.
func TestRadioSameModeRequestIdentity(t *testing.T) {
	posts := make(chan func(), 16)
	asked := make(chan chan []netease.Track, 4)
	stop := make(chan struct{})
	defer close(stop)
	p := player.New(func(func()) {})
	p.SuspendCache()
	m := new(Radio)
	s := RadioServices{Player: p, Post: func(f func()) { posts <- f }, Notify: func(string) { t.Error("unexpected error") }, PersonalFM: func(string) ([]netease.Track, error) {
		reply := make(chan []netease.Track, 1)
		asked <- reply
		select {
		case tracks := <-reply:
			return tracks, nil
		case <-stop:
			return nil, errors.New("stopped")
		}
	}}
	m.StartFM(FMRadio(netease.FMDefault), s)
	first := wait(t, asked, "first A")
	m.StartFM(FMRadio(netease.FMExplore), s)
	middle := wait(t, asked, "B")
	m.StartFM(FMRadio(netease.FMDefault), s)
	last := wait(t, asked, "last A")
	first <- []netease.Track{{ID: 1}}
	wait(t, posts, "old A")()
	middle <- []netease.Track{{ID: 2}}
	wait(t, posts, "old B")()
	if p.Radio() != "" || m.Starting != FMRadio(netease.FMDefault) {
		t.Fatal("obsolete response started playback")
	}
	tracks := make([]netease.Track, 100)
	for i := range tracks {
		tracks[i].ID = int64(100 + i)
	}
	last <- tracks
	wait(t, posts, "latest A")()
	if p.Current() == nil || p.Current().ID != 100 || p.Radio() != FMRadio(netease.FMDefault) {
		t.Fatal("latest request did not win")
	}
}

// Selecting a normal queue or exiting while FM loads prevents its later response from replacing that choice.
func TestRadioPendingKeepsManualIntent(t *testing.T) {
	for _, exit := range []bool{false, true} {
		p := player.New(func(func()) {})
		p.SuspendCache()
		m := new(Radio)
		posts := make(chan func(), 4)
		s := RadioServices{Player: p, Post: func(f func()) { posts <- f }, PersonalFM: func(string) ([]netease.Track, error) { return []netease.Track{{ID: 7}}, nil }}
		m.StartFM(FMRadio(netease.FMDefault), s)
		update := wait(t, posts, "FM request")
		if exit {
			m.Exit(s)
		} else {
			p.Cue([]netease.Track{{ID: 99}})
		}
		update()
		if p.Radio() != "" || m.Starting != "" || (!exit && (p.Current() == nil || p.Current().ID != 99)) {
			t.Fatal("old FM response replaced newer intent")
		}
	}
}

// An old radio's automatic refill must not cancel the explicit switch to another mode.
func TestRadioModeSwitchSurvivesRefill(t *testing.T) {
	p := player.New(func(func()) {})
	p.SuspendCache()
	p.Cue([]netease.Track{{ID: 1}})
	var refill func([]netease.Track)
	p.AttachRadio(FMRadio(netease.FMDefault), func(got func([]netease.Track)) { refill = got })
	posts := make(chan func(), 4)
	m := new(Radio)
	tracks := make([]netease.Track, 100)
	for i := range tracks {
		tracks[i].ID = int64(100 + i)
	}
	s := RadioServices{Player: p, Post: func(f func()) { posts <- f }, PersonalFM: func(string) ([]netease.Track, error) { return tracks, nil }}
	m.StartFM(FMRadio(netease.FMExplore), s)
	update := wait(t, posts, "new mode")
	if refill == nil {
		t.Fatal("old radio did not request refill")
	}
	refill([]netease.Track{{ID: 2}, {ID: 3}})
	update()
	if p.Radio() != FMRadio(netease.FMExplore) || p.Current() == nil || p.Current().ID != 100 {
		t.Fatal("automatic refill cancelled mode switch")
	}
}
