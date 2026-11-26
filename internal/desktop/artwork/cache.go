/*
管理解码后封面的异步加载、可见区域保留与内存预算，并协调结果发布和释放。
*/

package artwork

import (
	"image"
	"slices"
	"sync"
	"time"
	"weak"

	"github.com/egoist/mygo/ui"

	"netease-music-mygo/internal/navigation"
	"netease-music-mygo/internal/netease"
)

// Image is a cover, decoded and ready to draw, with what the player
// derives from it: its color, and a blurred copy to fill a backdrop.
type Image struct {
	family    string // public CDN variants of the same cover
	retryable bool
	Bitmap    *ui.Bitmap
	Backdrop  *ui.Bitmap
	Tint      ui.Color // the cover's most vivid color
	Failed    bool
	stale     bool      // newer image bytes are ready to load
	used      time.Time // when it was last asked for
	loaded    time.Time
	retryAt   time.Time // next refresh attempt after a temporary failure
	size      int       // the bytes it keeps: its data, the pixels those decode to, and the backdrop's pixels
}

// Cache loads covers on goroutines and keeps them, up to artLimit
// bytes: their pixels on the GPU while they show, and on the CPU only
// their data, which takes a fraction of the pixels. Its methods run on
// the main thread; post runs a function there.
type Cache struct {
	art         map[string]*Image
	variants    map[string]map[string]*Image
	page        navigation.Route
	pageVisible bool
	returnPages [2]artPage
	lists       listArtwork
	// gone are the covers dropped, for as long as something else holds
	// them: Now Playing keeps the cover it shows, which comes back as it
	// was rather than loading again.
	gone         map[string]weak.Pointer[Image]
	busy         map[string]uint64
	sem          chan struct{}
	load         func(string) (*ui.Bitmap, image.Image, int, error)
	post         func(func())
	postResource func(func(), func())
	now          func() time.Time
	size         int // the bytes the covers in art keep
	limit        int
	serial       uint64
	seen         map[string]struct{} // covers the most recent view asked to paint
	rejected     map[string]int      // decoded sizes that did not fit beside visible covers
	results      *sync.Cond
	resultBytes  int
	closed       bool // guarded by results.L
}

type artPage struct {
	route navigation.Route
	keys  map[string]struct{}
}

const artJobs = 8

const artResultLimit = 16 << 20

const artRefreshAfter = 24 * time.Hour

const artRefreshRetry = time.Minute

const artScrollGrace = time.Second

// artLimit bounds encoded data and estimated decoded pixels in the cache.
const artLimit = 64 << 20

// Options supplies the owning thread and optional loader, clock and byte budget.
// Post is required and schedules work on the owning thread. PostResource must
// run exactly one of its callbacks: the result on that thread or its release
// callback when dispatch is closed. Configure options before requesting images.
type Options struct {
	Post         func(func())
	PostResource func(func(), func())
	Load         func(string) (*ui.Bitmap, image.Image, int, error)
	Now          func() time.Time
	Limit        int
}

func New(options Options) *Cache {
	c := &Cache{art: map[string]*Image{}, variants: map[string]map[string]*Image{}, gone: map[string]weak.Pointer[Image]{}, busy: map[string]uint64{},
		sem: make(chan struct{}, artJobs), post: options.Post, now: options.Now, limit: options.Limit, load: options.Load,
		seen: map[string]struct{}{}, rejected: map[string]int{}, results: sync.NewCond(&sync.Mutex{})}
	if c.now == nil {
		c.now = time.Now
	}
	if c.limit <= 0 {
		c.limit = artLimit
	}
	if c.load == nil {
		c.load = LoadURL
	}
	c.postResource = func(fn, _ func()) { c.post(fn) }
	if options.PostResource != nil {
		c.postResource = options.PostResource
	}
	return c
}

// Bound bytes already posted to the UI. A single large result may exceed
// the budget; it must be accepted alone. Waiters also hold a job slot.
func (c *Cache) reserveResult(size int) bool {
	c.results.L.Lock()
	defer c.results.L.Unlock()
	for !c.closed && size > 0 && c.resultBytes > 0 && c.resultBytes+size > artResultLimit {
		c.results.Wait()
	}
	if c.closed {
		return false
	}
	c.resultBytes += size
	return true
}

