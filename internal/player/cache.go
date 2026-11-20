package player

import (
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"sync"
	"time"

	"netease-music-mygo/internal/netease"
)

// Cache keeps the songs played on disk, so that they play again at once,
// and without the network: each as <id>-<level>.mp3 or .flac, with what
// its stream was beside it in <id>-<level>.json. Once they take more than
// the limit, those played longest ago go first.
type Cache struct {
	dir string

	mu    sync.Mutex
	limit int64                  // in bytes; 0 keeps nothing
	spare int64                  // conservative headroom measured by the last eviction scan
	used  map[string]time.Time   // in-process access times when the filesystem rejects Chtimes
	bad   map[string]os.FileInfo // corrupt audio that could not yet be retired
	// blocked is set while the folder is being moved: nothing is looked up,
	// fetched into it or dropped, and what is in goes when it does.
	blocked bool
	// active counts what is being fetched into the folder, so that a move
	// may stop every one of them first.
	active map[*download]struct{}
}

// NewCache returns a cache in dir of limit bytes.
func NewCache(dir string, limit int64) *Cache {
	return &Cache{dir: dir, limit: limit}
}

// stopped reports whether the folder is being moved, and so takes nothing.
func (c *Cache) stopped() bool {
	if c == nil {
		return true
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.blocked
}

// SetLimit changes how much the cache may take, and drops what is over.
func (c *Cache) SetLimit(limit int64) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.limit = limit
	c.spare = 0
	c.pruneLocked("", 0)
}

func cacheKey(id int64, level string) string { return fmt.Sprintf("%d-%s", id, level) }

// keptSong is what a kept song's .json holds: where it streams from, at the
// level NetEase served, and the level it was asked for, which may be above
// that one.
type keptSong struct {
	netease.Stream
	Asked string `json:"asked,omitempty"`
	// File is the name it is kept under, when that is not the one its key
	// names: a copy kept beside one another stream still reads, which
	// Windows lets no file replace.
	File string `json:"file,omitempty"`
}

func (s keptSong) audioName(key string) string {
	if s.File == "" {
		return key + "." + s.Type
	}
	stamp, prefixed := strings.CutPrefix(s.File, key+"-")
	stamp, suffixed := strings.CutSuffix(stamp, "."+s.Type)
	if _, err := strconv.ParseInt(stamp, 10, 64); !prefixed || !suffixed || err != nil {
		return ""
	}
	return s.File
}

// keepable reports whether a song of level and type ext may be kept: they
// name its file, so only those known, which keep it in the cache's folder.
func keepable(level, ext string) bool {
	return slices.Contains(netease.Levels, level) && (ext == "mp3" || ext == "flac")
}

// lookup returns a song kept at level or a better one, open to read, with
// its stream. One served below the level asked for is kept there, and is
// what asking for that level finds.
func (c *Cache) lookup(id int64, level string) (*download, *netease.Stream, bool) {
	if c == nil || c.stopped() {
		return nil, nil, false
	}
	at := max(slices.Index(netease.Levels, level), 0)
	for i, lv := range netease.Levels {
		asked := ""
		if i < at {
			asked = level
		}
		if dl, info := c.openKept(id, lv, asked); dl != nil {
			return dl, info, true
		}
	}
	return nil, nil, false
}

// lookupOffline prefers the selected quality, then the best complete copy.
func (c *Cache) lookupOffline(id int64, level string) (*download, *netease.Stream, error) {
	if !c.stopped() {
		if slices.Contains(netease.Levels, level) {
			if dl, info := c.openKept(id, level, ""); dl != nil {
				return dl, info, nil
			}
		}
		for _, lv := range slices.Backward(netease.Levels) {
			if lv == level {
				continue
			}
			if dl, info := c.openKept(id, lv, ""); dl != nil {
				return dl, info, nil
			}
		}
	}
	return nil, nil, netease.ErrOffline
}

