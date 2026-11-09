// Package diskcache stores disposable, bounded snapshots. Call Read and Put
// off the UI thread; Queue only schedules a write of immutable bytes.
package diskcache

import (
	"bytes"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"errors"
	"log"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

const headerSize = 48 // magic, timestamp and checksum, followed by the payload
const maxEntry = 24 << 20
const digestLength = sha256.Size * 2
const entrySuffix = ".entry"

// The I/O lock precedes mu whenever both are needed. Acceptance uses mu alone:
// the latest per-entry operation wins, while Clear and Suspend invalidate the
// whole epoch. Clearing stays a barrier until every accepted clear finishes.
type Store struct {
	index     *indexLog // configured once; all mutable index state belongs to io
	forgotten map[string]uint64
	dir       string
	limit     int64
	spare     int64        // conservative headroom; owned by io, reset after clear/move
	io        sync.RWMutex // content reads share this; index state and mutations own it exclusively
	dirty     atomic.Bool  // a corrupt read invalidates spare without upgrading its read lock
	mu        sync.Mutex
	pending   map[string]uint64
	serial    uint64
	epoch     atomic.Uint64
	clearing  atomic.Bool
	closed    atomic.Bool
	moving    atomic.Bool
	workers   sync.WaitGroup // writes drained before a directory move
	cleaners  sync.WaitGroup // removals may also be accepted during a move
	clears    int            // protected by mu
	resumed   chan struct{}  // protected by mu; removals wait while source files are copied
}

func New(dir string, limit int64) *Store {
	return &Store{dir: dir, limit: limit, pending: map[string]uint64{}, forgotten: map[string]uint64{}}
}
func (s *Store) Epoch() uint64 {
	if s == nil {
		return 0
	}
	return s.epoch.Load()
}
func digest(s string) string        { sum := sha256.Sum256([]byte(s)); return hex.EncodeToString(sum[:]) }
func name(scope, key string) string { return digest(scope) + "-" + digest(key) + entrySuffix }

// Read verifies the complete payload before returning it. Corrupt and partial
// files are misses, never empty successful responses.
func (s *Store) Read(scope, key string) ([]byte, time.Time) {
	if s == nil || s.closed.Load() || s.clearing.Load() {
		return nil, time.Time{}
	}
	file := name(scope, key)
	indexed := s.index.accepts(file)
	if indexed {
		s.io.Lock()
		defer s.io.Unlock()
	} else {
		s.io.RLock()
		defer s.io.RUnlock()
	}
	s.mu.Lock()
	unavailable := s.closed.Load() || s.clearing.Load() || s.forgotten[file] != 0
	s.mu.Unlock()
	if unavailable {
		return nil, time.Time{}
	}
	if indexed {
		return s.index.read(s.dir, file)
	}
	path := filepath.Join(s.dir, file)
	info, err := os.Stat(path)
	if err != nil || info.Size() < headerSize || info.Size() > maxEntry+headerSize {
		return nil, time.Time{}
	}
	b, err := os.ReadFile(path)
	if err != nil || len(b) < headerSize {
		return nil, time.Time{}
	}
	sum := sha256.Sum256(b[headerSize:])
	if string(b[:8]) != "MYCACHE1" || !bytes.Equal(sum[:], b[16:48]) {
		if !s.moving.Load() {
			os.Remove(path)
		}
		// Unexpected filesystem damage invalidates our last size estimate;
		// the next write also removes any interrupted temporary files.
		s.dirty.Store(true)
		return nil, time.Time{}
	}
	now := time.Now()
	if !s.moving.Load() {
		os.Chtimes(path, now, now)
	}
	return b[headerSize:], time.Unix(0, int64(binary.LittleEndian.Uint64(b[8:16])))
}

// Queue accepts already serialized bytes, not mutable view state. The epoch
// is captured before a request starts, so clearing cannot be undone by it.
func (s *Store) Queue(scope, key string, b []byte, epoch uint64) {
	if write := s.Prepare(scope, key, epoch); write != nil {
		go write(b)
	}
}

// Prepare reserves acceptance order before a background serialization. A
// slower old encoder cannot replace a more recently accepted snapshot.
func (s *Store) Prepare(scope, key string, epoch uint64) func([]byte) {
	if s == nil || s.closed.Load() || s.moving.Load() || s.clearing.Load() || s.Epoch() != epoch {
		return nil
	}
	file := name(scope, key)
	s.mu.Lock()
	if s.closed.Load() || s.moving.Load() || s.clearing.Load() || s.Epoch() != epoch || len(s.pending) >= 32 && s.pending[file] == 0 {
		s.mu.Unlock()
		return nil
	}
	s.serial++
	seq := s.serial
	s.pending[file] = seq
	s.workers.Add(1)
	s.mu.Unlock()
	return func(b []byte) { s.write(file, b, epoch, seq) }
}

// Put commits content synchronously before an index refers to it.
func (s *Store) Put(scope, key string, b []byte, epoch uint64) {
	if write := s.Prepare(scope, key, epoch); write != nil {
		write(b)
	}
}

// Forget invalidates only this entry. A later successful write cancels its
// tombstone, so a delayed removal cannot delete the replacement.
func (s *Store) Forget(scope, key string) {
	if remove := s.prepareForget(scope, key); remove != nil {
		go remove()
	}
}

func (s *Store) prepareForget(scope, key string) func() {
	if s == nil {
		return nil
	}
	file := name(scope, key)
	s.mu.Lock()
	if s.closed.Load() {
		s.mu.Unlock()
		return nil
	}
	s.serial++
	seq := s.serial
	s.forgotten[file] = seq
	delete(s.pending, file)
	s.cleaners.Add(1)
	s.mu.Unlock()
	return func() {
		defer s.cleaners.Done()
		s.lockRemoval()
		defer s.io.Unlock()
		s.mu.Lock()
		current := s.forgotten[file] == seq
		s.mu.Unlock()
		if !current {
			return
		}
		if s.index.accepts(file) {
			before := s.index.size
			s.indexError(s.index.forget(s.dir, file))
			s.prune(max(0, s.index.size-before), "")
		} else {
			os.Remove(filepath.Join(s.dir, file))
		}
		s.mu.Lock()
		if s.forgotten[file] == seq {
			delete(s.forgotten, file)
		}
		s.mu.Unlock()
	}
}

func (s *Store) write(file string, b []byte, epoch, seq uint64) {
	defer s.workers.Done()
	s.io.Lock()
	defer s.io.Unlock()
	defer func() {
		s.mu.Lock()
		if s.pending[file] == seq {
			delete(s.pending, file)
		}
		s.mu.Unlock()
	}()
	current := func() bool {
		s.mu.Lock()
		defer s.mu.Unlock()
		return s.pending[file] == seq && s.Epoch() == epoch && !s.clearing.Load()
	}
	if b == nil || !current() || s.limit <= 0 || len(b) > maxEntry {
		return
	}
	if s.index.accepts(file) {
		before := s.index.size
		accepted, err := s.index.put(s.dir, file, b)
		s.indexError(err)
		if accepted {
			s.mu.Lock()
			if s.pending[file] == seq {
				delete(s.forgotten, file)
			}
			s.mu.Unlock()
			s.scheduleIndex()
			s.prune(max(0, s.index.size-before), "")
		}
		return
	}
	if os.MkdirAll(s.dir, 0700) != nil {
		return
	}
	f, err := os.CreateTemp(s.dir, ".part-")
	if err != nil {
		return
	}
	path := f.Name()
	defer os.Remove(path)
	header := make([]byte, headerSize)
	copy(header, "MYCACHE1")
	binary.LittleEndian.PutUint64(header[8:16], uint64(time.Now().UnixNano()))
	sum := sha256.Sum256(b)
	copy(header[16:], sum[:])
	_, e1 := f.Write(header)
	_, e2 := f.Write(b)
	e3 := f.Close()
	if e1 != nil || e2 != nil || e3 != nil || !current() {
		return
	}
	// Commit under mu as well: Forget cannot be accepted between the final
	// order check and the rename that makes the entry readable again.
	s.mu.Lock()
	committed := s.pending[file] == seq && s.Epoch() == epoch && !s.clearing.Load() && os.Rename(path, filepath.Join(s.dir, file)) == nil
	if committed {
		delete(s.forgotten, file)
	}
	s.mu.Unlock()
	if committed {
		s.prune(int64(headerSize+len(b)), file)
	}
}

// Clear invalidates requests immediately; deletion and accounting stay off
// the UI thread. Empty scope clears everything, otherwise just that account.
func (s *Store) Clear(scope string, done func()) {
	remove := s.prepareClear(scope)
	if remove == nil {
		if done != nil {
			done()
		}
		return
	}
	go func() {
		remove()
		if done != nil {
			done()
		}
	}()
}

func (s *Store) prepareClear(scope string) func() {
	if s == nil {
		return nil
	}
	s.mu.Lock()
	if s.closed.Load() {
		s.mu.Unlock()
		return nil
	}
	s.epoch.Add(1)
	s.clears++
	s.clearing.Store(true)
	s.cleaners.Add(1)
	s.mu.Unlock()
	return func() {
		defer s.cleaners.Done()
		s.lockRemoval()
		defer s.io.Unlock()
		if s.index != nil {
			if scope == "" {
				s.index.reset()
			} else if s.index.scopes[digest(scope)] {
				s.indexError(s.index.clearScope(s.dir, scope))
			}
		}
		entries, _ := os.ReadDir(s.dir)
		for _, e := range entries {
			if !e.IsDir() && (scope == "" || strings.HasPrefix(e.Name(), digest(scope)+"-")) {
				os.Remove(filepath.Join(s.dir, e.Name()))
			}
		}
		s.spare = 0
		s.mu.Lock()
		s.clears--
		s.clearing.Store(s.clears != 0)
		s.mu.Unlock()
	}
}

func (s *Store) Size() int64 {
	if s == nil {
		return 0
	}
	s.io.Lock()
	defer s.io.Unlock()
	var n int64
	entries, _ := os.ReadDir(s.dir)
	for _, e := range entries {
		if i, err := e.Info(); err == nil && !i.IsDir() {
			n += i.Size()
		}
	}
	if s.index != nil {
		n += int64(len(s.index.pending))
	}
	return n
}

// prune charges worst-case growth against the last measured free space.
// Replacements deliberately receive no credit: this can trigger an early scan,
// but cannot exceed the limit. A full directory scan happens only when that
// headroom runs out; eviction then frees a batch rather than one file at a time.
// The caller holds io, so normal app writes cannot bypass this accounting.
func (s *Store) prune(added int64, keep string) {
	if s.dirty.Swap(false) {
		s.spare = 0
	}
	if s.spare > 0 && added <= s.spare {
		s.spare -= added
		return
	}
	entries, _ := os.ReadDir(s.dir)
	var files []os.FileInfo
	var size int64
	var fixed int64 // the index cannot be evicted as an ordinary content file
	if s.index != nil {
		size = int64(len(s.index.pending))
		fixed = size
	}
	for _, e := range entries {
		if strings.HasPrefix(e.Name(), ".part-") {
			os.Remove(filepath.Join(s.dir, e.Name()))
			continue
		}
		if i, err := e.Info(); err == nil && !i.IsDir() {
			if s.index == nil || e.Name() != IndexFile {
				files = append(files, i)
			} else {
				fixed += i.Size()
			}
			size += i.Size()
		}
	}
	defer func() { s.spare = max(0, s.limit-size) }()
	if size <= s.limit {
		return
	}
	sort.Slice(files, func(i, j int) bool {
		if files[i].ModTime().Equal(files[j].ModTime()) {
			return files[i].Name() < files[j].Name()
		}
		return files[i].ModTime().Before(files[j].ModTime())
	})
	for _, f := range files {
		if size <= s.limit*3/4 {
			break
		}
		// Coarse filesystem timestamps cannot identify the latest commit.
		// Protect it up to the hard limit, even above the batch target.
		if f.Name() == keep && f.Size() <= s.limit-fixed {
			continue
		}
		if os.Remove(filepath.Join(s.dir, f.Name())) == nil {
			size -= f.Size()
		}
	}
}

// Close stops accepting requests and drains already accepted snapshots.
func (s *Store) Close() {
	if s == nil {
		return
	}
	s.mu.Lock()
	s.closed.Store(true)
	s.mu.Unlock()
	s.workers.Wait()
	s.cleaners.Wait()
	s.io.Lock()
	if s.index != nil {
		s.indexError(s.index.flush(s.dir))
		s.index.reset()
	}
	s.io.Unlock()
}

// Suspend freezes writes and removals while leaving old snapshots readable.
// It invalidates earlier requests; resume switches paths before originals go.
// Call and resume off the UI thread. An empty path resumes the original store.
// A failed index flush leaves the store usable and prevents moving incomplete metadata.
// The returned resume function is safe to call even when suspension fails.
func (s *Store) Suspend() (func(string), error) {
	if s == nil {
		return func(string) {}, nil
	}
	s.mu.Lock()
	if s.moving.Load() {
		s.mu.Unlock()
		return func(string) {}, errors.New("cache already moving")
	}
	s.moving.Store(true)
	s.resumed = make(chan struct{})
	s.mu.Unlock()
	// Prepare checks moving under the same lock, so no worker can be added
	// after this wait begins. Accepted snapshots are included in the copy.
	s.workers.Wait()
	s.epoch.Add(1)
	s.io.Lock()
	if s.index != nil {
		// Replay/repair before copying, so later reads only touch memory.
		err := s.index.load(s.dir)
		if err == nil {
			err = s.index.flush(s.dir)
		}
		if err != nil {
			s.finishMove()
			s.io.Unlock()
			return func(string) {}, err
		}
	}
	s.io.Unlock()
	var once sync.Once
	return func(dir string) {
		once.Do(func() {
			s.io.Lock()
			defer s.io.Unlock()
			if dir != "" {
				s.dir = dir
				s.spare = 0
				if s.index != nil {
					s.index.reset()
				}
			}
			s.finishMove()
		})
	}, nil
}

func (s *Store) finishMove() {
	s.mu.Lock()
	s.moving.Store(false)
	close(s.resumed)
	s.resumed = nil
	s.mu.Unlock()
}

// Deletion waits without holding io, keeping cache hits available during a
// long copy. Recheck after taking io in case another move started meanwhile.
func (s *Store) lockRemoval() {
	for {
		s.mu.Lock()
		resumed := s.resumed
		s.mu.Unlock()
		if resumed != nil {
			<-resumed
		}
		s.io.Lock()
		if !s.moving.Load() {
			return
		}
		s.io.Unlock()
	}
}

func (s *Store) indexError(err error) {
	if err != nil {
		// A failed append can leave partial bytes on disk. Recheck usage
		// before trusting headroom for another write; callers hold io.
		s.spare = 0
		log.Printf("cache index: operation failed (%T)", err)
	}
}
