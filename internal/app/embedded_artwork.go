package app

import (
	"container/list"
	"sync"

	"netease-music-mygo/internal/localcatalog"
)

const embeddedArtworkLimit = 8 << 20

type embeddedArtworkKey struct {
	path        string
	stamp, size int64
}

func localEmbeddedArtworkKey(s localcatalog.Song) embeddedArtworkKey {
	return embeddedArtworkKey{s.Path, s.Stamp, s.Size}
}

type embeddedArtwork struct {
	key   embeddedArtworkKey
	ready chan struct{}
	data  []byte
	entry *list.Element
}

// The player and the different UI sizes share encoded artwork. Keep one
// in-flight read per file and evict encoded bytes independently of bitmaps.
type embeddedArtworkCache struct {
	mu    sync.Mutex
	files map[embeddedArtworkKey]*embeddedArtwork
	order list.List
	bytes int
}

func (c *embeddedArtworkCache) get(key embeddedArtworkKey, read func(string) []byte) []byte {
	c.mu.Lock()
	if found := c.files[key]; found != nil {
		if found.entry != nil {
			c.order.MoveToFront(found.entry)
		}
		c.mu.Unlock()
		<-found.ready
		return found.data
	}
	f := &embeddedArtwork{key: key, ready: make(chan struct{})}
	if c.files == nil {
		c.files = make(map[embeddedArtworkKey]*embeddedArtwork)
	}
	c.files[key] = f
	c.mu.Unlock()

	data := read(key.path)
	c.mu.Lock()
	f.data = data
	if len(data) == 0 || len(data) > embeddedArtworkLimit {
		delete(c.files, key)
	} else {
		f.entry = c.order.PushFront(f)
		c.bytes += len(data)
		for c.bytes > embeddedArtworkLimit {
			old := c.order.Remove(c.order.Back()).(*embeddedArtwork)
			delete(c.files, old.key)
			c.bytes -= len(old.data)
		}
	}
	close(f.ready)
	c.mu.Unlock()
	return data
}
