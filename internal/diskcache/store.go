// Package diskcache stores disposable, bounded snapshots. Call Read and Put
// off the UI thread; Queue only schedules a write of immutable bytes.
package diskcache

import (
	"bytes"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
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

type Store struct {
	forgotten map[string]bool
	dir       string
	limit     int64
	io        sync.Mutex
	mu        sync.Mutex
	pending   map[string]uint64
	serial    uint64
	epoch     atomic.Uint64
	clearing  atomic.Bool
	closed    atomic.Bool
	workers   sync.WaitGroup
}

func New(dir string, limit int64) *Store {
	return &Store{dir: dir, limit: limit, pending: map[string]uint64{}, forgotten: map[string]bool{}}
}
func (s *Store) Epoch() uint64 {
	if s == nil {
		return 0
	}
	return s.epoch.Load()
}
func digest(s string) string        { sum := sha256.Sum256([]byte(s)); return hex.EncodeToString(sum[:]) }
func name(scope, key string) string { return digest(scope) + "-" + digest(key) + ".entry" }

// Read verifies the complete payload before returning it. Corrupt and partial
// files are misses, never empty successful responses.
func (s *Store) Read(scope, key string) ([]byte, time.Time) {
	if s == nil || s.closed.Load() || s.clearing.Load() {
		return nil, time.Time{}
	}
	s.io.Lock()
	defer s.io.Unlock()
	file := name(scope, key)
	s.mu.Lock()
	forgotten := s.forgotten[file]
	s.mu.Unlock()
	if forgotten {
		return nil, time.Time{}
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
		os.Remove(path)
		return nil, time.Time{}
	}
	now := time.Now()
	os.Chtimes(path, now, now)
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
	if s == nil || s.closed.Load() || s.clearing.Load() || s.Epoch() != epoch {
		return nil
	}
	file := name(scope, key)
	s.mu.Lock()
	if s.closed.Load() || len(s.pending) >= 32 && s.pending[file] == 0 {
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

func (s *Store) Forget(scope, key string) {
	if s == nil || s.closed.Load() {
		return
	}
	s.epoch.Add(1)
	file := name(scope, key)
	s.mu.Lock()
	s.forgotten[file] = true
	s.mu.Unlock()
	go func() {
		s.io.Lock()
		os.Remove(filepath.Join(s.dir, file))
		s.mu.Lock()
		delete(s.forgotten, file)
		s.mu.Unlock()
		s.io.Unlock()
	}()
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
	if !current() || s.limit <= 0 || len(b) > maxEntry {
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
	if os.Rename(path, filepath.Join(s.dir, file)) == nil {
		s.prune()
	}
}

// Clear invalidates requests immediately; deletion and accounting stay off
// the UI thread. Empty scope clears everything, otherwise just that account.
func (s *Store) Clear(scope string, done func()) {
	if s == nil {
		if done != nil {
			done()
		}
		return
	}
	s.epoch.Add(1)
	s.clearing.Store(true)
	go func() {
		s.io.Lock()
		entries, _ := os.ReadDir(s.dir)
		for _, e := range entries {
			if !e.IsDir() && (scope == "" || strings.HasPrefix(e.Name(), digest(scope)+"-")) {
				os.Remove(filepath.Join(s.dir, e.Name()))
			}
		}
		s.clearing.Store(false)
		s.io.Unlock()
		if done != nil {
			done()
		}
	}()
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
	return n
}

// prune runs under the I/O lock, evicting least recently read files. A batch
// leaves headroom, so a directory is not sorted after every small write.
func (s *Store) prune() {
	entries, _ := os.ReadDir(s.dir)
	var files []os.FileInfo
	var size int64
	for _, e := range entries {
		if strings.HasPrefix(e.Name(), ".part-") {
			os.Remove(filepath.Join(s.dir, e.Name()))
			continue
		}
		if i, err := e.Info(); err == nil && !i.IsDir() {
			files = append(files, i)
			size += i.Size()
		}
	}
	if size <= s.limit {
		return
	}
	sort.Slice(files, func(i, j int) bool { return files[i].ModTime().Before(files[j].ModTime()) })
	for _, f := range files {
		if size <= s.limit*3/4 {
			break
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
}
