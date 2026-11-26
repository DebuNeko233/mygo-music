/*
验证打包下载失败后的清理与重试，并在部分平台更新时保留其他清单内容。
*/

package main

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"strings"
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
			dir := filepath.Join(cache, "nyago-music", "sherpa-onnx", checksum)
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

// Packing a platform subset retains other platforms and replaces repeated entries for a selected platform.
func TestWriteManifestPlatformSubsetRetainsExistingPlatforms(t *testing.T) {
	file := filepath.Join(t.TempDir(), "natives.go")
	fileFor := func(platform string) packed {
		return packed{name: platform + ".so", asset: platform + ".so.gz", size: 100, gzSize: 50, sha256: platform + "-sha", gzSHA256: platform + "-gzsha"}
	}
	vad := packed{name: "silero_vad.onnx", asset: "silero_vad.onnx.gz", size: 1, gzSize: 1, sha256: "vad-sha", gzSHA256: "vad-gzsha"}
	if err := writeManifest(file, []string{"darwin-arm64", "linux-amd64"}, map[string][]packed{
		"darwin-arm64": {fileFor("darwin-arm64")},
		"linux-amd64":  {fileFor("linux-amd64")},
	}, vad); err != nil {
		t.Fatal(err)
	}
	if err := writeManifest(file, []string{"linux-amd64"}, map[string][]packed{
		"linux-amd64": {fileFor("linux-amd64")},
	}, vad); err != nil {
		t.Fatal(err)
	}
	b, err := os.ReadFile(file)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{`"darwin-arm64": {`, `name: "darwin-arm64.so"`, `sha256: "darwin-arm64-sha"`} {
		if !strings.Contains(string(b), want) {
			t.Errorf("subset manifest lost %s:\n%s", want, b)
		}
	}
	if got := strings.Count(string(b), `"linux-amd64": {`); got != 1 {
		t.Errorf("manifest has %d linux-amd64 entries, want 1:\n%s", got, b)
	}

	updated := fileFor("linux-amd64")
	updated.sha256 = "updated-sha"
	if err := writeManifest(file, []string{"linux-amd64"}, map[string][]packed{"linux-amd64": {updated}}, vad); err != nil {
		t.Fatal(err)
	}
	b, err = os.ReadFile(file)
	if err != nil {
		t.Fatal(err)
	}
	for _, check := range []struct {
		value string
		want  int
	}{
		{`"darwin-arm64": {`, 1},
		{`"linux-amd64": {`, 1},
		{`sha256: "updated-sha"`, 1},
		{`sha256: "linux-amd64-sha"`, 0},
	} {
		if got := strings.Count(string(b), check.value); got != check.want {
			t.Errorf("manifest has %s %d times, want %d:\n%s", check.value, got, check.want, b)
		}
	}
}

// Platform subset packing preserves the other native declarations outside the generated manifest.
func TestPlatformSubsetRetainsOtherNativeDeclarations(t *testing.T) {
	dir := t.TempDir()
	file := filepath.Join(dir, "natives.go")
	const other = `var runtimeGOOS = "native go other platforms"
`
	if err := os.WriteFile(file, []byte("package speech\n\n"+fmt.Sprintf("var libFiles = map[string][]nativeFile{\n%q: {{name: %q}},\n%q: {{name: %q}},\n}\n", "darwin-arm64", "darwin.so", "linux-amd64", "old.so")+"\n"+other), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := writeManifest(file, []string{"linux-amd64"}, map[string][]packed{
		"linux-amd64": {{name: "new.so", asset: "new.so.gz", size: 2, gzSize: 1, sha256: "new-sha", gzSHA256: "new-gzsha"}},
	}, packed{name: "vad.onnx"}); err != nil {
		t.Fatal(err)
	}
	b, err := os.ReadFile(file)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(b), other) {
		t.Errorf("manifest rewrite removed other native declarations:\n%s", b)
	}
	if strings.Contains(string(b), "old.so") || !strings.Contains(string(b), "new.so") {
		t.Errorf("selected platform was not replaced:\n%s", b)
	}
}
