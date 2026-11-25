package app

import (
	"bytes"
	"os"
	"slices"
	"testing"
	"time"

	"netease-music-mygo/internal/netease"
	"netease-music-mygo/internal/player"
	"netease-music-mygo/internal/sessionstore"
)

// The session is kept a while after playback changes and every while as
// it plays, and paused nothing more is due. It was kept by a ticker that
// woke the app, and redrew its window, every half minute even at rest.
func TestSessionRestsWhilePaused(t *testing.T) {
	isolateUserData(t)
	var due []func()
	defer func(after func(time.Duration, func()) func()) { sessionAfter = after }(sessionAfter)
	sessionAfter = func(_ time.Duration, fn func()) func() { due = append(due, fn); return func() {} }

	a := newTestApp()
	defer a.sessionSave.Wait()
	a.pl.Changed = a.keepSession
	a.pl.Cue([]netease.Track{{ID: 7, Name: "宝船", Duration: 200000}})
	out := newSilentAudio()
	out.Play()
	a.pl.Attach(out)
	defer out.Close()

	a.pl.TogglePlay()
	a.pl.TogglePlay()
	if len(due) != 1 {
		t.Fatalf("%d saves due after pausing and playing", len(due))
	}
	due[0]()
	if len(due) != 2 {
		t.Fatalf("playing, %d saves due after one, want another", len(due)-1)
	}

	a.pl.TogglePlay()
	due[1]()
	if len(due) != 2 {
		t.Errorf("paused, %d more saves due", len(due)-2)
	}
}

// A position-only save leaves the queue file unchanged; an old checkpoint
// cannot override the selection or position after the queue/order changes.
func TestSessionCheckpointFollowsQueue(t *testing.T) {
	isolateUserData(t)
	a := newTestApp()
	a.pl.SuspendCache()
	a.pl.Cue([]netease.Track{{ID: 1}, {ID: 2}, {ID: 3}})
	save := func() {
		a.saveSession()
		a.sessionSave.Wait()
	}
	save()
	path, err := sessionPath()
	if err != nil {
		t.Fatal(err)
	}
	original, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	a.pl.Jump(2)
	a.pl.CycleRepeat()
	a.pl.Hold(8 * time.Second)
	save()
	current, err := os.ReadFile(path)
	if err != nil || !bytes.Equal(original, current) {
		t.Fatal("checkpoint rewrote the full queue")
	}
	s, ok := sessionstore.Read(path)
	if !ok || s.At != 2 || s.Position != 8000 || s.Repeat != player.RepeatAll || len(s.Queue) != 3 {
		t.Fatalf("checkpoint did not restore playback: %+v", s)
	}
	a.pl.ToggleShuffle()
	a.pl.Hold(15 * time.Second)
	save()
	s, ok = sessionstore.Read(path)
	order, at := a.pl.PlayOrder()
	if !ok || s.At != at || !s.Shuffle || s.Position != 15000 || !slices.Equal(s.Order, order) {
		t.Fatalf("restored stale state from before shuffle: %+v", s)
	}
	a.pl.PlayQueue([]netease.Track{{ID: 9}}, 0)
	save()
	s, ok = sessionstore.Read(path)
	if !ok || len(s.Queue) != 1 || s.Queue[0].ID != 9 || s.At != 0 || s.Position != 0 {
		t.Fatalf("restored stale state from another queue: %+v", s)
	}
}

// Radio feed appends and leaving the radio trims the queue without
// necessarily changing the current song; both must rewrite the snapshot.
func TestSessionFollowsRadioQueueEdits(t *testing.T) {
	isolateUserData(t)
	a := newTestApp()
	a.pl.SuspendCache()
	var feed func([]netease.Track)
	a.pl.PlayRadio("test", []netease.Track{{ID: 1}, {ID: 2}}, func(got func([]netease.Track)) {
		feed = got
	})
	if feed == nil {
		t.Fatal("missing radio refill")
	}
	path, err := sessionPath()
	if err != nil {
		t.Fatal(err)
	}
	assertSaved := func(want int, radio string) {
		t.Helper()
		a.saveSession()
		a.sessionSave.Wait()
		s, ok := sessionstore.Read(path)
		if !ok || len(s.Queue) != want || s.Radio != radio || s.Queue[0].ID != 1 {
			t.Fatalf("queue edit was not saved: %+v", s)
		}
	}
	assertSaved(2, "test")
	feed([]netease.Track{{ID: 3}})
	assertSaved(3, "test")
	a.pl.EndRadio()
	assertSaved(1, "")
}
