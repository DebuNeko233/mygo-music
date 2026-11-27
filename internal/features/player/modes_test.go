/*
验证显式播放模式选择的幂等性、当前曲目保持与状态通知。
*/

package player

import (
	"slices"
	"testing"
	"time"

	"netease-music-mygo/internal/features/netease"
)

// Empty playback commands stay idle before and during a cache move, through
// Player itself so launchers, hotkeys and media controls share the guarantee.
func TestTogglePlayEmptyQueueDuringCacheMove(t *testing.T) {
	p := New(func(func()) {})
	defer p.Close()
	changes := 0
	p.Changed = func() { changes++ }
	for _, moving := range []bool{false, true} {
		if moving {
			p.SuspendCache()
		}
		changes = 0
		p.TogglePlay()
		p.TogglePlay()
		if p.Playing() || p.Loading() || p.Ready() || p.Current() != nil || changes != 0 {
			t.Fatalf("moving=%v: empty queue published playback intent or activity", moving)
		}
	}
}

// Selecting an active shuffle mode must preserve traversal and the selected
// duplicate queue entry; switching off restores its original queue position.
func TestSetShufflePreservesSelectionAndNotifies(t *testing.T) {
	p := New(func(func()) {})
	p.SuspendCache()
	defer p.Close()
	p.PlayQueue([]netease.Track{{ID: 1}, {ID: 2}, {ID: 1}, {ID: 3}}, 2)
	p.Seek(1400 * time.Millisecond)
	changes := 0
	p.Changed = func() { changes++ }
	p.SetShuffle(true)
	if p.order[p.at] != 2 || changes != 1 {
		t.Fatal("shuffle moved the selected duplicate or failed to notify")
	}
	order, revision := slices.Clone(p.order), p.QueueRevision()
	p.SetShuffle(true)
	if !slices.Equal(order, p.order) || revision != p.QueueRevision() || changes != 1 {
		t.Fatal("reselecting shuffle changed traversal or published a change")
	}
	p.ToggleShuffle()
	if p.Shuffle() || p.at != 2 || !slices.Equal(p.order, []int{0, 1, 2, 3}) || changes != 2 {
		t.Fatal("toggle did not restore queue order and notify")
	}
	p.SetShuffle(false)
	if changes != 2 || p.Position() != 1400*time.Millisecond || !p.Playing() {
		t.Fatal("reselecting shuffle-off notified or changed playback progress")
	}
}

// Explicit repeat choices and their repeated selection preserve the current
// song, progress, traversal and playing intent while notifying only changes.
func TestSetRepeatPreservesPlayback(t *testing.T) {
	p := New(func(func()) {})
	defer p.Close()
	p.SuspendCache()
	p.PlayQueue([]netease.Track{{ID: 1}, {ID: 2}}, 1)
	p.Seek(1700 * time.Millisecond)
	order, at := slices.Clone(p.order), p.at
	revision, seeks := p.QueueRevision(), p.SeekRevision()
	changes := 0
	p.Changed = func() { changes++ }
	for i, mode := range []RepeatMode{RepeatOne, RepeatAll, RepeatOff} {
		p.SetRepeat(mode)
		p.SetRepeat(mode)
		if p.Repeat() != mode || changes != i+1 || p.Current().ID != 2 || !p.Playing() ||
			p.Position() != 1700*time.Millisecond || p.at != at || !slices.Equal(p.order, order) ||
			p.QueueRevision() != revision || p.SeekRevision() != seeks {
			t.Fatalf("repeat selection changed playback or published twice: %v", mode)
		}
	}
}

// Mode changes notify even before a queue exists; no-op/invalid repeat values
// do not publish changes or alter the queue revision.
func TestPlaybackModesWithoutQueue(t *testing.T) {
	p := New(func(func()) {})
	defer p.Close()
	changes := 0
	p.Changed = func() { changes++ }
	p.SetShuffle(true)
	p.SetRepeat(RepeatOne)
	p.SetRepeat(RepeatOne)
	p.SetRepeat(RepeatMode(-1))
	p.SetRepeat(RepeatMode(3))
	if changes != 2 || !p.Shuffle() || p.Repeat() != RepeatOne || p.QueueRevision() != 0 {
		t.Fatal("empty queue mode changes were lost or invalid modes were accepted")
	}
	p.CycleRepeat()
	if p.Repeat() != RepeatOff || changes != 3 {
		t.Fatal("cycle did not use the notifying repeat setter")
	}
}
