package player

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log"
	"math"
	"net/http"
	"os"
	"runtime"
	"slices"
	"strconv"
	"strings"
	"sync"
	"time"

	"netease-music-mygo/internal/netease"
)

// A download stores received byte ranges at their original offsets, in a
// temporary cache file or in memory. Seek requests change the order in which
// its worker fills the gaps. Readers can use any contiguous bytes already in.
type download struct {
	mu     sync.Mutex
	cond   sync.Cond
	file   *os.File
	pages  map[int64][]byte
	spans  []byteRange // sorted, with adjacent ranges merged
	n      int64       // distinct bytes received
	size   int64       // size declared by the API, or -1
	length int64       // total size for planning requests, or -1
	done   bool
	err    error

	url      string
	cancel   context.CancelFunc // the entire download
	request  context.CancelFunc // the current HTTP request
	next     int64              // where to look for the next missing range
	revision uint64             // changes when a seek interrupts a request
	finished chan struct{}

	part string
	keep func(part string, n int64) (string, bool)
	temp bool // system temporary file; a write failure here cannot spill again

	// The worker and each reader hold one reference. The last to go closes
	// the file and removes it unless it made it into the cache.
	users     int
	closed    bool
	cache     *Cache
	onRelease func()
	drop      string // a bad cache file to remove after closing (Windows)
	local     bool   // a music-library file that the cache does not own
}

type byteRange struct {
	start, end int64 // end is exclusive
}

// startDownload fetches url into part or a temporary buffer.
func startDownload(url string, size int64, part *os.File, keep func(string, int64) (string, bool)) *download {
	d := newDownload(url, size, part, keep)
	d.start()
	return d
}

func newDownload(url string, size int64, part *os.File, keep func(string, int64) (string, bool)) *download {
	d := &download{url: url, size: size, length: size, users: 2, finished: make(chan struct{})}
	d.cond.L = &d.mu
	if size <= 0 {
		d.size, d.length = -1, -1
	}
	if part != nil {
		d.file, d.part, d.keep = part, part.Name(), keep
	}
	return d
}

func (d *download) start() {
	ctx, cancel := context.WithCancel(context.Background())
	d.cancel = cancel
	go d.run(ctx, stallGap)
}

// downloaded returns an in-memory download that has all of b in already.
func downloaded(b []byte) *download {
	d := opened(nil, int64(len(b)))
	d.pages = make(map[int64][]byte)
	for off := 0; off < len(b); off += downloadPageSize {
		d.pages[int64(off/downloadPageSize)] = b[off:min(off+downloadPageSize, len(b))]
	}
	return d
}

// opened returns a download backed by a complete cache file.
func opened(f *os.File, size int64) *download {
	d := &download{file: f, n: size, size: size, length: size, done: true, cancel: func() {}, users: 1}
	if size > 0 {
		d.spans = []byteRange{{0, size}}
	}
	d.cond.L = &d.mu
	return d
}

// stallGap is how long an HTTP request may go without receiving bytes.
var stallGap = 15 * time.Second

// run issues one request at a time. When a seek starts a request ahead of a
// gap, fill from that point first and return to earlier gaps afterwards.
func (d *download) run(ctx context.Context, gap time.Duration) {
	defer close(d.finished)
	defer d.release()
	defer d.cancel()
	var etag string
	for {
		d.mu.Lock()
		if d.closed {
			d.mu.Unlock()
			return
		}
		if d.length >= 0 && d.rangeEnd(0) >= d.length {
			d.finishLocked(d.hand())
			d.mu.Unlock()
			return
		}
		from, until := d.missingFrom(d.next)
		if d.length >= 0 && from >= d.length {
			from, until = d.missingFrom(0)
		}
		request, cancel := context.WithCancel(ctx)
		d.request, d.next = cancel, from
		revision, expected := d.revision, d.length
		d.mu.Unlock()

		size, err := d.fetchRange(request, cancel, from, until, expected, gap, &etag)
		cancel()

		d.mu.Lock()
		d.request = nil
		if d.closed {
			d.mu.Unlock()
			return
		}
		if revision != d.revision {
			// A seek superseded the request. Its received bytes still count.
			d.mu.Unlock()
			continue
		}
		if err == nil && size >= 0 {
			d.length = size
		}
		if err != nil {
			d.finishLocked(err)
			d.mu.Unlock()
			return
		}
		d.mu.Unlock()
	}
}

