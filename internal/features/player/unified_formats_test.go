/*
验证各音频格式在本地、网络和离线缓存中的解码与跳转一致性。
*/

package player

import (
	"bytes"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"math"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"netease-music-mygo/internal/features/netease"
)

type formatFixture struct {
	name, kind string
	data       []byte
}

func supportedAudioFixtures(t *testing.T) []formatFixture {
	t.Helper()
	fixtures := []formatFixture{
		{"wav-pcm", "wav", makeWAV(1, 16, 8000, func(frame int) int16 { return int16(frame*97%30000 - 15000) })},
		{"wav-float", "wav", makeWAV(3, 32, 8000, func(frame int) int16 { return int16(frame*97%30000 - 15000) })},
	}
	for _, f := range []struct{ name, kind string }{
		{"seek.mp3", "mp3"}, {"seek.flac", "flac"}, {"seek.ogg", "ogg"}, {"seek.aac", "aac"},
		{"seek-he.aac", "aac"}, {"seek.m4a", "m4a"}, {"seek-alac.m4a", "m4a"}, {"seek-he.m4a", "m4a"}, {"seek.3gp", "m4a"}, {"seek.3g2", "m4a"},
	} {
		fixtures = append(fixtures, formatFixture{f.name, f.kind, testAudio(t, f.name)})
	}
	return fixtures
}

func readAudioPCM(t *testing.T, s stream) []float32 {
	t.Helper()
	var pcm []float32
	buf := make([]float32, 4096)
	for {
		n, err := s.Read(buf)
		pcm = append(pcm, buf[:n]...)
		if err != nil {
			if !errors.Is(err, io.EOF) {
				t.Fatal(err)
			}
			break
		}
		if n == 0 {
			t.Fatal("decoder made no progress")
		}
	}
	if err := s.cut(); err != nil {
		t.Fatal(err)
	}
	return pcm
}

// Every supported format decodes the same PCM from local files, HTTP memory/disk buffers and offline cache, including seeks and fade-reader ownership.
func TestUnifiedFormatsNetworkCacheAndSeek(t *testing.T) {
	for _, fixture := range supportedAudioFixtures(t) {
		t.Run(fixture.name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "audio.bin")
			if err := os.WriteFile(path, fixture.data, 0600); err != nil {
				t.Fatal(err)
			}
			info, local, err := openLocal(path)
			if err != nil {
				t.Fatal(err)
			}
			want := readAudioPCM(t, local)
			local.Close()
			if info.Type != fixture.kind || len(want) == 0 {
				t.Fatalf("local type %s, samples %d", info.Type, len(want))
			}
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				http.ServeContent(w, r, "audio", time.Time{}, bytes.NewReader(fixture.data))
			}))
			defer srv.Close()
			for _, retain := range []bool{false, true} {
				t.Run(fmt.Sprintf("cache=%v", retain), func(t *testing.T) {
					limit := int64(0)
					if retain {
						limit = 16 << 20
					}
					c := NewCache(t.TempDir(), limit)
					// Deliberately misleading metadata: container bytes choose the decoder and cache extension.
					dl := c.download(1, "exhigh", &netease.Stream{URL: srv.URL, Size: int64(len(fixture.data)), Type: "mp3", Level: "exhigh"})
					defer dl.Close()
					s, err := openStream(dl, 5000)
					if err != nil {
						t.Fatal(err)
					}
					shared, err := openShared(dl, 5000)
					if err != nil {
						t.Fatal(err)
					}
					if _, spec := shared.source(); spec != nil {
						t.Fatal("fade-only reader measured a second spectrum")
					}
					buf := make([]float32, 512)
					n, err := shared.Read(buf)
					if n == 0 || err != nil || maxDiff(buf[:n], want[:n]) > 1e-6 {
						t.Fatalf("shared decoding mismatch: %d %v", n, err)
					}
					shared.Close()
					shared.Close()
					got := readAudioPCM(t, s)
					if len(got) != len(want) || maxDiff(got, want) > 1e-6 {
						t.Fatalf("network PCM differs: %d/%d samples", len(got), len(want))
					}
					dl.waitFor(math.MaxInt64)
					if _, spec := s.source(); spec == nil {
						t.Fatal("primary reader lost spectrum")
					}
					s.Close()
					if !retain {
						if c.Size() != 0 {
							t.Fatal("disabled cache retained audio")
						}
						return
					}
					kept, meta, err := c.lookupOffline(1, "exhigh")
					if err != nil {
						t.Fatal(err)
					}
					if meta.Type != fixture.kind || c.Size() != int64(len(fixture.data)) {
						t.Fatalf("cache type/size: %s/%d", meta.Type, c.Size())
					}
					replay, err := openStream(kept, 5000)
					if err != nil {
						kept.Close()
						t.Fatal(err)
					}
					defer replay.Close()
					if fixture.kind != "mp3" {
						const frame = 101
						if err := replay.SetPosition(frame); err != nil {
							t.Fatal(err)
						}
						n, err := replay.Read(buf)
						if n == 0 || err != nil || maxDiff(buf[:n], want[frame*2:frame*2+n]) > 1e-6 {
							t.Fatalf("offline seek differs: %d %v", n, err)
						}
					}
					replay.Close()
					c.Clear()
					if c.Size() != 0 {
						t.Fatal("clear ignored a supported audio format")
					}
				})
			}
		})
	}
}

