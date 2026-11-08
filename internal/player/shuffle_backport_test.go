package player

import (
	"slices"
	"testing"

	"netease-music-mygo/internal/netease"
)

// The new whole-list entry point must preserve the existing clamping of
// explicitly picked indexes, including while shuffle is enabled.
func TestShufflePreservesPickedIndexBounds(t *testing.T) {
	p := New(func(func()) {})
	p.cacheChanging = true
	q := []netease.Track{{ID: 1}, {ID: 2}, {ID: 3}}
	for _, shuffle := range []bool{false, true} {
		p.shuffle = shuffle
		for _, pick := range []struct {
			index int
			want  int64
		}{{-5, 1}, {1, 2}, {99, 3}} {
			p.PlayQueue(q, pick.index)
			if p.Current().ID != pick.want {
				t.Fatalf("shuffle=%v pick=%d: got %d", shuffle, pick.index, p.Current().ID)
			}
		}
	}
	p.PlayShuffled(nil)
	if len(p.Queue()) != 3 || p.Current().ID != 3 {
		t.Fatal("empty shuffle replaced the queue")
	}
}

// An empty whole-list shuffle used to enable the mode without reshuffling
// the existing queue; neither playback nor its mode may change for no tracks.
func TestEmptyShufflePreservesPlayback(t *testing.T) {
	for _, shuffle := range []bool{false, true} {
		p := New(func(func()) {})
		p.cacheChanging = true
		p.shuffle = shuffle
		tracks := []netease.Track{{ID: 1}, {ID: 2}, {ID: 3}}
		p.PlayQueue(tracks, 1)
		order := slices.Clone(p.order)
		p.PlayShuffled(nil)
		if p.shuffle != shuffle || p.Current().ID != 2 || !slices.Equal(p.order, order) || len(p.Queue()) != len(tracks) {
			t.Fatalf("empty shuffle changed playback: mode=%v order=%v", p.shuffle, p.order)
		}
	}
}
