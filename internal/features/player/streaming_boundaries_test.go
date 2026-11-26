/*
验证 AAC 和 MP4 增量解码不阻塞其他歌曲，并正确处理取消和不完整帧。
*/

package player

import (
	"bytes"
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"testing/synctest"
	"time"

	faad2 "github.com/llehouerou/go-faad2"
)

// A fragmented M4A waiting for its first fragment must release the codec factory
// so another song can open; this fixture is MyGo's generated fragmented AAC tone.
func TestMP4FragmentWaitDoesNotBlockAnotherSong(t *testing.T) {
	data := testAudio(t, "seek-fragmented.m4a")
	prefix := 0
	for prefix+8 <= len(data) {
		n := int(binary.BigEndian.Uint32(data[prefix:]))
		if n < 8 || n > len(data)-prefix {
			t.Fatal("bad fixture box")
		}
		if string(data[prefix+4:prefix+8]) == "moof" {
			break
		}
		prefix += n
	}
	if prefix == len(data) {
		t.Fatal("fixture has no fragments")
	}
	path := filepath.Join(t.TempDir(), "another.m4a")
	if err := os.WriteFile(path, testAudio(t, "seek-he.m4a"), 0600); err != nil {
		t.Fatal(err)
	}
	synctest.Test(t, func(t *testing.T) {
		dl := downloaded(nil)
		dl.done = false
		dl.length = int64(len(data))
		dl.size = dl.length
		defer dl.Close()
		if err := dl.writeAt(data[:prefix], 0); err != nil {
			t.Fatal(err)
		}
		result := make(chan error, 1)
		go func() {
			s, err := openStream(dl, 0)
			if s != nil {
				s.Close()
			}
			result <- err
		}()
		synctest.Wait()
		select {
		case err := <-result:
			t.Fatalf("fragment open should wait: %v", err)
		default:
		}
		other := make(chan error, 1)
		go func() {
			_, s, err := openLocal(path)
			if s != nil {
				s.Close()
			}
			other <- err
		}()
		synctest.Wait()
		select {
		case err := <-other:
			if err != nil {
				t.Fatal(err)
			}
		default:
			// Unblock the first worker before failing, so test cleanup never leaks it.
			dl.Close()
			synctest.Wait()
			t.Fatal("missing fragment blocked another song's codec")
		}
		if err := dl.writeAt(data[prefix:], int64(prefix)); err != nil {
			t.Fatal(err)
		}
		dl.mu.Lock()
		dl.finishLocked(nil)
		dl.mu.Unlock()
		synctest.Wait()
		if err := <-result; err != nil {
			t.Fatal(err)
		}
	})
}