func (d *download) finishLocked(err error) {
	d.done, d.err = true, err
	d.cond.Broadcast()
}

// fetchRange requests [from, until), or the rest of the file if until is -1.
// A server may ignore Range and return a complete response from byte zero.
func (d *download) fetchRange(ctx context.Context, cancel context.CancelFunc, from, until, expected int64, gap time.Duration, etag *string) (int64, error) {
	stall := time.AfterFunc(gap, cancel)
	defer stall.Stop()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, d.url, nil)
	if err != nil {
		return -1, err
	}
	req.Header.Set("User-Agent", netease.UserAgent)
	req.Header.Set("Accept-Encoding", "identity")
	if until >= 0 {
		req.Header.Set("Range", fmt.Sprintf("bytes=%d-%d", from, until-1))
	} else if from > 0 {
		req.Header.Set("Range", "bytes="+strconv.FormatInt(from, 10)+"-")
	}
	if *etag != "" && req.Header.Get("Range") != "" {
		req.Header.Set("If-Range", *etag)
	}
	res, err := netease.DoRequest(http.DefaultClient, req)
	if err != nil {
		return -1, err
	}
	defer res.Body.Close()

	// An ETag identifies which representation the buffered bytes belong to.
	if value := res.Header.Get("ETag"); strings.HasPrefix(value, "\"") {
		if *etag != "" && *etag != value {
			return -1, errors.New("audio changed during download")
		}
		*etag = value
	}

	pos, end, total := from, int64(-1), int64(-1)
	switch res.StatusCode {
	case http.StatusOK:
		pos = 0
	case http.StatusPartialContent:
		start, stop, size, ok := contentRange(res.Header.Get("Content-Range"))
		if !ok || start != from || (until >= 0 && stop > until) || (expected >= 0 && (stop > expected || size >= 0 && size != expected)) {
			return -1, errors.New("invalid audio Content-Range")
		}
		end, total = stop, size
	default:
		return -1, errors.New(res.Status)
	}

	buf := make([]byte, 64<<10)
	for {
		n, err := res.Body.Read(buf)
		if n > 0 {
			stall.Reset(gap)
			if end >= 0 && int64(n) > end-pos {
				return -1, errors.New("audio response exceeded its Content-Range")
			}
			if writeErr := d.writeAt(buf[:n], pos); writeErr != nil {
				return -1, writeErr
			}
			pos += int64(n)
		}
		if err == io.EOF {
			if end >= 0 {
				if pos != end {
					return -1, io.ErrUnexpectedEOF
				}
				if total < 0 && expected < 0 && until < 0 {
					total = end
				}
				return total, nil
			}
			if pos < expected {
				return -1, io.ErrUnexpectedEOF
			}
			return pos, nil
		}
		if err != nil {
			return -1, err
		}
	}
}

// contentRange parses the byte interval and optional total from a 206 reply.
func contentRange(raw string) (start, end, total int64, ok bool) {
	value, unit := strings.CutPrefix(raw, "bytes ")
	span, size, slash := strings.Cut(value, "/")
	first, last, dash := strings.Cut(span, "-")
	start, firstErr := strconv.ParseInt(first, 10, 64)
	stop, lastErr := strconv.ParseInt(last, 10, 64)
	if !unit || !slash || !dash || firstErr != nil || lastErr != nil ||
		start < 0 || stop < start || stop == math.MaxInt64 {
		return 0, 0, 0, false
	}
	if size == "*" {
		return start, stop + 1, -1, true
	}
	total, err := strconv.ParseInt(size, 10, 64)
	return start, stop + 1, total, err == nil && total > stop
}

