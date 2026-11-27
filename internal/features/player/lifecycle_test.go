/*
验证播放器关闭早于解码结果投递或应用时，预备解码器仍被释放。
*/

package player

import (
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"

	"netease-music-mygo/internal/features/netease"
)

type countedStream struct {
	stream
	closes atomic.Int32
}

func (s *countedStream) Close() error { s.closes.Add(1); return s.stream.Close() }

// A selection notification may close the player synchronously. Its prepared
// decoder must be retired before PlayTemporary returns, without a new worker.
func TestPlayerSelectionNotificationCanClose(t *testing.T) {
	posts := make(chan func(), 64)
	resources := make(chan [2]func(), 1)
	p := New(func(fn func()) { posts <- fn }, func(fn, release func()) { resources <- [2]func(){fn, release} })
	defer p.Close()
	audio, err := PrepareLocalAudio(filepath.Join("testdata", "seek.flac"))
	if err != nil {
		t.Fatal(err)
	}
	tracked := &countedStream{stream: audio.stream}
	audio.stream = tracked
	resolved := 0
	p.LocalPath = func(int64) string { resolved++; return "" }
	p.Changed = p.Close
	p.PlayTemporary(netease.Track{LocalID: 1}, audio)
	closedAtReturn := tracked.closes.Load()
	if closedAtReturn == 0 {
		// Retire the obsolete worker's resource before reporting the regression.
		select {
		case result := <-resources:
			result[1]()
		case <-time.After(5 * time.Second):
			audio.Close()
			t.Fatal("closed player retained its prepared decoder")
		}
	}
	if closedAtReturn != 1 || resolved != 0 || p.Ready() || p.Loading() || p.Playing() || len(resources) != 0 {
		t.Fatalf("notification close started obsolete loading: closed=%d resolved=%d ready=%v loading=%v playing=%v", closedAtReturn, resolved, p.Ready(), p.Loading(), p.Playing())
	}
}

// A notification that selects another track retires the first prepared
// decoder immediately and never starts a second, obsolete audio worker.
func TestPlayerSelectionNotificationCanReplaceTrack(t *testing.T) {
	resources := make(chan [2]func(), 2)
	p := New(func(func()) {}, func(fn, release func()) { resources <- [2]func(){fn, release} })
	defer p.Close()
	audio, err := PrepareLocalAudio(filepath.Join("testdata", "seek.flac"))
	if err != nil {
		t.Fatal(err)
	}
	tracked := &countedStream{stream: audio.stream}
	audio.stream = tracked
	obsoleteResolutions := 0
	p.LocalPath = func(id int64) string {
		if id == 1 {
			obsoleteResolutions++
		}
		return ""
	}
	p.Changed = func() {
		if p.Current().LocalID == 1 {
			p.PlayQueue([]netease.Track{{LocalID: 2}}, 0)
		}
	}
	p.PlayTemporary(netease.Track{LocalID: 1}, audio)
	closedAtReturn := tracked.closes.Load()
	p.Close()
	workers := 1 // The replacement has its own pending open of a missing local file.
	if closedAtReturn == 0 {
		workers++ // Retire an erroneously started obsolete worker before failing.
	}
	for range workers {
		select {
		case result := <-resources:
			result[1]()
		case <-time.After(5 * time.Second):
			t.Fatal("pending decoder did not reach resource cleanup")
		}
	}
	if closedAtReturn != 1 || tracked.closes.Load() != 1 || obsoleteResolutions != 0 || p.Current().LocalID != 2 || p.Ready() {
		t.Fatalf("replacement kept an obsolete decoder: closed at return=%d total=%d obsolete lookups=%d selected=%d", closedAtReturn, tracked.closes.Load(), obsoleteResolutions, p.Current().LocalID)
	}
}

// A prepared decoder closes when shutdown overtakes either its dispatch or its application.
func TestPlayerClosePendingDecoder(t *testing.T) {
	for _, discard := range []bool{false, true} {
		posts := make(chan func(), 64)
		resources := make(chan [2]func(), 1)
		p := New(func(fn func()) { posts <- fn }, func(fn, release func()) { resources <- [2]func(){fn, release} })
		audio, err := PrepareLocalAudio(filepath.Join("testdata", "seek.flac"))
		if err != nil {
			t.Fatal(err)
		}
		tracked := &countedStream{stream: audio.stream}
		audio.stream = tracked
		p.PlayTemporary(netease.Track{LocalID: 1}, audio)
		var result [2]func()
		select {
		case result = <-resources:
		case <-time.After(5 * time.Second):
			t.Fatal("decoder did not reach dispatch")
		}
		p.Close()
		p.Close()
		if discard {
			result[1]()
		} else {
			result[0]()
		}
		for len(posts) > 0 {
			(<-posts)()
		}
		if tracked.closes.Load() != 1 || p.out != nil || p.Playing() {
			t.Fatalf("discard=%v: closed decoder %d times, output=%v", discard, tracked.closes.Load(), p.out)
		}
	}
}