// openKept opens a complete file at level, requiring a request for asked if set.
func (c *Cache) openKept(id int64, level, asked string) (*download, *netease.Stream) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.blocked {
		return nil, nil
	}
	key := cacheKey(id, level)
	meta, err := os.ReadFile(filepath.Join(c.dir, key+".json"))
	if err != nil {
		return nil, nil
	}
	var kept keptSong
	if json.Unmarshal(meta, &kept) != nil || !keepable(level, kept.Type) || (asked != "" && kept.Asked != asked) {
		return nil, nil
	}
	st := kept.Stream
	name := kept.audioName(key)
	if name == "" {
		return nil, nil
	}
	path := filepath.Join(c.dir, name)
	f, err := os.Open(path)
	if err != nil {
		return nil, nil
	}
	if info, err := f.Stat(); err != nil || info.Size() != st.Size || os.SameFile(c.bad[path], info) {
		f.Close()
		return nil, nil
	}
	// Played now: the last to go.
	now := time.Now()
	if err := os.Chtimes(path, now, now); err != nil {
		if c.used == nil {
			c.used = make(map[string]time.Time)
		}
		c.used[path] = now
	} else {
		delete(c.used, path)
	}
	d := opened(f, st.Size)
	c.trackLocked(d)
	return d, &st
}

