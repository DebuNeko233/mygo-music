/*
验证音频缓存发布与元数据一致性，并覆盖临时存储、下载失败和损坏副本。
*/

package player

import (
	"bytes"
	"encoding/json"
	"math"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"sync/atomic"
	"testing"
	"time"

	"netease-music-mygo/internal/features/netease"
)

// Publishing audio without its sidecar used to leave an unplayable offline cache entry.
func TestCacheMetadataFailureLeavesOnlyTemporaryAudio(t *testing.T) {
	c := NewCache(t.TempDir(), 1<<20)
	key := cacheKey(1, "exhigh")
	if err := os.Mkdir(filepath.Join(c.dir, key+".json"), 0o700); err != nil {
		t.Fatal(err)
	}
	body := []byte("temporary music")
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write(body)
	}))
	defer server.Close()
	d := c.download(1, "exhigh", &netease.Stream{URL: server.URL, Level: "exhigh", Type: "mp3", Size: int64(len(body))})
	defer d.Close()
	d.waitFor(math.MaxInt64)
	if err := d.Err(); err != nil {
		t.Fatal(err)
	}
	if got := d.peek(0, len(body)); !bytes.Equal(got, body) {
		t.Fatalf("could not play temporary audio: %q", got)
	}
	if got := c.Size(); got != 0 {
		t.Fatalf("unpublished audio counted as cached: %d bytes", got)
	}
	d.Close()
	<-d.finished
	if entries, err := os.ReadDir(c.dir); err != nil || len(entries) != 1 || entries[0].Name() != key+".json" {
		t.Fatalf("temporary audio remained after close: %v (%v)", entries, err)
	}
}

// Pruning obsolete audio files used to delete the sidecar of the surviving copy.
func TestCachePruningPreservesReplacementMetadata(t *testing.T) {
	c := NewCache(t.TempDir(), 1<<20)
	for _, current := range []string{"1-exhigh.mp3", "1-exhigh-3.mp3"} {
		t.Run(current, func(t *testing.T) {
			c.Clear()
			for i, name := range []string{"1-exhigh.mp3", "1-exhigh-2.mp3", "1-exhigh-3.mp3"} {
				if err := os.WriteFile(filepath.Join(c.dir, name), []byte(name), 0o600); err != nil {
					t.Fatal(err)
				}
				at := time.Unix(1+int64(i), 0)
				if name == current {
					at = time.Unix(10, 0)
				}
				if err := os.Chtimes(filepath.Join(c.dir, name), at, at); err != nil {
					t.Fatal(err)
				}
			}
			meta := keptSong{Stream: netease.Stream{Level: "exhigh", Type: "mp3", Size: int64(len(current))}}
			if current != "1-exhigh.mp3" {
				meta.File = current
			}
			b, err := json.Marshal(meta)
			if err != nil {
				t.Fatal(err)
			}
			sidecar := filepath.Join(c.dir, "1-exhigh.json")
			if err := os.WriteFile(sidecar, b, 0o600); err != nil {
				t.Fatal(err)
			}
			c.SetLimit(meta.Size)
			d, _, err := c.lookupOffline(1, "exhigh")
			if err != nil {
				t.Fatal("surviving copy went missing from offline cache", err)
			}
			if got := d.peek(0, len(current)); string(got) != current {
				t.Errorf("offline returned %q, want %q", got, current)
			}
			d.Close()
			c.Clear()
			if _, err := os.Stat(sidecar); !os.IsNotExist(err) {
				t.Fatalf("evicted current copy retained sidecar: %v", err)
			}
		})
	}
}

