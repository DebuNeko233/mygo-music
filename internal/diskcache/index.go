package diskcache

import (
	"bufio"
	"bytes"
	"container/list"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// IndexFile is the only metadata file copied with an indexed cache.
const IndexFile = "index.jsonl"
const maxIndexValue = 16 << 10
const maxIndexLine = 32 << 10
const maxIndexEntries = 8192
const indexBatch = 16 << 10
const indexDelay = 250 * time.Millisecond

// indexLog is owned by Store.io: replay, append and compaction cannot race.
// Keys are already digests. Raw request URLs and credentials never enter it.
// The bounded map serves lookups; the log records both replacements and deletes.
// Only a truncated last line is recoverable. Skipping a corrupt middle line
// could skip a tombstone and resurrect an entry, so that invalidates the log.
type indexLog struct {
	scopes              map[string]bool
	loaded, corrupt     bool
	entries             map[string]*list.Element
	order               list.List
	live, size          int64
	lines               int
	pending             []byte
	timer               *time.Timer
	batch               uint64
	liveLimit, logLimit int64
}

type indexRecord struct {
	Version int             `json:"v"`
	Op      string          `json:"op"`
	Key     string          `json:"key"`
	At      int64           `json:"at,omitempty"`
	Value   json.RawMessage `json:"value,omitempty"`
}
type indexLine struct {
	Record json.RawMessage `json:"record"`
	Sum    string          `json:"sha256"`
}
type indexValue struct {
	record indexRecord
	bytes  int
}

// NewIndexed keeps the named scopes as JSON values in one append-only index.
// Other scopes retain ordinary snapshot files (for example image content).
// Loading is lazy and belongs on a worker, just like Read and Put. It does not
// import individual index entries created by earlier development formats.
func NewIndexed(dir string, limit int64, scopes ...string) *Store {
	s := New(dir, limit)
	s.index = &indexLog{scopes: map[string]bool{}, liveLimit: min(4<<20, max(0, limit/8)), logLimit: min(8<<20, max(0, limit/4))}
	for _, scope := range scopes {
		s.index.scopes[digest(scope)] = true
	}
	return s
}
func (j *indexLog) accepts(key string) bool {
	return j != nil && len(key) == 135 && key[64] == '-' && strings.HasSuffix(key, ".entry") && j.scopes[key[:64]]
}
func (j *indexLog) reset() {
	j.batch++
	if j.timer != nil {
		j.timer.Stop()
		j.timer = nil
	}
	j.loaded, j.corrupt = false, false
	j.entries = nil
	j.order.Init()
	j.live, j.size, j.lines = 0, 0, 0
	j.pending = nil
}
func (j *indexLog) apply(r indexRecord, size int) {
	if old := j.entries[r.Key]; old != nil {
		v := old.Value.(indexValue)
		j.live -= int64(v.bytes)
		j.order.Remove(old)
		delete(j.entries, r.Key)
	}
	if r.Op == "put" {
		j.entries[r.Key] = j.order.PushBack(indexValue{r, size})
		j.live += int64(size)
	}
}
func encodeIndex(r indexRecord) ([]byte, error) {
	b, err := json.Marshal(r)
	if err != nil {
		return nil, err
	}
	sum := sha256.Sum256(b)
	line, err := json.Marshal(indexLine{b, hex.EncodeToString(sum[:])})
	if err != nil {
		return nil, err
	}
	return append(line, '\n'), nil
}
func (j *indexLog) decode(b []byte) (indexRecord, error) {
	var line indexLine
	var r indexRecord
	if json.Unmarshal(b, &line) != nil {
		return r, errors.New("invalid index envelope")
	}
	sum := sha256.Sum256(line.Record)
	if line.Sum != hex.EncodeToString(sum[:]) || json.Unmarshal(line.Record, &r) != nil || r.Version != 1 || !j.accepts(r.Key) {
		return r, errors.New("invalid index record")
	}
	if _, err := hex.DecodeString(r.Key[:64] + r.Key[65:129]); err != nil {
		return r, err
	}
	if r.Op != "del" && (r.Op != "put" || r.At <= 0 || len(r.Value) == 0 || len(r.Value) > maxIndexValue) {
		return r, errors.New("invalid index operation")
	}
	return r, nil
}
func (j *indexLog) load(dir string) error {
	if j.loaded {
		return nil
	}
	j.loaded = true
	j.entries = map[string]*list.Element{}
	path := filepath.Join(dir, IndexFile)
	f, err := os.Open(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		j.corrupt = true
		return err
	}
	info, err := f.Stat()
	if err != nil {
		f.Close()
		j.corrupt = true
		return err
	}
	reader := bufio.NewReaderSize(f, maxIndexLine)
	valid := int64(0)
	for info.Size() <= j.logLimit+j.liveLimit+maxIndexLine {
		line, readErr := reader.ReadSlice('\n')
		if readErr == io.EOF {
			break
		} // The incomplete tail is discarded before appending.
		if readErr != nil {
			j.corrupt = true
			break
		}
		r, decodeErr := j.decode(line)
		if decodeErr != nil {
			j.corrupt = true
			break
		}
		j.apply(r, len(line))
		j.lines++
		valid += int64(len(line))
		// A damaged or externally enlarged file must not grow the replay map.
		for j.live > j.liveLimit || len(j.entries) > maxIndexEntries {
			old := j.order.Front()
			if old == nil {
				break
			}
			j.apply(indexRecord{Op: "del", Key: old.Value.(indexValue).record.Key}, 0)
		}
	}
	f.Close()
	if info.Size() > j.logLimit+j.liveLimit+maxIndexLine {
		j.corrupt = true
	}
	if j.corrupt {
		j.entries = map[string]*list.Element{}
		j.order.Init()
		j.live = 0
		j.lines = 0
		j.size = 0
		return j.compact(dir)
	}
	j.size = valid
	if valid != info.Size() {
		if err := os.Truncate(path, valid); err != nil {
			j.corrupt = true
			return err
		}
	}
	if j.size > j.logLimit || j.lines > 2*len(j.entries)+64 {
		return j.compact(dir)
	}
	return nil
}
func (j *indexLog) read(dir, key string) ([]byte, time.Time) {
	if j.load(dir) != nil {
		return nil, time.Time{}
	}
	e := j.entries[key]
	if e == nil {
		return nil, time.Time{}
	}
	j.order.MoveToBack(e)
	r := e.Value.(indexValue).record
	return bytes.Clone(r.Value), time.Unix(0, r.At)
}
func (j *indexLog) enqueue(r indexRecord) error {
	b, err := encodeIndex(r)
	if err != nil {
		return err
	}
	j.pending = append(j.pending, b...)
	j.size += int64(len(b))
	j.lines++
	j.apply(r, len(b))
	return nil
}
func (j *indexLog) put(dir, key string, b []byte) (bool, error) {
	if len(b) > maxIndexValue {
		return false, nil
	}
	if !json.Valid(b) {
		return false, errors.New("index value is not JSON")
	}
	if err := j.load(dir); err != nil {
		return false, err
	}
	r := indexRecord{Version: 1, Op: "put", Key: key, At: time.Now().UnixNano(), Value: bytes.Clone(b)}
	encoded, err := encodeIndex(r)
	if err != nil {
		return false, err
	}
	if len(encoded) > maxIndexLine || int64(len(encoded)) > j.liveLimit {
		return false, nil
	}
	if err := j.enqueue(r); err != nil {
		return false, err
	}
	trimmed := false
	if j.live > j.liveLimit || len(j.entries) > maxIndexEntries {
		for j.live > j.liveLimit*3/4 || len(j.entries) > maxIndexEntries*3/4 {
			old := j.order.Front()
			if old == nil {
				break
			}
			if err := j.enqueue(indexRecord{Version: 1, Op: "del", Key: old.Value.(indexValue).record.Key}); err != nil {
				return true, err
			}
			trimmed = true
		}
	}
	if trimmed || j.corrupt || j.size > j.logLimit || j.lines > 2*len(j.entries)+64 {
		return true, j.compact(dir)
	}
	if len(j.pending) >= indexBatch {
		return true, j.flush(dir)
	}
	return true, nil
}
func (j *indexLog) forget(dir, key string) error {
	if err := j.load(dir); err != nil {
		return err
	}
	if j.entries[key] == nil {
		return nil
	}
	if err := j.enqueue(indexRecord{Version: 1, Op: "del", Key: key}); err != nil {
		return err
	}
	// Refused/missing resources must not reappear after a successful deletion.
	if j.corrupt || j.size > j.logLimit || j.lines > 2*len(j.entries)+64 {
		return j.compact(dir)
	}
	return j.flush(dir)
}
func (j *indexLog) clearScope(dir, scope string) error {
	if err := j.load(dir); err != nil {
		return err
	}
	prefix := digest(scope) + "-"
	for key := range j.entries {
		if strings.HasPrefix(key, prefix) {
			if err := j.enqueue(indexRecord{Version: 1, Op: "del", Key: key}); err != nil {
				return err
			}
		}
	}
	if err := j.flush(dir); err != nil {
		return err
	}
	return j.compact(dir)
}
func (j *indexLog) flush(dir string) (err error) {
	defer func() {
		if err != nil {
			j.needRepair()
		}
	}()
	if j.timer != nil {
		j.timer.Stop()
		j.timer = nil
	}
	if j.corrupt {
		return j.compact(dir)
	}
	if len(j.pending) == 0 {
		return nil
	}
	if err := os.MkdirAll(dir, 0700); err != nil {
		return err
	}
	f, err := os.OpenFile(filepath.Join(dir, IndexFile), os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0600)
	if err != nil {
		return err
	}
	n, err := f.Write(j.pending)
	if err == nil && n != len(j.pending) {
		err = io.ErrShortWrite
	}
	if err == nil {
		err = f.Sync()
	}
	err = errors.Join(err, f.Close())
	if err != nil {
		j.corrupt = true
		return err
	} // A partial append is repaired by atomic rewrite.
	j.pending = nil
	return nil
}
func (j *indexLog) compact(dir string) (err error) {
	defer func() {
		if err != nil {
			j.needRepair()
		}
	}()
	if j.timer != nil {
		j.timer.Stop()
		j.timer = nil
	}
	if err := os.MkdirAll(dir, 0700); err != nil {
		return err
	}
	f, err := os.CreateTemp(dir, ".part-index-")
	if err != nil {
		return err
	}
	path := f.Name()
	defer os.Remove(path)
	writer := bufio.NewWriter(f)
	size := int64(0)
	for e := j.order.Front(); e != nil; e = e.Next() {
		b, encodeErr := encodeIndex(e.Value.(indexValue).record)
		if encodeErr != nil {
			err = encodeErr
			break
		}
		if _, err = writer.Write(b); err != nil {
			break
		}
		size += int64(len(b))
	}
	if err == nil {
		err = writer.Flush()
	}
	if err == nil {
		err = f.Sync()
	}
	err = errors.Join(err, f.Close())
	if err != nil {
		return err
	}
	// No append handle stays open, including on Windows. The old complete log
	// survives a failed publication; the bounded map remains available to retry.
	if err := os.Rename(path, filepath.Join(dir, IndexFile)); err != nil {
		return err
	}
	j.pending = nil
	j.size = size
	j.lines = len(j.entries)
	j.corrupt = false
	return nil
}

// A single delayed flush batches downloads without keeping an idle poll alive.
// Close/Suspend flush synchronously; stale timer callbacks carry no payload.
func (s *Store) scheduleIndex() {
	j := s.index
	if len(j.pending) == 0 || j.timer != nil || s.closed.Load() || s.moving.Load() {
		return
	}
	j.batch++
	batch := j.batch
	j.timer = time.AfterFunc(indexDelay, func() { s.flushIndex(batch) })
}

func (s *Store) flushIndex(batch uint64) {
	s.io.Lock()
	defer s.io.Unlock()
	j := s.index
	if j.batch != batch || j.timer == nil || s.closed.Load() || s.moving.Load() {
		return
	}
	j.timer.Stop()
	j.timer = nil
	s.indexError(j.flush(s.dir))
}

// If storage is unavailable, retain only the bounded current map. Accumulating
// failed append batches would grow RAM indefinitely. The next successful write
// atomically rebuilds the log from that map instead of appending to a torn log.
func (j *indexLog) needRepair() {
	j.corrupt = true
	j.pending = nil
	j.size, j.lines = 0, 0
}