// missingFrom returns the first gap at or after off and the start of the
// next retained span, or -1 if there is none. The caller holds mu.
func (d *download) missingFrom(off int64) (from, until int64) {
	for _, span := range d.spans {
		if span.start > off {
			return off, span.start
		}
		off = max(off, span.end)
	}
	return off, -1
}

// rangeEnd returns the end of the contiguous bytes at off. The caller holds mu.
func (d *download) rangeEnd(off int64) int64 {
	for _, span := range d.spans {
		if off < span.start {
			break
		}
		if off < span.end {
			return span.end
		}
	}
	return off
}

// writeAt stores only newly received bytes. This also preserves buffered
// data if a server ignores Range and sends the whole body again.
func (d *download) writeAt(b []byte, off int64) error {
	d.mu.Lock()
	defer d.mu.Unlock()
	if d.closed {
		return context.Canceled
	}
	end := off + int64(len(b))
	for next := off; next < end; {
		from, until := d.missingFrom(next)
		if from >= end {
			break
		}
		if until < 0 {
			until = end
		}
		next = min(until, end)
		p := b[from-off : next-off]
		if err := d.writeGap(p, from); err != nil {
			return err
		}
		d.addSpan(from, next)
	}
	d.cond.Broadcast()
	return nil
}

// writeGap keeps sparse seeks and backward reads available without retaining
// the whole response on the heap. The caller holds mu.
func (d *download) writeGap(p []byte, off int64) error {
	if d.file != nil {
		if _, err := d.file.WriteAt(p, off); err == nil || d.temp {
			return err
		}
		// A full or disconnected cache can still play via a small memory
		// buffer or the system's temporary filesystem.
		if len(d.spans) <= 1 && d.n <= downloadMemoryLimit-downloadPageSize {
			if err := d.toMemory(); err != nil {
				return err
			}
		} else if err := d.spill(); err != nil {
			return err
		}
	}
	if d.file == nil {
		if len(d.pages)*downloadPageSize+len(p)+downloadPageSize <= downloadMemoryLimit {
			d.writeMemory(p, off)
			return nil
		}
		if err := d.spill(); err != nil {
			return err
		}
	}
	_, err := d.file.WriteAt(p, off)
	return err
}

// addSpan marks a newly written gap as readable, merging its neighbors.
func (d *download) addSpan(start, end int64) {
	d.n += end - start
	i := 0
	for i < len(d.spans) && d.spans[i].end < start {
		i++
	}
	j := i
	for j < len(d.spans) && d.spans[j].start <= end {
		start, end = min(start, d.spans[j].start), max(end, d.spans[j].end)
		j++
	}
	d.spans = slices.Replace(d.spans, i, j, byteRange{start, end})
}

// Memory uses fixed-size pages so a far seek does not allocate the gap.
const downloadPageSize = 64 << 10
const downloadMemoryLimit = 4 << 20

func (d *download) writeMemory(b []byte, off int64) {
	if d.pages == nil {
		d.pages = make(map[int64][]byte)
	}
	for len(b) > 0 {
		page := off / downloadPageSize
		p := d.pages[page]
		if p == nil {
			p = make([]byte, downloadPageSize)
			d.pages[page] = p
		}
		n := copy(p[off%downloadPageSize:], b)
		b, off = b[n:], off+int64(n)
	}
}

func (d *download) copySpans(write func([]byte, int64) error) error {
	buf := make([]byte, downloadPageSize)
	for _, span := range d.spans {
		for off := span.start; off < span.end; {
			b := buf[:min(int64(len(buf)), span.end-off)]
			if _, err := d.readAt(b, off); err != nil {
				return err
			}
			if err := write(b, off); err != nil {
				return err
			}
			off += int64(len(b))
		}
	}
	return nil
}

// toMemory copies a small buffered prefix if the cache file stops accepting writes.
func (d *download) toMemory() error {
	if err := d.copySpans(func(b []byte, off int64) error {
		d.writeMemory(b, off)
		return nil
	}); err != nil {
		d.pages = nil
		return err
	}
	d.closeFile()
	removeCacheFile(d.part)
	d.file, d.part, d.keep = nil, "", nil
	return nil
}