// AAC-LC and implicit HE-AAC start from a few HTTP packets, reuse that prefix
// after fallback, and decode the remaining body identically to local playback.
func TestAACStartsBeforeDownloadCompletes(t *testing.T) {
	for _, name := range []string{"seek.aac", "seek-he.aac"} {
		data := testAudio(t, name)
		prefix := 0
		for range 3 {
			n, _, ok := adtsPacket(data[prefix:])
			if !ok {
				t.Fatal("bad fixture")
			}
			prefix += n
		}
		path := filepath.Join(t.TempDir(), name)
		if err := os.WriteFile(path, data, 0600); err != nil {
			t.Fatal(err)
		}
		_, local, err := openLocal(path)
		if err != nil {
			t.Fatal(err)
		}
		want := readAudioPCM(t, local)
		local.Close()
		for _, mode := range []struct{ unknown, chunked bool }{{false, false}, {true, false}, {true, true}} {
			t.Run(fmt.Sprintf("%s/unknown-api=%v/chunked=%v", name, mode.unknown, mode.chunked), func(t *testing.T) {
				release := make(chan struct{})
				resume := sync.OnceFunc(func() { close(release) })
				defer resume()
				srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					if r.Header.Get("Range") != "" {
						http.ServeContent(w, r, "audio.aac", time.Time{}, bytes.NewReader(data))
						return
					}
					if !mode.chunked {
						w.Header().Set("Content-Length", fmt.Sprint(len(data)))
					}
					w.Write(data[:prefix])
					w.(http.Flusher).Flush()
					select {
					case <-release:
						w.Write(data[prefix:])
					case <-r.Context().Done():
					}
				}))
				defer srv.Close()
				size := int64(len(data))
				if mode.unknown {
					size = -1
				}
				dl := startDownload(srv.URL, size, nil, nil)
				defer dl.Close()
				type opened struct {
					s   stream
					pcm []float32
					err error
				}
				ready := make(chan opened, 1)
				go func() {
					s, err := openStream(dl, 5000)
					var pcm []float32
					if err == nil {
						pcm = make([]float32, 512)
						var n int
						n, err = s.Read(pcm)
						pcm = pcm[:n]
					}
					ready <- opened{s, pcm, err}
				}()
				var first opened
				select {
				case first = <-ready:
				case <-time.After(5 * time.Second):
					t.Fatal("AAC scanned the tail before playback")
				}
				if first.err != nil {
					t.Fatal(first.err)
				}
				defer first.s.Close()
				if len(first.pcm) == 0 || maxDiff(first.pcm, want[:len(first.pcm)]) != 0 {
					t.Fatal("early PCM differs from local playback")
				}
				if dl.Progress() >= 1 {
					t.Fatal("download finished before playback")
				}
				if first.s.Len() != -1 {
					t.Fatalf("partial index exposed length %d", first.s.Len())
				}
				resume()
				got := append(first.pcm, readAudioPCM(t, first.s)...)
				if len(got) != len(want) || maxDiff(got, want) != 0 {
					t.Fatalf("completed PCM differs: %d/%d", len(got), len(want))
				}
				if first.s.Len() != int64(len(want)/2) {
					t.Fatalf("completed length %d", first.s.Len())
				}
				const frame = 4097
				if err := first.s.SetPosition(frame); err != nil {
					t.Fatal(err)
				}
				buf := make([]float32, 512)
				n, err := first.s.Read(buf)
				if n == 0 || err != nil {
					t.Fatalf("seek after completion: %d %v", n, err)
				}
				// Compare with a separately opened local decoder at the same
				// position: HE-AAC's SBR history differs from uninterrupted play.
				_, reference, err := openLocal(path)
				if err != nil {
					t.Fatal(err)
				}
				defer reference.Close()
				if err := reference.SetPosition(frame); err != nil {
					t.Fatal(err)
				}
				expected := make([]float32, len(buf))
				m, err := reference.Read(expected)
				if err != nil || m != n || maxDiff(buf[:n], expected[:m]) != 0 {
					t.Fatal("incremental seek differs from complete-file seek")
				}
			})
		}
	}
}

// Cancellation wakes an incremental AAC header read instead of returning a
// shortened index as a successful end-of-file, and closing a shared reader keeps its owner alive.
func TestAACIncrementalReadCancellation(t *testing.T) {
	data := testAudio(t, "seek-he.aac")
	prefix := 0
	for range 3 {
		n, _, _ := adtsPacket(data[prefix:])
		prefix += n
	}
	synctest.Test(t, func(t *testing.T) {
		dl := downloaded(nil)
		dl.done = false
		dl.length = int64(len(data))
		dl.size = dl.length
		defer dl.Close()
		if err := dl.writeAt(data[:prefix], 0); err != nil {
			t.Fatal(err)
		}
		s, err := openStream(dl, 0)
		if err != nil {
			t.Fatal(err)
		}
		defer s.Close()
		shared, err := openShared(dl, 0)
		if err != nil {
			t.Fatal(err)
		}
		shared.Close()
		if dl.closed {
			t.Fatal("shared reader cancelled its primary")
		}
		result := make(chan error, 1)
		go func() {
			buf := make([]float32, 4096)
			for {
				_, err := s.Read(buf)
				if err != nil {
					result <- err
					return
				}
			}
		}()
		synctest.Wait()
		select {
		case err := <-result:
			t.Fatalf("read ended before cancellation: %v", err)
		default:
		}
		dl.Close()
		synctest.Wait()
		if err := <-result; !errors.Is(err, context.Canceled) {
			t.Fatalf("cancelled read: %v", err)
		}
		if !errors.Is(s.cut(), context.Canceled) {
			t.Fatalf("cut did not preserve cancellation: %v", s.cut())
		}
	})
}

// A malformed CRC-protected packet cannot allocate a negative payload buffer.
func TestADTSRejectsShortCRCFrame(t *testing.T) {
	h := []byte{0xff, 0xf0, 0x50, 0x80, 0, 0xe0, 0} // length 7, but CRC requires a 9-byte header
	if _, _, ok := adtsPacket(h); ok {
		t.Fatal("short CRC frame accepted")
	}
}

