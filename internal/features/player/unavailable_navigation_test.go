/*
验证失效歌曲沿本次上一曲或下一曲方向跳过，并隔离重试和过期加载结果。
*/

package player

import (
	"slices"
	"testing"
	"testing/synctest"

	"netease-music-mygo/internal/features/netease"
)

// Consecutive failures follow play-order slots, wrap in either direction and
// stop after one traversal, including repeated songs and single-track queues.
func TestUnavailableNavigationTraversal(t *testing.T) {
	for _, tc := range []struct {
		name      string
		ids       []int64
		order     []int
		start     int
		direction int
		failures  int
		want      []int
		stopped   bool
	}{
		{"previous", []int64{1, 2, 3, 4}, nil, 3, -1, 2, []int{2, 1, 0}, false},
		{"previous wraps", []int64{1, 2, 3, 4}, nil, 0, -1, 2, []int{3, 2, 1}, false},
		{"next", []int64{1, 2, 3, 4}, nil, 0, 1, 2, []int{1, 2, 3}, false},
		{"next wraps", []int64{1, 2, 3, 4}, nil, 3, 1, 2, []int{0, 1, 2}, false},
		{"shuffled previous", []int64{1, 2, 3, 4}, []int{2, 0, 3, 1}, 3, -1, 2, []int{2, 1, 0}, false},
		{"all previous", []int64{1, 2, 3, 4}, nil, 0, -1, 4, []int{3, 2, 1, 0}, true},
		{"all next", []int64{1, 2, 3, 4}, nil, 0, 1, 4, []int{1, 2, 3, 0}, true},
		{"duplicate slots", []int64{1, 2, 1}, nil, 0, -1, 3, []int{2, 1, 0}, true},
		{"single track", []int64{1}, nil, 0, -1, 1, []int{0}, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			for _, repeat := range []RepeatMode{RepeatOff, RepeatAll, RepeatOne} {
				p := New(func(func()) {})
				t.Cleanup(p.Close)
				p.SuspendCache() // Complete each load explicitly, without audio or network.
				tracks := make([]netease.Track, len(tc.ids))
				for i, id := range tc.ids {
					tracks[i] = netease.Track{ID: id}
				}
				p.PlayQueue(tracks, 0)
				if tc.order != nil {
					p.shuffle, p.order = true, slices.Clone(tc.order)
				}
				p.Jump(p.order[tc.start])
				p.repeat = repeat
				if tc.direction < 0 {
					p.Prev()
				} else {
					p.Next()
				}
				got := []int{p.at}
				for skipped := 0; skipped < tc.failures; skipped++ {
					p.loading = false
					p.failed(p.at, skipped, *p.Current(), 0, netease.ErrUnavailable)
					if !p.Playing() {
						break
					}
					got = append(got, p.at)
				}
				if !slices.Equal(got, tc.want) || p.Playing() == tc.stopped || p.StepDirection() != tc.direction {
					t.Fatalf("repeat=%d slots=%v playing=%v direction=%d", repeat, got, p.Playing(), p.StepDirection())
				}
				if tc.stopped && p.Err() == "" {
					t.Fatal("exhausted queue did not explain the failure")
				}
			}
		})
	}
}

// A transient failure's timer reloads the same previous song; a later
// definitive restriction continues backward rather than returning to its origin.
func TestUnavailableRetryKeepsPreviousDirection(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		posts := make(chan func(), 1)
		p := New(func(fn func()) { posts <- fn })
		defer p.Close()
		p.SuspendCache()
		p.PlayQueue([]netease.Track{{ID: 1}, {ID: 2}, {ID: 3}}, 2)
		p.Prev()
		p.retry(0, 0, netease.ErrRiskControl)
		(<-posts)() // synctest advances virtual time to the retry, without sleeping.
		if p.Current().ID != 2 || p.StepDirection() != -1 {
			t.Fatal("retry changed the selected previous song")
		}
		p.failed(p.at, 0, *p.Current(), 0, netease.ErrUnavailable)
		if p.Current().ID != 1 || p.StepDirection() != -1 {
			t.Fatal("retry lost its backward unavailable fallback")
		}
	})
}