// spill moves all received ranges to a disposable file outside the cache.
func (d *download) spill() error {
	f, err := os.CreateTemp("", "mygo-audio-*.part")
	if err != nil {
		return fmt.Errorf("create temporary audio buffer: %w", err)
	}
	if err := d.copySpans(func(b []byte, off int64) error {
		_, err := f.WriteAt(b, off)
		return err
	}); err != nil {
		closeErr := f.Close()
		removeCacheFile(f.Name())
		return fmt.Errorf("copy to temporary audio buffer: %w", errors.Join(err, closeErr))
	}
	if d.file != nil {
		d.closeFile()
		removeCacheFile(d.part)
	}
	d.file, d.part, d.pages, d.keep, d.temp = f, f.Name(), nil, nil, true
	// Where supported, unlink now so a process exit also reclaims the file.
	if runtime.GOOS != "windows" {
		removeCacheFile(d.part)
	}
	return nil
}

// hand publishes a complete song to the cache while holding mu, closing the
// file before moving it so Windows can rename it.
func (d *download) hand() error {
	if d.file == nil || d.part == "" || d.keep == nil || (d.size > 0 && d.n != d.size) {
		return nil
	}
	if err := d.file.Close(); err != nil {
		d.file, d.spans, d.n = nil, nil, 0
		return fmt.Errorf("close downloaded audio before caching: %w", err)
	}
	path, ok := d.keep(d.part, d.n)
	if ok {
		d.part = ""
	} else {
		d.part = path
	}
	f, err := os.Open(path)
	if err != nil {
		d.file, d.spans, d.n = nil, nil, 0
		return err
	}
	d.file = f
	return nil
}

// discard prevents a failed stream from being kept or served from the cache
// again: its metadata goes at once, and the song itself as soon as nothing
// reads it, which is when Windows lets it go.
func (d *download) discard() {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.keep = nil
	if d.local || d.cache == nil || d.file == nil || d.part != "" {
		return
	}
	d.cache.mu.Lock()
	defer d.cache.mu.Unlock()
	path := d.file.Name()
	opened, err := d.file.Stat()
	current, statErr := os.Stat(path)
	if err != nil || statErr != nil || !os.SameFile(opened, current) {
		return // Another download has already replaced this file.
	}
	removeSongMetadata(path)
	if d.cache.bad == nil {
		d.cache.bad = make(map[string]os.FileInfo)
	}
	d.cache.bad[path] = opened
	d.drop = path
}

// seekTo prioritizes missing bytes at off, or the next gap after a buffered
// position if it is behind the active request. Leave it alone if shared.
func (d *download) seekTo(off int64) bool {
	d.mu.Lock()
	defer d.mu.Unlock()
	if off < 0 || d.done || d.users > 2 {
		return false
	}
	if have := d.rangeEnd(off); have > off {
		if have >= d.next {
			return false
		}
		off = have
	}
	if off == d.next || (d.length >= 0 && off >= d.length) {
		return false
	}
	d.next = off
	d.revision++
	if d.request != nil {
		d.request()
	}
	return true
}

// Close stops the download and wakes its readers: the stream lets the
// song go.
func (d *download) Close() {
	d.mu.Lock()
	d.cancel()
	if !d.done {
		d.done, d.err = true, context.Canceled
	}
	d.cond.Broadcast()
	closed := d.closed
	d.closed = true
	d.mu.Unlock()
	if !closed {
		d.release()
	}
}

// share counts one more user of the song, as a second stream decoding
// it, unless it is gone already.
func (d *download) share() bool {
	d.mu.Lock()
	defer d.mu.Unlock()
	if d.users <= 0 || d.closed {
		return false
	}
	d.users++
	return true
}

// release lets the song go for one of its users. With none left, nothing
// reads it any more: its file closes, and goes unless the cache kept it.
func (d *download) release() {
	d.mu.Lock()
	defer d.mu.Unlock()
	if d.users--; d.users > 0 {
		return
	}
	d.cleanupLocked()
	if d.onRelease != nil {
		d.onRelease()
	}
}

