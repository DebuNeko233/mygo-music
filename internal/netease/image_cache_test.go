package netease

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"image"
	"image/color"
	"image/png"
	"io"
	"net/http"
	"net/http/httptest"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"netease-music-mygo/internal/diskcache"
)

func coverBytes(t *testing.T) []byte {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, 2, 2))
	img.Set(0, 0, color.White)
	var b bytes.Buffer
	if err := png.Encode(&b, img); err != nil {
		t.Fatal(err)
	}
	return b.Bytes()
}
func imageFixture(t *testing.T, handler http.HandlerFunc) (*httptest.Server, *diskcache.Store) {
	t.Helper()
	server := httptest.NewServer(handler)
	client, cache := httpClient, images.Load()
	httpClient = server.Client()
	store := diskcache.New(t.TempDir(), 1<<20)
	UseImageCache(store, nil)
	t.Cleanup(func() { server.Close(); httpClient = client; images.Store(cache) })
	return server, store
}

// Concurrent covers share a download and remain readable after process restart.
func TestImageCacheCoalescesAndSurvivesRestart(t *testing.T) {
	body := coverBytes(t)
	var count atomic.Int32
	entered, release := make(chan struct{}, 1), make(chan struct{})
	server, store := imageFixture(t, func(w http.ResponseWriter, r *http.Request) {
		count.Add(1)
		entered <- struct{}{}
		<-release
		w.Header().Set("Cache-Control", "max-age=3600")
		w.Write(body)
	})
	var group sync.WaitGroup
	results := make(chan error, 12)
	for range 12 {
		group.Add(1)
		go func() {
			defer group.Done()
			b, err := Fetch(server.URL + "/cover?param=80y80")
			if err == nil && !bytes.Equal(b, body) {
				t.Error("wrong cover")
			}
			results <- err
		}()
	}
	<-entered
	close(release)
	group.Wait()
	close(results)
	for err := range results {
		if err != nil {
			t.Fatal(err)
		}
	}
	if count.Load() != 1 {
		t.Fatalf("downloaded %d times", count.Load())
	}
	UseImageCache(store, nil)
	b, err := Fetch(server.URL + "/cover?param=80y80")
	if err != nil || !bytes.Equal(b, body) || count.Load() != 1 {
		t.Fatal("restart fetched an already cached cover")
	}
}

// A stale public cover displays immediately and 304 revalidates its existing bytes.
func TestImageCacheStaleThenConditionalRequest(t *testing.T) {
	body := coverBytes(t)
	entered, release := make(chan struct{}), make(chan struct{})
	server, store := imageFixture(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("If-None-Match") != "old" {
			t.Error("missing validator")
		}
		close(entered)
		<-release
		w.Header().Set("Cache-Control", "max-age=3600")
		w.WriteHeader(304)
	})
	url := server.URL + "/cover"
	req, _ := http.NewRequest("GET", url, nil)
	req.Header.Set("User-Agent", UserAgent)
	sum := sha256.Sum256(body)
	content := hex.EncodeToString(sum[:])
	store.Put("content", content, body, 0)
	record := imageRecord{Content: content, ETag: "old", Expires: time.Unix(1, 0)}
	b, _ := json.Marshal(record)
	store.Put("requests", imageIdentity(req), b, 0)
	b, err := Fetch(url)
	if err != nil || !bytes.Equal(b, body) {
		t.Fatal("did not serve stale cover immediately")
	}
	<-entered
	c := images.Load()
	c.mu.Lock()
	flight := c.flights[imageIdentity(req)]
	c.mu.Unlock()
	close(release)
	<-flight.done
	b, _ = store.Read("requests", imageIdentity(req))
	json.Unmarshal(b, &record)
	if record.Expires.Before(time.Now()) || record.Content != content {
		t.Fatal("304 did not preserve and freshen content")
	}
}

// Explicit HTTP prohibitions never become persistent image cache entries.
func TestImageCacheHonorsHTTPPolicy(t *testing.T) {
	for _, policy := range []string{"no-store", "private", "vary"} {
		t.Run(policy, func(t *testing.T) {
			body := coverBytes(t)
			var calls atomic.Int32
			server, store := imageFixture(t, func(w http.ResponseWriter, r *http.Request) {
				calls.Add(1)
				if policy == "vary" {
					w.Header().Set("Vary", "*")
				} else {
					w.Header().Set("Cache-Control", policy)
				}
				w.Write(body)
			})
			for range 2 {
				if _, err := Fetch(server.URL); err != nil {
					t.Fatal(err)
				}
			}
			if calls.Load() != 2 || store.Size() != 0 {
				t.Fatal("persisted a prohibited response")
			}
		})
	}
}