// Jumping to an earlier row animates backward, but keeps the established
// forward fallback for explicit selection instead of inheriting Prev.
func TestUnavailableDirectSelectionResetsSearchDirection(t *testing.T) {
	p := New(func(func()) {})
	defer p.Close()
	p.SuspendCache()
	p.PlayQueue([]netease.Track{{ID: 1}, {ID: 2}, {ID: 3}, {ID: 4}}, 3)
	p.Prev()
	p.Jump(0)
	if p.StepDirection() != -1 {
		t.Fatal("direct selection lost its backward animation")
	}
	p.failed(p.at, 0, *p.Current(), 0, netease.ErrUnavailable)
	if p.Current().ID != 2 || p.StepDirection() != 1 {
		t.Fatal("direct selection inherited the previous command's fallback")
	}
}

// When opposite navigation overtakes a blocked load, its late unavailable
// result cannot select another song or replace the new song's error.
func TestUnavailableOppositeNavigationRejectsLateLoad(t *testing.T) {
	analysisHTTP(t, nil)
	for _, direction := range []int{-1, 1} {
		t.Run(map[int]string{-1: "previous then next", 1: "next then previous"}[direction], func(t *testing.T) {
			requested, release := make(chan struct{}), make(chan struct{})
			callbacks := make(chan func(), 4)
			oldID := int64(2 + direction)
			old := songURL
			songURL = func(id int64, _ string) (*netease.Stream, error) {
				if id == oldID {
					close(requested)
					<-release
					return nil, netease.ErrUnavailable
				}
				return nil, netease.ErrNotLoggedIn
			}
			t.Cleanup(func() { songURL = old })
			_, p := newLoadPlayer(t, nil)
			p.postResource = func(fn, _ func()) { callbacks <- fn }
			t.Cleanup(func() {
				select {
				case <-release:
				default:
					close(release)
				}
			})
			resume := p.SuspendCache()
			p.PlayQueue([]netease.Track{{ID: 1}, {ID: 2}, {ID: 3}}, 1)
			if direction < 0 {
				p.Prev()
			} else {
				p.Next()
			}
			resume()
			<-requested
			if direction < 0 {
				p.Next()
			} else {
				p.Prev()
			}
			(<-callbacks)() // Accept the newer load first.
			wantErr := p.Err()
			if p.Current().ID != 2 || p.StepDirection() != -direction || p.Playing() || wantErr == "" {
				t.Fatal("newer navigation did not settle on its selected song")
			}
			close(release)
			(<-callbacks)() // Deliver the stale restriction after its replacement.
			if p.Current().ID != 2 || p.StepDirection() != -direction || p.Playing() || p.Err() != wantErr {
				t.Fatal("late restriction changed the newer navigation")
			}
		})
	}
}

// The actual asynchronous load chain C -> unavailable B -> playable A
// must start A's audio, rather than returning to the original song C.
func TestPreviousUnavailableLoadsEarlierAudio(t *testing.T) {
	data := song(t)
	analysisHTTP(t, data)
	old := songURL
	songURL = func(id int64, level string) (*netease.Stream, error) {
		if id == 2 {
			return nil, netease.ErrUnavailable
		}
		return &netease.Stream{URL: "https://fixture.invalid/song", Type: "mp3", Level: level, Size: int64(len(data))}, nil
	}
	t.Cleanup(func() { songURL = old })
	posts, p := newLoadPlayer(t, nil)
	resume := p.SuspendCache()
	p.PlayQueue([]netease.Track{{ID: 1}, {ID: 2}, {ID: 3}}, 2)
	p.Prev()
	resume()
	pump(t, posts, "earlier playable song after unavailable previous song", func() bool { return p.out != nil && !p.Loading() })
	if p.Current().ID != 1 || !p.Playing() || p.StepDirection() != -1 {
		t.Fatalf("song=%d playing=%v direction=%d", p.Current().ID, p.Playing(), p.StepDirection())
	}
}
