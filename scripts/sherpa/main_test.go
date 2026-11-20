package main

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"sync/atomic"
	"testing"
)

// Failed archive transfers and publications leave no partial files and a retry can populate the cache.
func TestFetchArchiveCleansUpFailureAndCachesRetry(t *testing.T) {
	payload := []byte("verified Sherpa archive")
	sum := sha256.Sum256(payload)
	checksum := hex.EncodeToString(sum[:])

	for _, failure := range []string{"truncated response", "digest mismatch", "blocked publication"} {
		t.Run(failure, func(t *testing.T) {
			cache := t.TempDir()
			dir := filepath.Join(cache, "mygo-cloudmusic", "sherpa-onnx", checksum)
			archive := filepath.Join(dir, "archive.tar.bz2")
			var requests atomic.Int32
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if requests.Add(1) == 1 {
					switch failure {
					case "truncated response":
						w.Header().Set("Content-Length", strconv.Itoa(len(payload)+1))
					case "digest mismatch":
						w.Write([]byte("wrong archive"))
						return
					}
				}
				w.Write(payload)
			}))
			defer server.Close()
			url := server.URL + "/archive.tar.bz2"

			var sentinel string
			if failure == "blocked publication" {
				if err := os.MkdirAll(archive, 0o700); err != nil {
					t.Fatal(err)
				}
				sentinel = filepath.Join(archive, "existing")
				if err := os.WriteFile(sentinel, []byte("preserve"), 0o600); err != nil {
					t.Fatal(err)
				}
			}
			if _, err := fetch(cache, url, checksum); err == nil {
				t.Fatal("accepted failed download")
			}
			entries, err := os.ReadDir(dir)
			if err != nil {
				t.Fatal(err)
			}
			if sentinel == "" {
				if len(entries) != 0 {
					t.Fatalf("failed download left cache files: %v", entries)
				}
			} else {
				if len(entries) != 1 || entries[0].Name() != filepath.Base(archive) {
					t.Fatalf("expected only the blocking directory, got %v", entries)
				}
				if b, err := os.ReadFile(sentinel); err != nil || string(b) != "preserve" {
					t.Fatalf("changed an existing file during publication: %q, %v", b, err)
				}
				if err := os.Remove(sentinel); err != nil {
					t.Fatal(err)
				}
				if err := os.Remove(archive); err != nil {
					t.Fatal(err)
				}
			}

			path, err := fetch(cache, url, checksum)
			if err != nil || path != archive {
				t.Fatalf("retry returned %q, %v", path, err)
			}
			if actual, err := os.ReadFile(path); err != nil || !bytes.Equal(actual, payload) {
				t.Fatalf("retry cached the wrong bytes: %q, %v", actual, err)
			}
			if path, err := fetch(cache, url, checksum); err != nil || path != archive {
				t.Fatalf("cache hit returned %q, %v", path, err)
			}
			if count := requests.Load(); count != 2 {
				t.Fatalf("got %d HTTP requests, want two attempts and a cache hit", count)
			}
		})
	}
}