// Different complete URLs remain distinct requests, while equal image bytes share storage.
func TestImageCachePreservesURLAndDeduplicatesContent(t *testing.T) {
	body := coverBytes(t)
	var calls atomic.Int32
	server, store := imageFixture(t, func(w http.ResponseWriter, r *http.Request) { calls.Add(1); w.Write(body) })
	for _, query := range []string{"?param=80y80&v=1", "?param=600y600&v=2"} {
		if _, err := Fetch(server.URL + query); err != nil {
			t.Fatal(err)
		}
	}
	if calls.Load() != 2 {
		t.Fatal("collapsed distinct URL parameters")
	}
	sum := sha256.Sum256(body)
	b, _ := store.Read("content", hex.EncodeToString(sum[:]))
	if !bytes.Equal(b, body) {
		t.Fatal("missing shared content")
	}
}

// Clearing while an HTTP request is in flight prevents its response from refilling disk.
func TestImageCacheClearDuringDownload(t *testing.T) {
	body := coverBytes(t)
	entered, release := make(chan struct{}), make(chan struct{})
	server, store := imageFixture(t, func(w http.ResponseWriter, r *http.Request) { close(entered); <-release; w.Write(body) })
	done := make(chan error, 1)
	go func() { _, err := Fetch(server.URL); done <- err }()
	<-entered
	cleared := make(chan struct{})
	store.Clear("", func() { close(cleared) })
	<-cleared
	close(release)
	if err := <-done; err != nil {
		t.Fatal(err)
	}
	if store.Size() != 0 {
		t.Fatal("download undid clear")
	}
}

// A known CDN's larger response serves small views, but never the reverse;
// adding unrelated query parameters disables this explicit size relationship.
func TestImageCacheCDNSizeReuse(t *testing.T) {
	client, previous := httpClient, images.Load()
	store := diskcache.New(t.TempDir(), 8<<20)
	UseImageCache(store, nil)
	t.Cleanup(func() { httpClient = client; images.Store(previous); store.Close() })
	calls := 0
	httpClient = &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
		calls++
		size := 80
		if r.URL.Query().Get("param") == "600y600" {
			size = 600
		}
		var b bytes.Buffer
		png.Encode(&b, image.NewRGBA(image.Rect(0, 0, size, size)))
		return &http.Response{StatusCode: 200, Header: http.Header{"Cache-Control": []string{"max-age=3600"}}, Body: io.NopCloser(bytes.NewReader(b.Bytes()))}, nil
	})}
	for _, query := range []string{"?param=600y600", "?param=80y80"} {
		if _, err := Fetch("https://p1.music.126.net/large.jpg" + query); err != nil {
			t.Fatal(err)
		}
	}
	if calls != 1 {
		t.Fatal("redownloaded small view of existing large cover")
	}
	for _, query := range []string{"?param=80y80", "?param=600y600"} {
		if _, err := Fetch("https://p1.music.126.net/small.jpg" + query); err != nil {
			t.Fatal(err)
		}
	}
	if calls != 3 {
		t.Fatal("upscaled a small cached response")
	}
	if _, err := Fetch("https://p1.music.126.net/large.jpg?param=80y80&version=2"); err != nil {
		t.Fatal(err)
	}
	if calls != 4 {
		t.Fatal("ignored version query")
	}
}

// no-cache requires validation even when max-age follows it, and must-revalidate
// cannot serve an expired cover when the connection fails.
func TestImageCacheRequiredValidation(t *testing.T) {
	for _, policy := range []string{"no-cache, max-age=3600", "max-age=0, must-revalidate"} {
		t.Run(policy, func(t *testing.T) {
			body := coverBytes(t)
			calls := 0
			server, _ := imageFixture(t, func(w http.ResponseWriter, r *http.Request) {
				calls++
				if calls > 1 {
					w.WriteHeader(503)
					return
				}
				w.Header().Set("Cache-Control", policy)
				w.Write(body)
			})
			if _, err := Fetch(server.URL); err != nil {
				t.Fatal(err)
			}
			if _, err := Fetch(server.URL); err == nil {
				t.Fatal("served a cover requiring successful validation")
			}
		})
	}
}
