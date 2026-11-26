/*
按字节预算缓存自选背景图，优先加载当前目标并保留加载期间的旧图。
*/

package artwork

import (
	"slices"
	"time"
	"weak"
)

const pictureCacheLimit = 64 << 20
const pictureCacheEntries = 128
const pictureLoadJobs = 2

// PictureSlot belongs to one background surface. Visible protects the shown
// image; target changes never remove it before its replacement is accepted.
type PictureSlot struct {
	Visible       bool
	paths         []string
	key           int64
	next          int64
	hasNext       bool
	attempt       int
	target, shown string
}

// Post dispatches to the owner thread. PostResource must execute exactly one
// callback: publish on that thread, or discard if dispatch has closed.
type PictureCacheOptions struct {
	Post         func(func())
	PostResource func(func(), func())
	Load         func(string) *Picture
	Limit        int
}

type pictureEntry struct {
	picture         *Picture
	loading, failed bool
	bytes, blocked  int
	used            uint64
}

// PictureCache is owned by the UI thread. Only the two admitted workers read
// files. The budget includes encoded data and estimated bitmap/mipmap storage;
// decoder scratch space and in-flight renderer frames are separate.
type PictureCache struct {
	entries      map[string]*pictureEntry
	slots        []*PictureSlot
	bytes, limit int
	tick         uint64
	jobs         chan struct{}
	done         chan struct{}
	closed       bool
	options      PictureCacheOptions
}

func NewPictureCache(options PictureCacheOptions) *PictureCache {
	if options.Limit <= 0 {
		options.Limit = pictureCacheLimit
	}
	if options.Load == nil {
		options.Load = DecodePicture
	}
	return &PictureCache{entries: map[string]*pictureEntry{}, limit: options.Limit, jobs: make(chan struct{}, pictureLoadJobs), done: make(chan struct{}), options: options}
}

func pictureIndex(key int64, n int) int {
	if n == 0 {
		return 0
	}
	i := key % int64(n)
	if i < 0 {
		i = -i
	}
	return int(i)
}

// Select schedules the current image and, when there is room, the next song's
// image. Paths retain their configured order; failed targets advance at most
// once through that order. A missing replacement keeps the previous picture.
func (c *PictureCache) Select(slot *PictureSlot, paths []string, key int64, next int64, hasNext bool) *Picture {
	if c.closed {
		return nil
	}
	if !slices.Contains(c.slots, slot) {
		c.slots = append(c.slots, slot)
	}
	if slot.key != key || !slices.Equal(slot.paths, paths) {
		slot.paths = slices.Clone(paths)
		slot.key = key
		slot.attempt = 0
		if !slices.Contains(paths, slot.shown) {
			slot.shown = ""
		}
	}
	slot.next, slot.hasNext = next, hasNext
	c.choose(slot)
	if e := c.entries[slot.target]; e != nil && e.picture != nil {
		slot.shown = slot.target
		c.touch(e)
	}
	c.pump()
	if e := c.entries[slot.shown]; e != nil && e.picture != nil {
		c.touch(e)
		return e.picture
	}
	// A newly exposed cold surface can keep the sibling's last background
	// while its own target waits for space. Empty/failed configurations do not.
	if slot.Visible && slot.target != "" {
		for _, other := range c.slots {
			if e := c.entries[other.shown]; e != nil && e.picture != nil {
				c.touch(e)
				return e.picture
			}
		}
	}
	return nil
}

func (c *PictureCache) touch(e *pictureEntry) { c.tick++; e.used = c.tick }

func (c *PictureCache) choose(s *PictureSlot) {
	s.target = ""
	for s.attempt < len(s.paths) {
		path := s.paths[(pictureIndex(s.key, len(s.paths))+s.attempt)%len(s.paths)]
		if e := c.entries[path]; path != "" && (e == nil || !e.failed) {
			s.target = path
			return
		}
		s.attempt++
	}
}

func (c *PictureCache) protected(path, incoming string) bool {
	for _, s := range c.slots {
		if s.Visible && s.shown == path && s.target != incoming {
			return true
		}
	}
	return false
}

func (c *PictureCache) canFit(path string, size int) bool {
	pinned := 0
	for k, e := range c.entries {
		if k != path && c.protected(k, path) {
			pinned += e.bytes
		}
	}
	return size <= c.limit-pinned
}