// halt stops the song for good, for its cache to move: the fetch stops, and
// is waited for, then the file goes even under a decoder still reading it,
// so that no file of the cache is left open. What reads the song reaches the
// file under the lock, and finds none.
func (d *download) halt() {
	d.Close()
	d.mu.Lock()
	finished := d.finished
	d.mu.Unlock()
	if finished != nil {
		<-finished
	}
	d.mu.Lock()
	defer d.mu.Unlock()
	d.cleanupLocked()
}

func (d *download) closeFile() {
	if d.file != nil {
		if err := d.file.Close(); err != nil {
			log.Printf("audio: close: %v", err)
		}
		d.file = nil
	}
}

// cleanupLocked retires scratch files and closes the backing file once,
// including when halt runs before the last reader releases its reference.
func (d *download) cleanupLocked() {
	if d.drop != "" && d.file != nil {
		// Serialize with publication so the path cannot be replaced between
		// checking the identity and unlinking it after closing the handle.
		d.cache.mu.Lock()
		opened, err := d.file.Stat()
		current, statErr := os.Stat(d.drop)
		d.closeFile()
		if err == nil && statErr == nil && os.SameFile(opened, current) {
			d.cache.remove(d.drop)
		}
		d.cache.mu.Unlock()
	} else {
		d.closeFile()
	}
	if d.part != "" {
		removeCacheFile(d.part)
	}
	d.drop, d.part = "", ""
	d.pages, d.spans, d.n = nil, nil, 0
}

// Progress returns the share of the song downloaded so far.
func (d *download) Progress() float64 {
	d.mu.Lock()
	defer d.mu.Unlock()
	if d.done {
		if d.err == nil {
			return 1
		}
		return 0
	}
	if d.length <= 0 {
		return 0
	}
	return min(1, float64(d.n)/float64(d.length))
}

// Err returns why the download stopped before the whole song came in, or
// nil: it came in whole, or still comes in.
func (d *download) Err() error {
	d.mu.Lock()
	defer d.mu.Unlock()
	if !d.done {
		return nil
	}
	return d.err
}

// waitFor waits for the prefix up to n, or for completion if n is MaxInt64.
func (d *download) waitFor(n int64) int64 {
	if n != math.MaxInt64 {
		return d.waitRange(0, n)
	}
	d.mu.Lock()
	defer d.mu.Unlock()
	for !d.done {
		d.cond.Wait()
	}
	return d.n
}

// waitRange waits for [off, end) to be readable or for the download to stop.
func (d *download) waitRange(off, end int64) int64 {
	d.mu.Lock()
	defer d.mu.Unlock()
	have := d.rangeEnd(off)
	for have < end && !d.done {
		d.cond.Wait()
		have = d.rangeEnd(off)
	}
	return have
}

// readAt reads into b as far as contiguous bytes are in. The caller holds mu.
func (d *download) readAt(b []byte, off int64) (int, error) {
	n := min(int64(len(b)), d.rangeEnd(off)-off)
	if n <= 0 {
		return 0, nil
	}
	if d.file != nil {
		return d.file.ReadAt(b[:n], off)
	}
	p := b[:n]
	for len(p) != 0 {
		data := d.pages[off/downloadPageSize]
		got := copy(p, data[off%downloadPageSize:])
		p, off = p[got:], off+int64(got)
	}
	return int(n), nil
}

// peek returns up to n bytes from off, as far as they are in.
func (d *download) peek(off int64, n int) []byte {
	return d.peekInto(make([]byte, n), off)
}

// peekInto fills b with what is in from off, as much as fits, and returns the
// part it filled: a scan looking for a frame reads into the same b for every
// look rather than allocating another 32 KiB.
func (d *download) peekInto(b []byte, off int64) []byte {
	d.mu.Lock()
	defer d.mu.Unlock()
	got, _ := d.readAt(b, off)
	return b[:got]
}

// arrived returns the length of the downloaded prefix and whether it stopped.
func (d *download) arrived() (int64, bool) {
	d.mu.Lock()
	defer d.mu.Unlock()
	return d.rangeEnd(0), d.done
}
