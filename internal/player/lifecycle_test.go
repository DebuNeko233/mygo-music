package player

import (
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"

	"netease-music-mygo/internal/netease"
)

type countedStream struct {
	stream
	closes atomic.Int32
}

func (s *countedStream) Close() error { s.closes.Add(1); return s.stream.Close() }

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