func (c *PictureCache) admit(path string, e *pictureEntry, pic *Picture) bool {
	size := pixelBytes(pic.Bitmap) + pic.encodedBytes
	if !c.canFit(path, size) {
		e.blocked = size
		return false
	}
	for c.bytes+size > c.limit {
		var oldest string
		var used uint64
		found := false
		for k, v := range c.entries {
			if k == path || v.picture == nil || c.protected(k, path) {
				continue
			}
			if !found || v.used < used {
				oldest, used, found = k, v.used, true
			}
		}
		if !found {
			return false
		}
		victim := c.entries[oldest]
		c.bytes -= victim.bytes
		victim.blocked, victim.bytes, victim.picture = victim.bytes, 0, nil
	}
	e.picture, e.bytes, e.blocked = pic, size, 0
	c.bytes += size
	c.touch(e)
	for _, s := range c.slots {
		if s.target == path {
			s.shown = path
		}
	}
	return true
}

func (c *PictureCache) request(path string, prefetch bool, estimate int) {
	if path == "" || len(c.jobs) == cap(c.jobs) {
		return
	}
	e := c.entries[path]
	if e != nil && (e.picture != nil || e.loading || e.failed) {
		return
	}
	if e == nil {
		e = &pictureEntry{}
		c.entries[path] = e
	}
	if e.blocked > 0 && !c.canFit(path, e.blocked) {
		return
	}
	if prefetch {
		if e.blocked > 0 {
			estimate = e.blocked
		}
		if estimate <= 0 || c.bytes+estimate > c.limit {
			return
		}
	}
	e.loading = true
	c.touch(e)
	c.jobs <- struct{}{}
	go func() {
		select {
		case <-c.done:
			<-c.jobs
			return
		default:
		}
		pic := c.options.Load(path)
		c.options.PostResource(func() {
			<-c.jobs
			if c.closed {
				return
			}
			if c.entries[path] != e {
				c.pump()
				return
			}
			e.loading = false
			wanted, target := false, false
			for _, s := range c.slots {
				if s.target == path {
					wanted, target = true, true
				}
				if c.nextPath(s) == path {
					wanted = true
				}
			}
			if !wanted {
				delete(c.entries, path)
				c.pump()
				return
			}
			if pic == nil {
				e.failed = true
				for _, s := range c.slots {
					if s.target == path {
						s.attempt++
						c.choose(s)
					}
				}
			} else if !target && c.bytes+pixelBytes(pic.Bitmap)+pic.encodedBytes > c.limit {
				e.blocked = pixelBytes(pic.Bitmap) + pic.encodedBytes
			} else if c.admit(path, e, pic) {
				ref := weak.Make(pic.Bitmap)
				time.AfterFunc(250*time.Millisecond, func() {
					c.options.Post(func() {
						if b := ref.Value(); b != nil {
							b.ReleasePixels()
						}
					})
				})
			}
			c.pump()
		}, func() { <-c.jobs })
	}()
}

func (c *PictureCache) nextPath(s *PictureSlot) string {
	if !s.hasNext || len(s.paths) == 0 {
		return ""
	}
	path := s.paths[pictureIndex(s.next, len(s.paths))]
	if path == s.target {
		return ""
	}
	return path
}

func (c *PictureCache) pump() {
	if c.closed {
		return
	}
	// Current visible targets take priority over hidden-page warmup and prefetch.
	for _, visible := range []bool{true, false} {
		for _, s := range c.slots {
			if s.Visible == visible {
				c.request(s.target, false, 0)
			}
		}
	}
	for _, s := range c.slots {
		if e := c.entries[s.shown]; e != nil && s.shown == s.target {
			c.request(c.nextPath(s), true, e.bytes)
		}
	}
	c.trimMetadata()
}

func (c *PictureCache) trimMetadata() {
	for len(c.entries) > pictureCacheEntries {
		var oldest string
		var used uint64
		found := false
		for path, e := range c.entries {
			if e.loading || c.protected(path, "") {
				continue
			}
			wanted := false
			for _, s := range c.slots {
				if s.target == path || s.shown == path || c.nextPath(s) == path {
					wanted = true
					break
				}
			}
			if !wanted && (!found || e.used < used) {
				oldest, used, found = path, e.used, true
			}
		}
		if !found {
			return
		}
		c.bytes -= c.entries[oldest].bytes
		delete(c.entries, oldest)
	}
}

// Retain forgets files removed from both configurations. A retired in-flight
// result cannot replace a later entry for the same path.
func (c *PictureCache) Retain(paths []string) {
	if c.closed {
		return
	}
	for path, e := range c.entries {
		if !slices.Contains(paths, path) {
			c.bytes -= e.bytes
			delete(c.entries, path)
		}
	}
	for _, s := range c.slots {
		if !slices.Contains(paths, s.shown) {
			s.shown = ""
		}
		s.paths = slices.DeleteFunc(s.paths, func(path string) bool { return !slices.Contains(paths, path) })
		s.attempt = 0
		c.choose(s)
	}
}

func (c *PictureCache) Close() {
	if c == nil || c.closed {
		return
	}
	c.closed = true
	close(c.done)
	clear(c.entries)
	c.bytes = 0
	c.slots = nil
}
