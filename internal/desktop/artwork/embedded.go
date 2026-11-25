/*
共享本地音频内嵌封面的读取任务，并独立限制编码图片的缓存大小。
*/

package artwork

import (
	"container/list"
	"sync"

	"netease-music-mygo/internal/localcatalog"
)

const embeddedArtworkLimit = 8 << 20

type EmbeddedKey struct {
	Path        string
	Stamp, Size int64
}

func LocalEmbeddedKey(s localcatalog.Song) EmbeddedKey {
	return EmbeddedKey{s.Path, s.Stamp, s.Size}
}

type embeddedArtwork struct {
	key   EmbeddedKey
	ready chan struct{}
	data  []byte
	entry *list.Element
}

// The player and the different UI sizes share encoded artwork. Keep one
// in-flight read per file and evict encoded bytes independently of bitmaps.
type EmbeddedCache struct {
	mu    sync.Mutex
	files map[EmbeddedKey]*embeddedArtwork
	order list.List
	bytes int
}

func (c *EmbeddedCache) Get(key EmbeddedKey, read func(string) []byte) []byte {
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
		c.files = make(map[EmbeddedKey]*embeddedArtwork)
	}
	c.files[key] = f
	c.mu.Unlock()

	data := read(key.Path)
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
