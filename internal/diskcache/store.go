/*
实现有容量上限的磁盘快照缓存，协调异步写入、失效、淘汰和目录迁移。
*/

// Package diskcache stores disposable, bounded snapshots. Call Read and Put
// off the UI thread; Queue only schedules a write of immutable bytes.
package diskcache

import (
	"bytes"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"errors"
	"io"
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
	access    map[string]entryAccess // entries whose disk timestamp or removal failed
	// After a failed clear, only successful replacements in that scope are readable.
	uncleared map[string]map[string]bool // empty prefix covers every scope; owned by mu
	dir       string
	limit     int64
	spare     int64        // conservative headroom; owned by io, reset after clear/move
	latest    string       // last content commit; its following index writes must not evict it
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

type entryAccess struct {
	info os.FileInfo
	used time.Time
	bad  bool
}

func (a entryAccess) matches(info os.FileInfo) bool {
	return os.SameFile(a.info, info) && a.info.Size() == info.Size() && a.info.ModTime().Equal(info.ModTime())
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
	unavailable := s.closed.Load() || s.clearing.Load() || s.hidden(file)
	access := s.access[file]
	s.mu.Unlock()
	if unavailable {
		return nil, time.Time{}
	}
	if indexed {
		return s.index.read(s.dir, file)
	}
	path := filepath.Join(s.dir, file)
	info, err := os.Lstat(path)
	if err != nil || !info.Mode().IsRegular() {
		return nil, time.Time{}
	}
	knownBad := access.bad && access.matches(info)
	if knownBad || info.Size() < headerSize || info.Size() > maxEntry+headerSize {
		s.removeBrokenEntry(path, file, info, knownBad)
		return nil, time.Time{}
	}
	b, err := os.ReadFile(path)
	if err != nil {
		return nil, time.Time{}
	}
	if len(b) < headerSize {
		s.removeBrokenEntry(path, file, info, false)
		return nil, time.Time{}
	}
	sum := sha256.Sum256(b[headerSize:])
	if string(b[:8]) != "MYCACHE1" || !bytes.Equal(sum[:], b[16:48]) {
		s.removeBrokenEntry(path, file, info, false)
		return nil, time.Time{}
	}
	now := time.Now()
	if !s.moving.Load() {
		if err := os.Chtimes(path, now, now); err != nil {
			s.noteAccess(file, entryAccess{info: info, used: now})
		} else if access.info != nil {
			s.noteAccess(file, entryAccess{})
		}
	}
	return b[headerSize:], time.Unix(0, int64(binary.LittleEndian.Uint64(b[8:16])))
}

func (s *Store) removeBrokenEntry(path, file string, info os.FileInfo, knownBad bool) {
	s.dirty.Store(true)
	if s.moving.Load() {
		return
	}
	if err := removeEntry(path); err != nil {
		if !knownBad {
			log.Printf("cache: remove corrupt entry: %v", err)
			s.noteAccess(file, entryAccess{info: info, bad: true})
		}
	} else {
		s.noteAccess(file, entryAccess{})
	}
}

// Readers share the I/O lock, so their exceptional state also takes mu.
func (s *Store) noteAccess(file string, access entryAccess) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if access.info == nil {
		delete(s.access, file)
		return
	}
	if s.access == nil {
		s.access = make(map[string]entryAccess)
	}
	s.access[file] = access
}

// hidden and replaced are called with mu held.
func (s *Store) hidden(file string) bool {
	all, clearedAll := s.uncleared[""]
	scope, clearedScope := s.uncleared[file[:digestLength]]
	return s.forgotten[file] != 0 || clearedAll && !all[file] || clearedScope && !scope[file]
}

func (s *Store) replaced(file string) {
	delete(s.forgotten, file)
	if all := s.uncleared[""]; all != nil {
		all[file] = true
	}
	if scope := s.uncleared[file[:digestLength]]; scope != nil {
		scope[file] = true
	}
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
	return s.prepare(scope, key, epoch, false)
}

// PrepareIfChanged reserves a write like Prepare, but keeps an identical
// file-backed snapshot in place. A missing file is still written.
func (s *Store) PrepareIfChanged(scope, key string, epoch uint64) func([]byte) {
	return s.prepare(scope, key, epoch, true)
}

func (s *Store) prepare(scope, key string, epoch uint64, skipEqual bool) func([]byte) {
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
	return func(b []byte) { s.write(file, b, epoch, seq, skipEqual) }
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
		var err error
		if s.index.accepts(file) {
			before := s.index.size
			err = s.index.forget(s.dir, file)
			s.indexError(err)
			s.prune(max(0, s.index.size-before), s.latest)
		} else {
			err = removeEntry(filepath.Join(s.dir, file))
		}
		if err != nil {
			log.Printf("cache: forget failed: %v", err)
			return
		}
		s.mu.Lock()
		delete(s.access, file)
		if s.forgotten[file] == seq {
			delete(s.forgotten, file)
		}
		s.mu.Unlock()
	}
}

