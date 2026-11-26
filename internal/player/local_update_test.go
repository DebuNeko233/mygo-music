/*
验证批量更新本地歌曲时同步修改队列内所有副本和当前歌曲。
*/

package player

import (
	"testing"

	"netease-music-mygo/internal/netease"
)

// Batch relinking updates every duplicate and the current track with one queue revision.
func TestUpdateLocalTracksUpdatesTheWholeQueue(t *testing.T) {
	p := &Player{at: 1}
	Cue(p, []netease.Track{{LocalID: 1, Name: "old"}, {LocalID: 2, Name: "current"}, {LocalID: 1, Name: "old"}})
	p.order, p.track = []int{0, 1, 2}, &p.queue[1]
	rev := p.QueueRevision()
	p.UpdateLocalTracks([]netease.Track{{LocalID: 1, Name: "new"}, {LocalID: 2, Name: "relocated"}})
	q := p.Queue()
	if q[0].Name != "new" || q[2].Name != "new" || p.Current().Name != "relocated" {
		t.Fatalf("batch update missed queue entries or current track: %+v", q)
	}
	if p.QueueRevision() != rev+1 {
		t.Fatal("one batch update published more than one queue revision")
	}
}