func (c *Cache) finishResult(size int) {
	c.results.L.Lock()
	c.resultBytes -= size
	c.results.Broadcast()
	c.results.L.Unlock()
	<-c.sem
}

func (c *Cache) Close() {
	if c == nil {
		return
	}
	c.results.L.Lock()
	c.closed = true
	c.results.Broadcast()
	c.results.L.Unlock()
}

// add gives the incoming cover space by dropping offscreen entries first.
// Reject it if it would displace a visible cover or exceed the hard limit.
func (c *Cache) add(key string, a *Image) bool {
	previous := c.art[key]
	size := c.size + a.size
	if previous != nil {
		size -= previous.size
	}
	if size > c.limit && !c.fitsVisible(key, a.size) {
		c.rejected[key] = a.size
		return false
	}
	delete(c.rejected, key)
	c.remove(key)
	c.art[key] = a
	if a.family != "" && a.Bitmap != nil {
		if c.variants[a.family] == nil {
			c.variants[a.family] = map[string]*Image{}
		}
		c.variants[a.family][key] = a
	}
	c.size = size
	if c.size <= c.limit {
		return true
	}
	for key, p := range c.gone {
		if p.Value() == nil {
			delete(c.gone, key)
		}
	}
	type candidate struct {
		key  string
		keep bool
		list int
		used time.Time
	}
	candidates := make([]candidate, 0, len(c.art))
	retained := c.retainedListKeys()
	recent := c.now().Add(-artScrollGrace)
	for candidateKey, held := range c.art {
		if _, visible := c.seen[candidateKey]; !visible && candidateKey != key && held.size > 0 {
			keep := c.keepForReturn(candidateKey) || (held.Bitmap != nil && held.used.After(recent))
			candidates = append(candidates, candidate{key: candidateKey, keep: keep, list: retained[candidateKey], used: held.used})
		}
	}
	slices.SortFunc(candidates, func(x, y candidate) int {
		if x.list != y.list {
			return x.list - y.list
		}
		if x.keep != y.keep {
			if x.keep {
				return 1
			}
			return -1
		}
		return x.used.Compare(y.used)
	})
	for _, candidate := range candidates {
		if c.size <= c.limit*3/4 || (c.size <= c.limit && (candidate.keep || candidate.list != 0)) {
			break
		}
		c.gone[candidate.key] = weak.Make(c.remove(candidate.key))
	}
	return true
}

func (c *Cache) remove(key string) *Image {
	a := c.art[key]
	if a == nil {
		return nil
	}
	delete(c.art, key)
	c.size -= a.size
	if variants := c.variants[a.family]; variants != nil {
		delete(variants, key)
		if len(variants) == 0 {
			delete(c.variants, a.family)
		}
	}
	return a
}

func (c *Cache) keepForReturn(key string) bool {
	_, latest := c.returnPages[0].keys[key]
	_, previous := c.returnPages[1].keys[key]
	return latest || previous
}

func (c *Cache) fitsVisible(key string, size int) bool {
	for visible := range c.seen {
		if a := c.art[visible]; visible != key && a != nil {
			size += a.size
		}
	}
	return size <= c.limit
}

func artworkKey(url string, backdrop bool) string {
	if backdrop {
		return url + "#backdrop"
	}
	return url
}

// get returns the cover at url, or nil while it loads.
func (c *Cache) Get(url string, backdrop bool) *Image {
	return c.request(url, backdrop, true)
}

// Use the previous layout's clipping to avoid loading offscreen scroll
// content. MyGo rebuilds when InView changes, and a warm cover can paint
// on the first frame without waiting for that measurement.
func (c *Cache) GetInView(url string, backdrop bool, box ui.Element) *Image {
	return c.getInView(url, backdrop, box.InView(), c.load)
}

