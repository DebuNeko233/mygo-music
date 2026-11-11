package player

import (
	"testing"

	"netease-music-mygo/internal/netease"
)

// A previous command followed by an exhausted radio's automatic continuation
// must publish a forward transition, not inherit the old backward direction.
func TestRadioContinuationDirection(t *testing.T) {
	p := New(func(func()) {})
	p.SuspendCache()
	var refill func([]netease.Track)
	p.PlayRadio("test", []netease.Track{{ID: 1}, {ID: 2}}, func(got func([]netease.Track)) { refill = got })
	p.Prev()
	p.ended()
	if !p.awaiting || refill == nil {
		t.Fatal("radio did not wait for its pending feed")
	}
	refill([]netease.Track{{ID: 3}})
	if p.Current().ID != 3 || p.awaiting || p.StepDirection() != 1 {
		t.Fatalf("continued with song %d, awaiting=%v, direction=%d", p.Current().ID, p.awaiting, p.StepDirection())
	}
}

// Skipping a song that failed after Prev is an automatic forward move; a
// reload of the same song must leave the last navigation direction alone.
func TestUnavailableSkipDirection(t *testing.T) {
	p := New(func(func()) {})
	p.SuspendCache()
	p.PlayQueue([]netease.Track{{ID: 1}, {ID: 2}, {ID: 3}}, 2)
	p.Prev()
	p.loadSkipping(p.at, 0, true, 0)
	if p.StepDirection() != -1 {
		t.Fatal("reloading the same song changed its navigation direction")
	}
	p.failed(p.at, 0, *p.Current(), 0, netease.ErrUnavailable)
	if p.Current().ID != 3 || p.StepDirection() != 1 {
		t.Fatalf("skip reached %d with direction %d", p.Current().ID, p.StepDirection())
	}
}

// Direct selection follows actual play-order slots, including duplicate
// songs and shuffle; selecting the same slot has no direction.
func TestQueueSelectionDirection(t *testing.T) {
	p := New(func(func()) {})
	p.SuspendCache()
	p.PlayQueue([]netease.Track{{ID: 1}, {ID: 2}, {ID: 3}, {ID: 1}}, 3)
	p.Jump(2)
	if p.StepDirection() != -1 {
		t.Fatal("selection from the last duplicate did not move backward")
	}
	p.Jump(3)
	if p.StepDirection() != 1 {
		t.Fatal("selection of the last duplicate did not move forward")
	}
	p.ToggleShuffle()
	first, second := p.order[0], p.order[1]
	p.Jump(second)
	if p.at != 1 || p.StepDirection() != 1 {
		t.Fatal("selection did not use the shuffled play order")
	}
	p.Jump(first)
	if p.at != 0 || p.StepDirection() != -1 {
		t.Fatal("backward selection did not use the shuffled play order")
	}
	p.Jump(first)
	if p.StepDirection() != 0 {
		t.Fatal("reselecting the same slot inherited an old direction")
	}
}

// Replacing or restoring a queue, or turning it into a radio, must not
// compare positions from the old queue or inherit its previous command.
func TestQueueReplacementDirection(t *testing.T) {
	for _, change := range []string{"queue", "restore", "radio"} {
		t.Run(change, func(t *testing.T) {
			p := New(func(func()) {})
			p.SuspendCache()
			q := []netease.Track{{ID: 1}, {ID: 2}, {ID: 3}}
			p.PlayQueue(q, 1)
			p.Prev()
			switch change {
			case "queue":
				p.PlayQueue(q, 1)
			case "restore":
				p.Restore(Session{Queue: q, Order: []int{0, 1, 2}, At: 1})
			case "radio":
				p.RadioFrom("test", q, func(func([]netease.Track)) {})
			}
			if p.StepDirection() != 0 {
				t.Fatal("replacement inherited a direction from the old queue")
			}
		})
	}
}