// A WAV starts from its prefix and a later seek reuses HTTP Range instead of decoding sparse-file zeros or waiting for a full download.
func TestUnifiedWAVStartsAndSeeksBeforeDownloadCompletes(t *testing.T) {
	seed := makeWAV(1, 16, 8000, func(frame int) int16 { return int16(frame*97%30000 - 15000) })
	data := append(bytes.Clone(seed[:44]), bytes.Repeat(seed[44:], 512)...)
	binary.LittleEndian.PutUint32(data[4:8], uint32(len(data)-8))
	binary.LittleEndian.PutUint32(data[40:44], uint32(len(data)-44))
	for _, mode := range []struct{ unknown, chunked bool }{{false, false}, {true, false}, {true, true}} {
		t.Run(fmt.Sprintf("unknown-api=%v/chunked=%v", mode.unknown, mode.chunked), func(t *testing.T) {
			ranges := make(chan string, 16)
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.Header.Get("Range") == "" {
					if !mode.chunked {
						w.Header().Set("Content-Length", fmt.Sprint(len(data)))
					}
					w.Write(data[:64])
					w.(http.Flusher).Flush()
					<-r.Context().Done()
					return
				}
				select {
				case ranges <- r.Header.Get("Range"):
				default:
				}
				http.ServeContent(w, r, "audio.wav", time.Time{}, bytes.NewReader(data))
			}))
			defer srv.Close()
			size := int64(len(data))
			if mode.unknown {
				size = -1
			}
			dl := startDownload(srv.URL, size, nil, nil)
			defer dl.Close()
			type result struct {
				s   stream
				err error
			}
			opened := make(chan result, 1)
			go func() {
				s, err := openStream(dl, 0)
				if err == nil {
					buf := make([]float32, 4)
					_, err = s.Read(buf)
				}
				opened <- result{s, err}
			}()
			var s stream
			select {
			case got := <-opened:
				if got.err != nil {
					t.Fatal(got.err)
				}
				s = got.s
			case <-time.After(5 * time.Second):
				t.Fatal("WAV waited for the complete download")
			}
			defer s.Close()
			if dl.Progress() >= 1 {
				t.Fatal("fixture completed before playback began")
			}
			const frame = 200101
			if err := s.SetPosition(frame); err != nil {
				t.Fatal(err)
			}
			read := make(chan error, 1)
			go func() {
				buf := make([]float32, 512)
				n, err := s.Read(buf)
				if err == nil && n > 0 {
					v := int16((frame%1000)*97%30000 - 15000)
					if buf[0] != float32(v)/(1<<15) {
						err = fmt.Errorf("seek sample %v, want %v", buf[0], float32(v)/(1<<15))
					}
				} else if err == nil {
					err = io.ErrNoProgress
				}
				read <- err
			}()
			select {
			case err := <-read:
				if err != nil {
					t.Fatal(err)
				}
			case <-time.After(5 * time.Second):
				t.Fatal("WAV seek did not request its missing byte range")
			}
			select {
			case <-ranges:
			default:
				t.Fatal("seek did not reuse the HTTP Range downloader")
			}
		})
	}
}

