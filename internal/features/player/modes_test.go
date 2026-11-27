/*
验证显式播放模式选择的幂等性、当前曲目保持与状态通知。
*/

package player

import (
	"slices"
	"testing"

	"netease-music-mygo/internal/features/netease"
)

// Selecting an active shuffle mode must preserve traversal and the selected
// duplicate queue entry; switching off restores its original queue position.
func TestSetShufflePreservesSelectionAndNotifies(t *testing.T) {
	p := New(func(func()) {})
	p.SuspendCache()
	defer p.Close()
	p.PlayQueue([]netease.Track{{ID: 1}, {ID: 2}, {ID: 1}, {ID: 3}}, 2)
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
