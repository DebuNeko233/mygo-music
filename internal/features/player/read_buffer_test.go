/*
验证缓冲读取的偏移、EOF、并发和错误传播，并测量文件读取开销。
*/

package player

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"sync"
	"sync/atomic"
	"testing"
)

// Cached reads preserve bytes and EOF across read-ahead boundaries and backward seeks.
func TestBufferedReaderAtPositionsAndEOF(t *testing.T) {
	data := make([]byte, audioReadBuffer*3+13)
	for i := range data {
		data[i] = byte(i * 97)
	}
	source := bytes.NewReader(data)
	r := &bufferedReaderAt{source: source}
	for _, read := range []struct {
		at   int64
		size int
	}{
		{0, 7}, {5, 11}, {audioReadBuffer - 3, 17}, {2*audioReadBuffer + 4, 23},
		{1, 9}, {20, audioReadBuffer + 1}, {int64(len(data) - 3), 7},
		{int64(len(data)), 1}, {int64(len(data) + 4), 9}, {0, 0}, {int64(len(data)), 0},
	} {
		got, want := make([]byte, read.size), make([]byte, read.size)
		n, err := r.ReadAt(got, read.at)
		m, expected := source.ReadAt(want, read.at)
		if n != m || !errors.Is(err, expected) || !bytes.Equal(got[:n], want[:m]) {
			t.Fatalf("read %d at %d: %d/%d bytes, %v/%v", read.size, read.at, n, m, err, expected)
		}
	}
}

// Sequential packet headers share file reads instead of making a syscall per header.
func TestBufferedReaderAtSmallReadBudget(t *testing.T) {
	data := make([]byte, 3*audioReadBuffer)
	source := &countedReaderAt{source: bytes.NewReader(data)}
	r := &bufferedReaderAt{source: source}
	var header [7]byte
	for at := int64(0); at+int64(len(header)) <= int64(len(data)); at += int64(len(header)) {
		if _, err := r.ReadAt(header[:], at); err != nil {
			t.Fatal(err)
		}
	}
	if calls := source.reads.Load(); calls > 4 {
		t.Fatalf("small headers made %d backend reads", calls)
	}
}

// ReaderAt callers can read different packet positions concurrently without sharing stale bytes.
func TestBufferedReaderAtConcurrent(t *testing.T) {
	data := make([]byte, 4*audioReadBuffer)
	for i := range data {
		data[i] = byte(i * 97)
	}
	r := &bufferedReaderAt{source: bytes.NewReader(data)}
	var wg sync.WaitGroup
	for worker := range 8 {
		wg.Go(func() {
			var got [33]byte
			for i := range 80 {
				at := (worker*17017 + i*997) % (len(data) - len(got))
				if n, err := r.ReadAt(got[:], int64(at)); n != len(got) || err != nil || !bytes.Equal(got[:], data[at:at+len(got)]) {
					t.Errorf("worker %d at %d: %d bytes, %v", worker, at, n, err)
					return
				}
			}
		})
	}
	wg.Wait()
}

// Read-ahead failures are reported when the caller reaches the bytes that failed.
func TestBufferedReaderAtPreservesFailure(t *testing.T) {
	failure := errors.New("file disconnected")
	r := &bufferedReaderAt{source: failingReaderAt{bytes.NewReader([]byte("available")), failure}}
	first := make([]byte, 4)
	if n, err := r.ReadAt(first, 0); n != 4 || err != nil || string(first) != "avai" {
		t.Fatalf("available prefix: %q, %d, %v", first, n, err)
	}
	end := make([]byte, 8)
	if n, err := r.ReadAt(end, 4); n != 5 || !errors.Is(err, failure) || string(end[:n]) != "lable" {
		t.Fatalf("failed tail: %q, %d, %v", end, n, err)
	}
}

type countedReaderAt struct {
	source io.ReaderAt
	reads  atomic.Int64
}

func (r *countedReaderAt) ReadAt(p []byte, off int64) (int, error) {
	r.reads.Add(1)
	return r.source.ReadAt(p, off)
}

type failingReaderAt struct {
	io.ReaderAt
	failure error
}

func (r failingReaderAt) ReadAt(p []byte, off int64) (int, error) {
	n, err := r.ReaderAt.ReadAt(p, off)
	if err != nil {
		return n, r.failure
	}
	return n, nil
}

// Compare small-packet, PCM-block and large reads through the same file interface.
func BenchmarkAudioFileReads(b *testing.B) {
	data := make([]byte, audioReadBuffer*16)
	d := mp3FileDownload(b, data)
	for _, size := range []int{7, 512, 8192, 65536} {
		for _, buffered := range []bool{false, true} {
			b.Run(fmt.Sprintf("bytes%d/buffered%t", size, buffered), func(b *testing.B) {
				counter := &countedReaderAt{source: d.file}
				var source io.ReaderAt = counter
				if buffered {
					source = &bufferedReaderAt{source: counter}
				}
				p := make([]byte, size)
				at := int64(0)
				b.ReportAllocs()
				b.SetBytes(int64(size))
				for b.Loop() {
					if _, err := source.ReadAt(p, at); err != nil {
						b.Fatal(err)
					}
					at += int64(size)
					if at+int64(size) > int64(len(data)) {
						at = 0
					}
				}
				b.ReportMetric(float64(counter.reads.Load())/float64(b.N), "reads/op")
			})
		}
	}
}
