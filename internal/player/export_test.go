package player

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"

	"netease-music-mygo/internal/netease"
)

// Export reuses only a complete cache at the returned quality and byte size;
// editing the exported copy cannot alter a playing or cached original.
func TestCopyAudioCacheAndNetwork(t *testing.T) {
	audio := testAudio(t, "seek.mp3")
	var calls atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		http.ServeContent(w, r, "audio", time.Time{}, bytes.NewReader(audio))
	}))
	defer srv.Close()
	dir := t.TempDir()
	c := NewCache(dir, 16<<20)
	st := &netease.Stream{URL: srv.URL, Type: "mp3", Level: "exhigh", Size: int64(len(audio))}
	meta, _ := json.Marshal(st)
	cachePath := filepath.Join(dir, "12-exhigh.mp3")
	os.WriteFile(cachePath, audio, 0600)
	os.WriteFile(filepath.Join(dir, "12-exhigh.json"), meta, 0600)
	for _, level := range []string{"exhigh", "lossless"} {
		copyInfo := *st
		copyInfo.Level = level
		f, err := os.CreateTemp(t.TempDir(), "export")
		if err != nil {
			t.Fatal(err)
		}
		if err := c.CopyAudio(context.Background(), 12, &copyInfo, f, nil); err != nil {
			t.Fatal(err)
		}
		got, _ := os.ReadFile(f.Name())
		if !bytes.Equal(got, audio) {
			t.Fatal("copy changed")
		}
		if _, err := f.WriteAt([]byte("changed"), 0); err != nil {
			t.Fatal(err)
		}
		f.Close()
		got, _ = os.ReadFile(cachePath)
		if !bytes.Equal(got, audio) {
			t.Fatal("cache was modified")
		}
	}
	if calls.Load() != 1 {
		t.Fatalf("cache quality ignored: %d requests", calls.Load())
	}
}

// Canceling a stalled export interrupts the existing downloader promptly and
// returns the cancellation rather than reporting a partially copied success.
func TestCopyAudioCancellation(t *testing.T) {
	started := make(chan struct{})
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte("prefix"))
		w.(http.Flusher).Flush()
		close(started)
		<-r.Context().Done()
	}))
	defer srv.Close()
	f, err := os.CreateTemp(t.TempDir(), "export")
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan error, 1)
	go func() { var c *Cache; done <- c.CopyAudio(ctx, 1, &netease.Stream{URL: srv.URL, Size: -1}, f, nil) }()
	select {
	case <-started:
	case <-time.After(5 * time.Second):
		t.Fatal("request did not start")
	}
	cancel()
	select {
	case err := <-done:
		if !errors.Is(err, context.Canceled) {
			t.Fatal(err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("cancel stuck")
	}
}