// Seeking an in-flight AAC extends the same index only to the target packets,
// without scanning the tail, and produces the same seek output as a complete file.
func TestAACSeekBuildsOnlyRequiredIndex(t *testing.T) {
	for _, name := range []string{"seek.aac", "seek-he.aac"} {
		t.Run(name, func(t *testing.T) {
			data := testAudio(t, name)
			ends := []int{0}
			for at := 0; at+7 <= len(data); {
				n, _, ok := adtsPacket(data[at:])
				if !ok {
					t.Fatal("invalid fixture")
				}
				at += n
				ends = append(ends, at)
			}
			path := filepath.Join(t.TempDir(), name)
			if err := os.WriteFile(path, data, 0600); err != nil {
				t.Fatal(err)
			}
			_, reference, err := openLocal(path)
			if err != nil {
				t.Fatal(err)
			}
			defer reference.Close()
			synctest.Test(t, func(t *testing.T) {
				dl := downloaded(nil)
				dl.done = false
				dl.length = int64(len(data))
				dl.size = dl.length
				defer dl.Close()
				if err := dl.writeAt(data[:ends[2]], 0); err != nil {
					t.Fatal(err)
				}
				s, err := openAACStreamSpectrum(dl, true)
				if err != nil {
					t.Fatal(err)
				}
				defer s.Close()
				frame := s.packetFrames*4 + 17
				if err := reference.SetPosition(frame); err != nil {
					t.Fatal(err)
				}
				want := make([]float32, 512)
				m, err := reference.Read(want)
				if err != nil {
					t.Fatal(err)
				}
				result := make(chan error, 1)
				go func() {
					if err := s.SetPosition(frame); err != nil {
						result <- err
						return
					}
					got := make([]float32, 512)
					n, err := s.Read(got)
					if err == nil && (n != m || maxDiff(got[:n], want[:m]) != 0) {
						err = errors.New("incremental seek differs")
					}
					result <- err
				}()
				synctest.Wait()
				select {
				case err := <-result:
					t.Fatalf("seek did not wait for missing target: %v", err)
				default:
				}
				if err := dl.writeAt(data[ends[2]:ends[7]], int64(ends[2])); err != nil {
					t.Fatal(err)
				}
				synctest.Wait()
				select {
				case err := <-result:
					if err != nil {
						t.Fatal(err)
					}
				default:
					dl.Close()
					synctest.Wait()
					t.Fatal("seek waited for unrelated tail packets")
				}
				if len(s.offs) > 6 || s.Len() != -1 {
					t.Fatalf("seek indexed unrelated tail: %d packets, length %d", len(s.offs), s.Len())
				}
			})
		})
	}
}

// Cancelling HE-AAC initialization while its priming packet waits for more bytes
// releases the fallback decoder rather than retaining a failed-open WASM instance.
func TestAACOpenCancellationClosesFallback(t *testing.T) {
	data := testAudio(t, "seek-he.aac")
	prefix, _, _ := adtsPacket(data)
	synctest.Test(t, func(t *testing.T) {
		factory := newADTSDecoder
		var decoder *faad2.Decoder
		newADTSDecoder = func(ctx context.Context) (*faad2.Decoder, error) {
			var err error
			decoder, err = factory(ctx)
			return decoder, err
		}
		defer func() {
			newADTSDecoder = factory
			if decoder != nil {
				decoder.Close(context.Background())
			}
		}()
		dl := downloaded(nil)
		dl.done = false
		dl.length = -1
		dl.size = -1
		defer dl.Close()
		if err := dl.writeAt(data[:prefix], 0); err != nil {
			t.Fatal(err)
		}
		result := make(chan error, 1)
		go func() {
			s, err := openAACStreamSpectrum(dl, true)
			if s != nil {
				s.Close()
			}
			result <- err
		}()
		synctest.Wait()
		if decoder == nil {
			t.Fatal("fallback was not created")
		}
		dl.Close()
		synctest.Wait()
		if err := <-result; !errors.Is(err, context.Canceled) {
			t.Fatalf("cancelled open: %v", err)
		}
		if _, err := decoder.Decode(context.Background(), testADTSPacket(t, 0)); !errors.Is(err, faad2.ErrDecoderClosed) {
			t.Fatalf("failed-open decoder remains alive: %v", err)
		}
	})
}