// An uncached response used to retain every byte on the heap, including bytes needed for backward seeks.
func TestUncachedDownloadSpillsAndPreservesBackwardReads(t *testing.T) {
	body := bytes.Repeat([]byte("0123456789abcdefghijklmnopqrstuvwxyz"), downloadMemoryLimit/32)
	for _, size := range []int64{int64(len(body)), 0} {
		t.Run(strconv.FormatInt(size, 10), func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if size == 0 {
					w.(http.Flusher).Flush()
				}
				w.Write(body)
			}))
			defer server.Close()
			c := NewCache(filepath.Join(t.TempDir(), "disabled"), 0)
			d := c.download(1, "exhigh", &netease.Stream{URL: server.URL, Level: "exhigh", Type: "mp3", Size: size})
			defer d.Close()
			if n := d.waitFor(math.MaxInt64); n != int64(len(body)) || d.Err() != nil {
				t.Fatalf("downloaded %d bytes: %v", n, d.Err())
			}
			d.mu.Lock()
			file, pages := d.part, len(d.pages)
			d.mu.Unlock()
			if file == "" || pages != 0 {
				t.Fatalf("uncached download retained %d memory pages and file %q", pages, file)
			}
			for _, off := range []int{len(body) - 100, 0, downloadMemoryLimit - 113} {
				if got := d.peek(int64(off), 100); !bytes.Equal(got, body[off:off+100]) {
					t.Fatalf("audio range at %d changed after spilling", off)
				}
			}
			if _, err := os.Stat(c.dir); !os.IsNotExist(err) {
				t.Fatalf("disabled cache directory was touched: %v", err)
			}
			d.Close()
			<-d.finished
			if _, err := os.Stat(file); !os.IsNotExist(err) {
				t.Fatalf("temporary audio survived close: %v", err)
			}
		})
	}
}

// Restoring a paused session waits for Play before asking for uncached audio.
func TestPausedRestoreDefersUncachedAudio(t *testing.T) {
	body := song(t)
	requests := make(chan struct{}, 2)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests <- struct{}{}
		w.Write(body)
	}))
	defer server.Close()
	old := songURL
	var urls atomic.Int32
	songURL = func(int64, string) (*netease.Stream, error) {
		urls.Add(1)
		return &netease.Stream{URL: server.URL, Level: "exhigh", Type: "mp3", Size: int64(len(body))}, nil
	}
	defer func() { songURL = old }()
	posts := make(chan func(), 32)
	p := New(func(fn func()) { posts <- fn })
	p.Cache = NewCache(t.TempDir(), 1<<30)
	defer p.stop()
	position := 250 * time.Millisecond
	p.Restore(Session{
		Queue: []netease.Track{{ID: 1, Duration: 10000}},
		Order: []int{0}, Position: position.Milliseconds(),
	})
	for p.Loading() {
		next(t, posts)
	}
	if urls.Load() != 0 || len(requests) != 0 || p.Playing() || p.Position() != position {
		t.Fatal("restoring paused started streaming or lost the saved position")
	}
	p.TogglePlay()
	waitCachePlayer(t, p, posts)
	if urls.Load() != 1 || len(requests) != 1 || !p.Playing() || p.Position() < position {
		t.Fatalf("Play did not stream from the saved position: URLs=%d audio=%d position=%v", urls.Load(), len(requests), p.Position())
	}
}

// A failed close leaves a temporary download unpublished.
func TestDownloadCloseFailurePreventsPublication(t *testing.T) {
	part, err := os.CreateTemp(t.TempDir(), "*.part")
	if err != nil {
		t.Fatal(err)
	}
	if err := part.Close(); err != nil {
		t.Fatal(err)
	}
	d := newDownload("unused", 4, part, func(path string, n int64) (string, bool) {
		t.Fatal("published audio after close failed")
		return path, true
	})
	d.n = 4
	if err := d.hand(); err == nil {
		t.Fatal("ignored the audio close failure")
	}
	d.cleanupLocked()
	if _, err := os.Stat(part.Name()); !os.IsNotExist(err) {
		t.Fatalf("failed download was left behind: %v", err)
	}
}

// Invalidating a corrupt alternate copy also invalidates its shared sidecar.
func TestDiscardCorruptAlternateAudio(t *testing.T) {
	c := NewCache(t.TempDir(), 1<<20)
	name := "1-exhigh-123.mp3"
	audio := filepath.Join(c.dir, name)
	if err := os.WriteFile(audio, []byte("invalid"), 0o600); err != nil {
		t.Fatal(err)
	}
	meta := []byte(`{"level":"exhigh","type":"mp3","size":7,"file":"1-exhigh-123.mp3"}`)
	sidecar := filepath.Join(c.dir, "1-exhigh.json")
	if err := os.WriteFile(sidecar, meta, 0o600); err != nil {
		t.Fatal(err)
	}
	d, _, err := c.lookupOffline(1, "exhigh")
	if err != nil {
		t.Fatal(err)
	}
	defer d.Close()
	d.discard()
	if _, err := os.Stat(sidecar); !os.IsNotExist(err) {
		t.Fatalf("corrupt audio remained published: %v", err)
	}
	if copy, _, err := c.lookupOffline(1, "exhigh"); err == nil {
		copy.Close()
		t.Fatal("offline playback reopened the corrupt audio")
	}
}
