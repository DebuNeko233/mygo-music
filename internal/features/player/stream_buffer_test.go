/*
验证 MP3 缓冲解码保持 PCM 一致，并支持下载未完成时播放和失败传播。
*/

package player

import (
	"bufio"
	"bytes"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"os"
	"testing"
	"testing/synctest"

	gomp3 "github.com/hajimehoshi/go-mp3"
)

// Buffered cache playback preserves every PCM sample from an unbuffered decoder.
func TestMP3BufferedCacheMatchesPCM(t *testing.T) {
	data := song(t)
	dec, err := gomp3.NewDecoder(bytes.NewReader(data))
	if err != nil {
		t.Fatal(err)
	}
	want, err := io.ReadAll(dec)
	if err != nil {
		t.Fatal(err)
	}
	s, err := openMP3StreamSpectrum(mp3FileDownload(t, data), 5000, true)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	var got []float32
	buf := make([]float32, 4096)
	for {
		n, err := s.Read(buf)
		got = append(got, buf[:n]...)
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			t.Fatal(err)
		}
		if n == 0 {
			t.Fatal("decoder returned no samples without EOF")
		}
	}
	if len(got)*2 != len(want) {
		t.Fatalf("decoded %d samples, want %d", len(got), len(want)/2)
	}
	for i, v := range got {
		expected := float32(int16(binary.LittleEndian.Uint16(want[2*i:]))) / (1 << 15)
		if v != expected {
			t.Fatalf("sample %d: got %v, want %v", i, v, expected)
		}
	}
}

// An unfinished download starts playing its available prefix without filling the read buffer.
func TestMP3BufferStartsBeforeDownloadFinishes(t *testing.T) {
	data := song(t)
	synctest.Test(t, func(t *testing.T) {
		d := downloaded(data[:len(data)/4])
		d.done, d.size, d.length = false, int64(len(data)), int64(len(data))
		defer d.Close()
		result := make(chan error, 1)
		go func() {
			s, err := openMP3StreamSpectrum(d, 5000, true)
			if err == nil {
				var n int
				n, err = s.Read(make([]float32, 4096))
				if err == nil && n == 0 {
					err = errors.New("no samples from the available prefix")
				}
			}
			result <- err
		}()
		synctest.Wait()
		select {
		case err := <-result:
			if err != nil {
				t.Fatal(err)
			}
		default:
			t.Fatal("playback waited for more data although complete MP3 frames were available")
		}
	})
}

// A buffered reader drains available bytes, waits at a gap, and preserves the download failure.
func TestMP3BufferWaitsAndPreservesFailure(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		d := opened(nil, 0)
		d.done, d.size, d.length = false, 20, 20
		defer d.Close()
		if err := d.writeAt([]byte("first"), 0); err != nil {
			t.Fatal(err)
		}
		r := mp3DownloadReader(d, 0)
		buf := make([]byte, 5)
		if _, err := io.ReadFull(r, buf); err != nil || string(buf) != "first" {
			t.Fatalf("first bytes: %q, %v", buf, err)
		}
		type readResult struct {
			data string
			err  error
		}
		result := make(chan readResult, 1)
		go func() {
			buf := make([]byte, 6)
			n, err := io.ReadFull(r, buf)
			result <- readResult{string(buf[:n]), err}
		}()
		synctest.Wait()
		select {
		case got := <-result:
			t.Fatalf("read across the gap returned early: %+v", got)
		default:
		}
		if err := d.writeAt([]byte("second"), 5); err != nil {
			t.Fatal(err)
		}
		synctest.Wait()
		got := <-result
		if got.data != "second" || got.err != nil {
			t.Fatalf("resumed read: %+v", got)
		}
		failure := errors.New("download interrupted")
		d.mu.Lock()
		d.done, d.err = true, failure
		d.cond.Broadcast()
		d.mu.Unlock()
		if _, err := r.ReadByte(); !errors.Is(err, failure) {
			t.Fatalf("download failure: got %v, want %v", err, failure)
		}
	})
}

// mp3FileDownload keeps the production cache-file path in playback and benchmark coverage.
func mp3FileDownload(t testing.TB, data []byte) *download {
	t.Helper()
	f, err := os.CreateTemp(t.TempDir(), "song-*.mp3")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { f.Close() })
	if _, err := f.Write(data); err != nil {
		t.Fatal(err)
	}
	return opened(f, int64(len(data)))
}

type mp3ReadCounter struct {
	io.Reader
	reads int
}

func (r *mp3ReadCounter) Read(p []byte) (int, error) {
	r.reads++
	return r.Reader.Read(p)
}

// Decode the same cache file with different read buffers, including decoder startup and EOF.
func BenchmarkMP3CacheDecode(b *testing.B) {
	data, err := os.ReadFile("testdata/seek.mp3")
	if err != nil {
		b.Fatal(err)
	}
	for _, size := range []int{0, 4 << 10, 32 << 10} {
		b.Run(fmt.Sprintf("buffer%d", size), func(b *testing.B) {
			d := mp3FileDownload(b, data)
			var reads int64
			b.ReportAllocs()
			b.SetBytes(int64(len(data)))
			for b.Loop() {
				counter := &mp3ReadCounter{Reader: &downloadReader{d: d}}
				var reader io.Reader = counter
				if size != 0 {
					reader = bufio.NewReaderSize(reader, size)
				}
				dec, err := gomp3.NewDecoder(reader)
				if err != nil {
					b.Fatal(err)
				}
				if _, err := io.Copy(io.Discard, dec); err != nil {
					b.Fatal(err)
				}
				reads += int64(counter.reads)
			}
			b.ReportMetric(float64(reads)/float64(b.N), "reads/op")
		})
	}
}
