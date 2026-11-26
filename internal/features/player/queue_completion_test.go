/*
验证补全队列保留当前播放与随机历史，并拒绝已改变归属的队列结果。
*/

package player

import (
	"slices"
	"testing"
	"time"

	"netease-music-mygo/internal/features/netease"
)

// Completing an unchanged source queue deduplicates tracks and preserves pause, position, history and the current song.
func TestAppendQueuePreservesPlayback(t *testing.T) {
	for _, playing := range []bool{false, true} {
		t.Run(map[bool]string{false: "paused", true: "playing"}[playing], func(t *testing.T) {
			p := &Player{}
			Cue(p, []netease.Track{{ID: 1}, {ID: 2}, {LocalID: 3}})
			p.SuspendCache()
			p.at, p.track = 1, &p.queue[1]
			Hold(p, 17*time.Second)
			p.playing = playing
			current, rev := p.Current(), p.QueueRevision()
			if !p.AppendQueue([]netease.Track{{ID: 2}, {ID: 3}, {ID: 3}, {LocalID: 3}, {ID: 4}}, rev) {
				t.Fatal("unchanged queue was rejected")
			}
			if len(p.Queue()) != 5 || !slices.Equal(p.order, []int{0, 1, 2, 3, 4}) || p.Current() != current || p.at != 1 || p.Playing() != playing || p.Position() != 17*time.Second || p.QueueRevision() != rev+1 {
				t.Fatalf("append changed playback: order=%v queue=%v", p.order, p.queue)
			}
			if p.AppendQueue([]netease.Track{{ID: 5}}, rev) {
				t.Fatal("old revision was accepted twice")
			}
		})
	}
}

// New metadata must not overwrite queue edits, a different queue, a radio, or a temporary-file session.
func TestAppendQueueRejectsChangedOwner(t *testing.T) {
	for _, kind := range []string{"add", "remove", "replace", "shuffle", "radio", "temporary"} {
		t.Run(kind, func(t *testing.T) {
			p := &Player{}
			Cue(p, []netease.Track{{ID: 1}, {ID: 2}, {ID: 3}})
			p.SuspendCache()
			rev := p.QueueRevision()
			switch kind {
			case "add":
				p.Enqueue(netease.Track{ID: 8}, true)
			case "remove":
				p.RemoveQueueEntry(1)
			case "replace":
				Cue(p, []netease.Track{{ID: 9}})
			case "shuffle":
				p.ToggleShuffle()
			case "temporary":
				p.queue[0].Temporary = true
			case "radio":
				p.feed = func(func([]netease.Track)) {}
			}
			queue, order, current := slices.Clone(p.queue), slices.Clone(p.order), p.Current()
			if p.AppendQueue([]netease.Track{{ID: 10}}, rev) || !slices.EqualFunc(queue, p.queue, func(a, b netease.Track) bool { return a.Key() == b.Key() }) || !slices.Equal(order, p.order) || p.Current() != current {
				t.Fatal("completion modified a queue it no longer owns")
			}
		})
	}
}

// Extending a shuffled queue preserves played entries and includes every upcoming song exactly once.
func TestAppendQueuePreservesShuffledHistory(t *testing.T) {
	p := &Player{shuffle: true}
	Cue(p, []netease.Track{{ID: 1}, {ID: 2}, {ID: 3}})
	p.SuspendCache()
	p.order, p.at, p.track = []int{2, 0, 1}, 1, &p.queue[0]
	if !p.AppendQueue([]netease.Track{{ID: 4}, {ID: 5}}, p.QueueRevision()) {
		t.Fatal("append rejected")
	}
	if !slices.Equal(p.order[:2], []int{2, 0}) || p.Current().ID != 1 || p.at != 1 {
		t.Fatal("shuffle moved playback history")
	}
	upcoming := slices.Clone(p.order[2:])
	slices.Sort(upcoming)
	if !slices.Equal(upcoming, []int{1, 3, 4}) {
		t.Fatalf("shuffle lost or duplicated songs: %v", p.order)
	}
}