// Tail-indexed M4A metadata uses Range before playback; waiting for that metadata never holds the codec lock needed by another song.
func TestUnifiedMP4TailMetadataDoesNotBlockOtherSongs(t *testing.T) {
	original := testAudio(t, "seek.m4a")
	moov := -1
	for off := 0; off+8 <= len(original); {
		size := int(binary.BigEndian.Uint32(original[off : off+4]))
		if size < 8 || off+size > len(original) {
			t.Fatal("invalid MP4 fixture")
		}
		if string(original[off+4:off+8]) == "moov" {
			moov = off
			break
		}
		off += size
	}
	if moov < 0 {
		t.Fatal("missing movie metadata")
	}
	const gap = 1 << 20
	free := make([]byte, gap)
	binary.BigEndian.PutUint32(free[:4], gap)
	copy(free[4:8], "free")
	data := append(bytes.Clone(original[:moov]), free...)
	data = append(data, original[moov:]...)
	prefix, tail := moov+8, moov+gap
	tailAsked := make(chan struct{}, 1)
	releaseTail := make(chan struct{})
	release := sync.OnceFunc(func() { close(releaseTail) })
	defer release()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		raw := r.Header.Get("Range")
		if raw == "" {
			w.Header().Set("Content-Length", fmt.Sprint(len(data)))
			w.Write(data[:prefix])
			w.(http.Flusher).Flush()
			<-r.Context().Done()
			return
		}
		if strings.HasPrefix(raw, fmt.Sprintf("bytes=%d-", tail)) {
			select {
			case tailAsked <- struct{}{}:
			default:
			}
			select {
			case <-releaseTail:
			case <-r.Context().Done():
				return
			}
		}
		if strings.HasPrefix(raw, fmt.Sprintf("bytes=%d-", prefix)) {
			w.Header().Set("Content-Range", fmt.Sprintf("bytes %d-%d/%d", prefix, tail-1, len(data)))
			w.Header().Set("Content-Length", fmt.Sprint(tail-prefix))
			w.WriteHeader(http.StatusPartialContent)
			w.Write(data[prefix : prefix+128])
			w.(http.Flusher).Flush()
			<-r.Context().Done()
			return
		}
		http.ServeContent(w, r, "audio.m4a", time.Time{}, bytes.NewReader(data))
	}))
	defer srv.Close()
	dl := startDownload(srv.URL, int64(len(data)), nil, nil)
	defer dl.Close()
	type result struct {
		s   stream
		err error
	}
	opened := make(chan result, 1)
	go func() {
		s, err := openStream(dl, 0)
		if err == nil {
			buf := make([]float32, 512)
			_, err = s.Read(buf)
		}
		opened <- result{s, err}
	}()
	select {
	case <-tailAsked:
	case <-time.After(5 * time.Second):
		t.Fatal("MP4 did not request tail metadata")
	}
	path := filepath.Join(t.TempDir(), "another.m4a")
	if err := os.WriteFile(path, original, 0600); err != nil {
		t.Fatal(err)
	}
	other := make(chan error, 1)
	go func() {
		_, s, err := openLocal(path)
		if s != nil {
			s.Close()
		}
		other <- err
	}()
	select {
	case err := <-other:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("network metadata blocked another song's codec")
	}
	release()
	select {
	case got := <-opened:
		if got.err != nil {
			t.Fatal(got.err)
		}
		defer got.s.Close()
	case <-time.After(5 * time.Second):
		t.Fatal("MP4 waited for the entire padded file")
	}
	if dl.Progress() >= 1 {
		t.Fatal("MP4 downloaded unused padding before playback")
	}
}
