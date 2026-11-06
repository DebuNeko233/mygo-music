package player

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"slices"
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
	limit int64 // in bytes; 0 keeps nothing
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
	c.limit = limit
	blocked := c.blocked
	c.mu.Unlock()
	if !blocked {
		c.prune("")
	}
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
		key := cacheKey(id, lv)
		meta, err := os.ReadFile(filepath.Join(c.dir, key+".json"))
		if err != nil {
			continue
		}
		var kept keptSong
		if json.Unmarshal(meta, &kept) != nil || !keepable(lv, kept.Type) {
			continue
		}
		if i < at && kept.Asked != level {
			continue
		}
		st := kept.Stream
		path := filepath.Join(c.dir, key+"."+st.Type)
		if kept.File != "" {
			path = filepath.Join(c.dir, kept.File)
		}
		f, err := os.Open(path)
		if err != nil {
			continue
		}
		if info, err := f.Stat(); err != nil || info.Size() != st.Size {
			f.Close()
			continue
		}
		// Played now: the last to go.
		now := time.Now()
		os.Chtimes(path, now, now)
		return c.track(opened(f, st.Size)), &st, true
	}
	return nil, nil, false
}

// download fetches the song of the stream st, asked for at level, into a
// file, to keep once it has all come in; or into memory, if the cache would
// not keep it.
func (c *Cache) download(id int64, level string, st *netease.Stream) *download {
	part := c.part(st)
	if part == nil {
		return c.track(startDownload(st.URL, st.Size, nil, nil))
	}
	return c.track(startDownload(st.URL, st.Size, part, func(path string, n int64) (string, bool) {
		return c.keep(id, level, st, path, n)
	}))
}

// Keeps reports whether the cache would keep a song of size bytes: one of
// more than a quarter of its limit would push out too many, and none of a
// cache turned off.
func (c *Cache) Keeps(size int64) bool {
	if c == nil {
		return false
	}
	c.mu.Lock()
	limit := c.limit
	c.mu.Unlock()
	return limit > 0 && size <= limit/4
}

// Holds reports whether the cache would keep the song st streams: one of a
// level and a kind it keeps, of a size it has room for.
func (c *Cache) Holds(st *netease.Stream) bool {
	if st == nil || !c.Keeps(st.Size) {
		return false
	}
	ext := st.Type
	if ext == "" {
		ext = "mp3"
	}
	return keepable(st.Level, ext)
}

// part makes a file for a song of the stream st to come into, or returns
// nil if the cache would not keep it. Each has a name of its own: the same
// song played again while it still comes in has another.
func (c *Cache) part(st *netease.Stream) *os.File {
	if !c.Keeps(st.Size) || c.stopped() {
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
	limit, blocked := c.limit, c.blocked
	c.mu.Unlock()
	if blocked || n > limit/4 {
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
	path := filepath.Join(c.dir, key+"."+kept.Type)
	if os.Rename(part, path) != nil {
		// Windows replaces no file another stream still reads: rather than
		// lose the song, it is kept under a name of its own, which its
		// metadata names, and the one it could not take goes when that
		// stream lets go of it.
		aside := fmt.Sprintf("%s-%d.%s", key, time.Now().UnixNano(), kept.Type)
		path = filepath.Join(c.dir, aside)
		if os.Rename(part, path) != nil {
			return part, false
		}
		kept.File = aside
	}
	meta, _ := json.Marshal(kept)
	os.WriteFile(filepath.Join(c.dir, key+".json"), meta, 0o600)
	c.prune(path)
	return path, true
}

// Size returns how many bytes the songs kept take.
func (c *Cache) Size() int64 {
	if c.stopped() {
		return 0
	}
	var n int64
	for _, f := range c.songs() {
		n += f.size
	}
	return n
}

// Clear drops every song kept.
func (c *Cache) Clear() {
	if c.stopped() {
		return
	}
	for _, f := range c.songs() {
		c.remove(f.path)
	}
}

type cachedSong struct {
	path string
	size int64
	used time.Time
}

func (c *Cache) songs() []cachedSong {
	entries, _ := os.ReadDir(c.dir)
	var out []cachedSong
	for _, e := range entries {
		ext := filepath.Ext(e.Name())
		if ext != ".mp3" && ext != ".flac" {
			continue
		}
		info, err := e.Info()
		if err != nil {
			continue
		}
		out = append(out, cachedSong{filepath.Join(c.dir, e.Name()), info.Size(), info.ModTime()})
	}
	return out
}

func (c *Cache) remove(path string) {
	os.Remove(path)
	// For aside files (key-nanos.ext), the metadata is stored in key.json
	// We need to find the base key and remove its metadata
	base := filepath.Base(path)
	ext := filepath.Ext(base)
	name := strings.TrimSuffix(base, ext)
	// Check if this is an aside file (contains a timestamp suffix)
	if idx := strings.LastIndex(name, "-"); idx > 0 {
		// Extract the base key (everything before the last dash)
		baseKey := name[:idx]
		// Check if the remainder is a timestamp (numeric)
		if _, err := fmt.Sscanf(name[idx+1:], "%d", new(int64)); err == nil {
			// This is an aside file, remove the base key's metadata
			os.Remove(filepath.Join(filepath.Dir(path), baseKey+".json"))
			return
		}
	}
	// Normal file, remove its own metadata
	os.Remove(strings.TrimSuffix(path, filepath.Ext(path)) + ".json")
}

// prune drops the songs played longest ago until the rest fit the limit,
// but keep, the song just kept.
func (c *Cache) prune(keep string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.dropParts()
	songs := c.songs()
	slices.SortFunc(songs, func(a, b cachedSong) int { return a.used.Compare(b.used) })
	var total int64
	for _, f := range songs {
		total += f.size
	}
	for _, f := range songs {
		if total <= c.limit {
			return
		}
		if f.path != keep {
			c.remove(f.path)
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
			os.Remove(path)
		}
	}
}