func (s *Store) write(file string, b []byte, epoch, seq uint64, skipEqual bool) {
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
				s.replaced(file)
			}
			s.mu.Unlock()
			s.scheduleIndex()
			s.prune(max(0, s.index.size-before), s.latest)
		}
		return
	}
	dest := filepath.Join(s.dir, file)
	sum := sha256.Sum256(b)
	if skipEqual && snapshotMatches(dest, len(b), sum) {
		s.mu.Lock()
		forgotten := s.hidden(file)
		s.mu.Unlock()
		if !forgotten {
			return
		}
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
	committed := s.pending[file] == seq && s.Epoch() == epoch && !s.clearing.Load() && os.Rename(path, dest) == nil
	if committed {
		s.replaced(file)
		delete(s.access, file)
	}
	s.mu.Unlock()
	if committed {
		s.latest = file
		s.prune(int64(headerSize+len(b)), file)
	}
}

func snapshotMatches(path string, size int, sum [sha256.Size]byte) bool {
	f, err := os.Open(path)
	if err != nil {
		return false
	}
	defer f.Close()
	info, err := f.Stat()
	if err != nil || info.Size() != int64(headerSize+size) {
		return false
	}
	var header [headerSize]byte
	_, err = io.ReadFull(f, header[:])
	return err == nil && string(header[:8]) == "MYCACHE1" && bytes.Equal(header[16:], sum[:])
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
		prefix := ""
		if scope != "" {
			prefix = digest(scope)
		}
		entries, err := os.ReadDir(s.dir)
		if errors.Is(err, os.ErrNotExist) {
			err = nil
		}
		if s.index != nil && scope != "" && s.index.scopes[prefix] {
			err = errors.Join(err, s.index.clearScope(s.dir, scope))
		}
		for _, e := range entries {
			if !e.IsDir() && (scope == "" || strings.HasPrefix(e.Name(), prefix+"-")) {
				if removeErr := removeEntry(filepath.Join(s.dir, e.Name())); removeErr != nil {
					err = errors.Join(err, removeErr)
				} else {
					delete(s.access, e.Name())
				}
			}
		}
		if s.index != nil && scope == "" {
			s.index.reset()
			if err != nil {
				s.index.invalidate()
			}
		}
		if err != nil {
			log.Printf("cache: clear failed: %v", err)
		}
		s.spare = 0
		s.latest = ""
		s.mu.Lock()
		if err != nil {
			if scope == "" || s.uncleared == nil {
				s.uncleared = make(map[string]map[string]bool)
			}
			s.uncleared[prefix] = make(map[string]bool)
		} else if scope == "" {
			s.uncleared = nil
		} else {
			delete(s.uncleared, prefix)
		}
		s.clears--
		s.clearing.Store(s.clears != 0)
		s.mu.Unlock()
	}
}

func removeEntry(path string) error {
	err := os.Remove(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	return err
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
	entries, scanErr := os.ReadDir(s.dir)
	type candidate struct {
		os.FileInfo
		used time.Time
		bad  bool
	}
	var files []candidate
	var size int64
	var fixed int64 // the index cannot be evicted as an ordinary content file
	if s.index != nil {
		size = int64(len(s.index.pending))
		fixed = size
	}
	for _, e := range entries {
		if strings.HasPrefix(e.Name(), ".part-") {
			if err := removeEntry(filepath.Join(s.dir, e.Name())); err == nil {
				continue
			} else {
				log.Printf("cache: remove abandoned snapshot: %v", err)
			}
		}
		if i, err := e.Info(); err == nil && !i.IsDir() {
			if s.index == nil || e.Name() != IndexFile {
				f := candidate{FileInfo: i, used: i.ModTime()}
				if access := s.access[e.Name()]; access.matches(i) {
					f.bad = access.bad
					if f.bad && removeEntry(filepath.Join(s.dir, e.Name())) == nil {
						delete(s.access, e.Name())
						continue
					}
					if access.used.After(f.used) {
						f.used = access.used
					}
				}
				files = append(files, f)
			} else {
				fixed += i.Size()
			}
			size += i.Size()
		} else if err != nil && !errors.Is(err, os.ErrNotExist) {
			scanErr = errors.Join(scanErr, err)
		}
	}
	defer func() {
		s.spare = 0
		if scanErr == nil || errors.Is(scanErr, os.ErrNotExist) {
			s.spare = max(0, s.limit-size)
		}
	}()
	if size <= s.limit {
		return
	}
	sort.Slice(files, func(i, j int) bool {
		if files[i].bad != files[j].bad {
			return files[i].bad
		}
		if files[i].used.Equal(files[j].used) {
			return files[i].Name() < files[j].Name()
		}
		return files[i].used.Before(files[j].used)
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
		if removeEntry(filepath.Join(s.dir, f.Name())) == nil {
			delete(s.access, f.Name())
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
				s.latest = ""
				s.access = nil
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