func (c *Cache) getInView(url string, backdrop, visible bool, load func(string) (*ui.Bitmap, image.Image, int, error)) *Image {
	key := artworkKey(url, backdrop)
	a := c.art[key]
	if visible {
		a = c.RequestCover(url, backdrop, true, load)
	} else if a == nil {
		a = c.gone[key].Value()
	}
	if (a != nil && !a.retryable) || backdrop || len(c.variants) == 0 || url == "" {
		return a
	}
	// Draw another decoded size while the requested size loads. Keep this
	// as a display fallback so the original request still runs normally.
	var bestKey string
	var bestSize int
	for candidate, art := range c.variants[netease.PublicCoverFamily(url)] {
		w, h := art.Bitmap.Size()
		if size := min(w, h); size > bestSize {
			a, bestKey, bestSize = art, candidate, size
		}
	}
	if visible && bestKey != "" {
		c.seen[bestKey] = struct{}{}
		a.used = c.now()
	}
	return a
}

func (c *Cache) Prefetch(url string, backdrop bool) {
	if c.size < c.limit*3/4 && len(c.sem) < cap(c.sem)-1 {
		c.request(url, backdrop, false)
	}
}

func (c *Cache) request(url string, backdrop, visible bool) *Image {
	return c.RequestCover(url, backdrop, visible, c.load)
}

func (c *Cache) RequestCover(url string, backdrop, visible bool, load func(string) (*ui.Bitmap, image.Image, int, error)) *Image {
	if url == "" {
		return nil
	}
	key := artworkKey(url, backdrop)
	if visible {
		c.seen[key] = struct{}{}
	}
	now := c.now()
	current := c.art[key]
	if current == nil {
		if held := c.gone[key].Value(); held != nil {
			delete(c.gone, key)
			held.used = now
			c.add(key, held)
			current = held
		}
	}
	if a := current; a != nil {
		if visible {
			a.used = now
		}
		if a.Failed || now.Before(a.retryAt) || !a.stale && (a.loaded.IsZero() || now.Sub(a.loaded) < artRefreshAfter) {
			return a
		}
	}
	if c.busy[key] != 0 || c.rejected[key] > 0 && !c.fitsVisible(key, c.rejected[key]) {
		return current
	}
	// Do not queue a goroutine for every URL scrolled past. A completion
	// redraws the view, which asks again for the covers it still needs.
	select {
	case c.sem <- struct{}{}:
	default:
		return current
	}
	c.serial++
	request := c.serial
	c.busy[key] = request
	go func() {
		family := netease.PublicCoverFamily(url)
		bmp, img, size, err := load(url)
		var blurred *image.RGBA
		var tint ui.Color
		if err == nil {
			tint = DominantColor(img)
			size += pixelBytes(bmp)
			if backdrop {
				blurred = Blur(img)
				size += len(blurred.Pix)
			}
		} else {
			size = 0
		}
		if !c.reserveResult(size) {
			<-c.sem
			return
		}
		c.postResource(func() {
			defer c.finishResult(size)
			if c.busy[key] != request {
				return
			}
			delete(c.busy, key)
			if err != nil {
				if netease.CoverRevoked(err) {
					c.ImageChanged(url, netease.ImageRevoked)
					return
				}
				if old := c.art[key]; old != nil && old.Bitmap != nil {
					old.retryAt = c.now().Add(artRefreshRetry)
					return
				}
				c.add(key, &Image{Failed: true, retryable: netease.RetryableCoverError(err), loaded: c.now(), used: c.now()})
				return
			}
			// The pixels count against the limit as well as the data: they
			// come and go as the GPU needs them, and the limit has to bound
			// both.
			a := &Image{family: family, loaded: c.now(), Bitmap: bmp, Tint: tint, used: c.now(), size: size}
			if blurred != nil {
				a.Backdrop = ui.NewBitmap(blurred)
			}
			if !c.add(key, a) {
				return
			}
			// Shown by the frame post draws, the cover drops its pixels,
			// which its data decodes again if they are needed.
			ref := weak.Make(bmp)
			time.AfterFunc(artRelease, func() {
				c.post(func() {
					if bmp := ref.Value(); bmp != nil {
						bmp.ReleasePixels()
					}
				})
			})
		}, func() { c.finishResult(size) })
	}()
	return current
}