// download fetches the song of the stream st, asked for at level, into a
// cache file, to keep once it has all come in. Uncached downloads use a
// bounded memory buffer and spill larger responses into a temporary file.
func (c *Cache) download(id int64, level string, st *netease.Stream) *download {
	if c == nil {
		return startDownload(st.URL, st.Size, nil, nil)
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	d := newDownload(st.URL, st.Size, c.part(st), func(path string, n int64) (string, bool) {
		return c.keep(id, level, st, path, n)
	})
	c.trackLocked(d)
	d.start()
	return d
}

// part makes a file for a song of the stream st to come into, or returns
// nil if the cache would not keep it. Each has a name of its own: the same
// song played again while it still comes in has another. The caller holds mu.
func (c *Cache) part(st *netease.Stream) *os.File {
	if c.blocked || c.limit <= 0 || st.Size > c.limit/4 {
		return nil
	}
	if os.MkdirAll(c.dir, 0o700) != nil {
		return nil
	}
	f, err := os.CreateTemp(c.dir, "*.part")
	if err != nil {
		return nil
	}
	return f
}

// keep keeps the song of the stream st, n bytes, from the file part, which
// is closed, and drops what is over the limit then. It returns where the
// song is: kept, or still at part. asked is the level it was asked for,
// kept beside the one served so that asking for it again finds it.
func (c *Cache) keep(id int64, asked string, st *netease.Stream, part string, n int64) (string, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.blocked || c.limit <= 0 || n > c.limit/4 {
		return part, false
	}
	kept := keptSong{Stream: *st, Asked: asked}
	if kept.Type == "" {
		kept.Type = "mp3"
	}
	kept.Size = n
	if !keepable(kept.Level, kept.Type) {
		return part, false
	}
	key := cacheKey(id, kept.Level)
	metaPath := filepath.Join(c.dir, key+".json")
	previous, _ := os.ReadFile(metaPath)
	path := filepath.Join(c.dir, key+"."+kept.Type)
	if _, err := os.Lstat(path); !errors.Is(err, os.ErrNotExist) {
		// Keep the old audio readable until its metadata is replaced. This
		// also works when Windows has an active reader of the original file.
		kept.File = fmt.Sprintf("%s-%d.%s", key, time.Now().UnixNano(), kept.Type)
		path = filepath.Join(c.dir, kept.File)
		if _, err := os.Lstat(path); !errors.Is(err, os.ErrNotExist) {
			return part, false
		}
	}
	meta, err := json.Marshal(kept)
	if err != nil {
		log.Printf("song cache: encode metadata: %v", err)
		return part, false
	}
	tmp, err := os.CreateTemp(c.dir, ".song-meta-*")
	if err != nil {
		log.Printf("song cache: stage metadata: %v", err)
		return part, false
	}
	defer removeCacheFile(tmp.Name())
	_, err = tmp.Write(meta)
	if err = errors.Join(err, tmp.Close()); err != nil {
		log.Printf("song cache: write metadata: %v", err)
		return part, false
	}
	if err := os.Rename(part, path); err != nil {
		return part, false
	}
	if err := os.Rename(tmp.Name(), metaPath); err != nil {
		log.Printf("song cache: publish metadata: %v", err)
		if err := os.Rename(path, part); err != nil {
			// Playback can still use this copy and remove it when closed.
			return path, false
		}
		return part, false
	}
	delete(c.used, path)
	delete(c.bad, path)
	var old keptSong
	if json.Unmarshal(previous, &old) == nil && keepable(kept.Level, old.Type) {
		if name := old.audioName(key); name != "" && name != filepath.Base(path) {
			c.remove(filepath.Join(c.dir, name))
		}
	}
	c.pruneLocked(path, n)
	return path, true
}

// Size returns how many bytes the songs kept take.
func (c *Cache) Size() int64 {
	if c == nil {
		return 0
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.blocked {
		return 0
	}
	var n int64
	songs, _ := c.songs()
	for _, f := range songs {
		n += f.size
	}
	return n
}

// Clear drops every song kept.
func (c *Cache) Clear() {
	if c == nil {
		return
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.blocked {
		return
	}
	c.spare = 0
	c.dropParts()
	songs, _ := c.songs()
	for _, f := range songs {
		c.remove(f.path)
	}
}

type cachedSong struct {
	path string
	size int64
	used time.Time
	bad  bool
}

func (c *Cache) songs() ([]cachedSong, error) {
	entries, err := os.ReadDir(c.dir)
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	var out []cachedSong
	for _, e := range entries {
		ext := filepath.Ext(e.Name())
		if !e.Type().IsRegular() || (ext != ".mp3" && ext != ".flac") {
			continue
		}
		info, statErr := e.Info()
		if statErr != nil {
			if !errors.Is(statErr, os.ErrNotExist) {
				err = errors.Join(err, statErr)
			}
			continue
		}
		path := filepath.Join(c.dir, e.Name())
		used := info.ModTime()
		if recent := c.used[path]; recent.After(used) {
			used = recent
		}
		out = append(out, cachedSong{path, info.Size(), used, os.SameFile(c.bad[path], info)})
	}
	return out, err
}

func (c *Cache) remove(path string) bool {
	if !removeCacheFile(path) {
		return false
	}
	delete(c.used, path)
	delete(c.bad, path)
	removeSongMetadata(path)
	return true
}

// Copies of the same quality share a sidecar. Only its current audio file
// may invalidate it. Cache callers hold mu through this check and removal.
func removeSongMetadata(path string) {
	base := filepath.Base(path)
	key := strings.TrimSuffix(base, filepath.Ext(base))
	if prefix, suffix, ok := strings.Cut(key, "-"); ok {
		if level, stamp, ok := strings.Cut(suffix, "-"); ok {
			if _, err := strconv.ParseInt(stamp, 10, 64); err == nil {
				key = prefix + "-" + level
			}
		}
	}
	meta := filepath.Join(filepath.Dir(path), key+".json")
	b, err := os.ReadFile(meta)
	var kept keptSong
	if err == nil && json.Unmarshal(b, &kept) == nil && kept.audioName(key) == base {
		removeCacheFile(meta)
	}
}

func removeCacheFile(path string) bool {
	if err := os.Remove(path); err != nil && !errors.Is(err, os.ErrNotExist) {
		log.Printf("song cache: remove: %v", err)
		return false
	}
	return true
}

func (c *Cache) pruneLocked(keep string, added int64) {
	if c.blocked {
		return
	}
	if c.spare > 0 && added <= c.spare {
		c.spare -= added
		return
	}
	c.dropParts()
	songs, scanErr := c.songs()
	var total int64
	songs = slices.DeleteFunc(songs, func(f cachedSong) bool {
		if f.bad && f.path != keep && c.remove(f.path) {
			return true
		}
		total += f.size
		return false
	})
	defer func() {
		if scanErr == nil {
			c.spare = max(0, c.limit-total)
		}
	}()
	if total <= c.limit {
		return
	}
	slices.SortFunc(songs, func(a, b cachedSong) int { return a.used.Compare(b.used) })
	for _, f := range songs {
		if total <= c.limit {
			return
		}
		if f.path != keep && c.remove(f.path) {
			total -= f.size
		}
	}
}

// dropParts removes the files of songs that never all came in, left by an
// app that quit as they came: one coming in is written to every moment.
func (c *Cache) dropParts() {
	parts, _ := filepath.Glob(filepath.Join(c.dir, "*.part"))
	for _, path := range parts {
		if info, err := os.Stat(path); err == nil && time.Since(info.ModTime()) > time.Hour {
			removeCacheFile(path)
		}
	}
}