// artRelease is how long a cover keeps its pixels once loaded: past the
// frame that first shows it, and the smaller copies it shows from there.
const artRelease = 2 * time.Second

// Visibility is collected during normal drawing, without retaining page objects.
// pageFrame remembers the Image of pages the listener might return to.
// These keys only affect eviction priority within the existing memory limit.
func (c *Cache) PageFrame(page navigation.Route, visible bool) {
	if c.pageVisible && (c.page != page || !visible) {
		keys := make(map[string]struct{}, len(c.seen))
		for key := range c.seen {
			if a := c.art[key]; a != nil && a.Bitmap != nil {
				keys[key] = struct{}{}
			}
		}
		if c.returnPages[0].route != c.page {
			c.returnPages[1] = c.returnPages[0]
		}
		c.returnPages[0] = artPage{route: c.page, keys: keys}
	}
	c.page, c.pageVisible = page, visible
	c.BeginFrame()
}

func (c *Cache) BeginFrame() {
	c.finishListFrame()
	if len(c.art) > 512 {
		for key, a := range c.art {
			if a.Failed {
				if _, visible := c.seen[key]; !visible {
					c.remove(key)
				}
			}
		}
	}
	for key := range c.rejected {
		if _, seen := c.seen[key]; !seen {
			delete(c.rejected, key)
		}
	}
	clear(c.seen)
}

func (c *Cache) HasFailed(manual bool) bool {
	for key := range c.seen {
		if a := c.art[key]; a != nil && a.Failed && (manual || a.retryable) {
			return true
		}
	}
	return false
}

func (c *Cache) RetryFailed(manual bool) {
	for key := range c.seen {
		if a := c.art[key]; a != nil && a.Failed && (manual || a.retryable) {
			c.remove(key)
		}
	}
}

// A replacement keeps the old bitmap visible until its successor decodes.
func (c *Cache) Invalidate(url string, replacement bool) {
	for _, key := range []string{url, url + "#backdrop"} {
		// A queued result may still contain the previous bytes.
		delete(c.busy, key)
		delete(c.rejected, key)
		current := c.art[key]
		if replacement {
			held := current
			if held == nil {
				held = c.gone[key].Value()
			}
			if held != nil && held.Bitmap != nil {
				held.stale = true
				held.retryAt = time.Time{}
				continue
			}
		}
		c.remove(key)
		delete(c.gone, key)
	}
}

func (c *Cache) ImageChanged(url string, change netease.ImageChange) {
	c.Invalidate(url, change == netease.ImageReplaced)
	if change == netease.ImageRevoked {
		// The disk revalidation may finish before or after a UI decode.
		// Both orders must leave a missing cover, with explicit retry.
		now := c.now()
		for _, backdrop := range []bool{false, true} {
			c.add(artworkKey(url, backdrop), &Image{Failed: true, loaded: now, used: now})
		}
	}
}

func (c *Cache) HasLoading() bool {
	for key := range c.seen {
		if c.busy[key] != 0 {
			return true
		}
	}
	return false
}

// Put makes an already decoded image available under its cache key. It accounts
// for bitmap storage and applies the same eviction rules as asynchronous loads.
func (c *Cache) Put(key string, value *Image) bool {
	value.family = netease.PublicCoverFamily(key)
	value.used, value.loaded = c.now(), c.now()
	value.size = pixelBytes(value.Bitmap) + pixelBytes(value.Backdrop)
	return c.add(key, value)
}

// Peek observes a cached image without starting a request or changing visibility.
func (c *Cache) Peek(key string) *Image { return c.art[key] }

type Stats struct{ Bytes, Entries, Pending, Jobs, PendingBytes int }

// Stats is read on the owner thread; workers only change the pending byte budget.
func (c *Cache) Stats() Stats {
	c.results.L.Lock()
	bytes := c.resultBytes
	c.results.L.Unlock()
	return Stats{c.size, len(c.art), len(c.busy), len(c.sem), bytes}
}
