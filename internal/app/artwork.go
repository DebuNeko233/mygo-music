package app

import (
	"bytes"
	"encoding/binary"
	"image"
	"image/color"
	_ "image/gif"
	_ "image/jpeg"
	_ "image/png"
	"math"
	"slices"
	"sync"
	"time"
	"weak"

	"github.com/egoist/mygo/ui"
	xdraw "golang.org/x/image/draw"
	_ "golang.org/x/image/webp"

	"netease-music-mygo/internal/netease"
)

// artwork is a cover, decoded and ready to draw, with what the player
// derives from it: its color, and a blurred copy to fill a backdrop.
type artwork struct {
	family    string // public CDN variants of the same cover
	retryable bool
	bmp       *ui.Bitmap
	backdrop  *ui.Bitmap
	tint      ui.Color // the cover's most vivid color
	failed    bool
	stale     bool      // newer image bytes are ready to load
	used      time.Time // when it was last asked for
	loaded    time.Time
	retryAt   time.Time // next refresh attempt after a temporary failure
	size      int       // the bytes it keeps: its data, the pixels those decode to, and the backdrop's pixels
}

// artCache loads covers on goroutines and keeps them, up to artLimit
// bytes: their pixels on the GPU while they show, and on the CPU only
// their data, which takes a fraction of the pixels. Its methods run on
// the main thread; post runs a function there.
type artCache struct {
	art         map[string]*artwork
	variants    map[string]map[string]*artwork
	page        route
	pageVisible bool
	returnPages [2]artPage
	// gone are the covers dropped, for as long as something else holds
	// them: Now Playing keeps the cover it shows, which comes back as it
	// was rather than loading again.
	gone         map[string]weak.Pointer[artwork]
	busy         map[string]uint64
	sem          chan struct{}
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
	route route
	keys  map[string]struct{}
}

const artJobs = 8
const artResultLimit = 16 << 20
const artRefreshAfter = 24 * time.Hour
const artRefreshRetry = time.Minute
const artScrollGrace = time.Second

// artLimit bounds encoded data and estimated decoded pixels in the cache.
const artLimit = 64 << 20

func newArtCache(post func(func()), resources ...func(func(), func())) *artCache {
	c := &artCache{art: map[string]*artwork{}, variants: map[string]map[string]*artwork{}, gone: map[string]weak.Pointer[artwork]{}, busy: map[string]uint64{},
		sem: make(chan struct{}, artJobs), post: post, now: time.Now, limit: artLimit,
		seen: map[string]struct{}{}, rejected: map[string]int{},
		results: sync.NewCond(&sync.Mutex{})}
	c.postResource = func(fn, _ func()) { c.post(fn) }
	if len(resources) != 0 {
		c.postResource = resources[0]
	}
	return c
}

// Bound bytes already posted to the UI. A single large result may exceed
// the budget; it must be accepted alone. Waiters also hold a job slot.
func (c *artCache) reserveResult(size int) bool {
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

func (c *artCache) finishResult(size int) {
	c.results.L.Lock()
	c.resultBytes -= size
	c.results.Broadcast()
	c.results.L.Unlock()
	<-c.sem
}

func (c *artCache) close() {
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
func (c *artCache) add(key string, a *artwork) bool {
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
	if a.family != "" && a.bmp != nil {
		if c.variants[a.family] == nil {
			c.variants[a.family] = map[string]*artwork{}
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
		used time.Time
	}
	candidates := make([]candidate, 0, len(c.art))
	recent := c.now().Add(-artScrollGrace)
	for candidateKey, held := range c.art {
		if _, visible := c.seen[candidateKey]; !visible && candidateKey != key && held.size > 0 {
			keep := c.keepForReturn(candidateKey) || (held.bmp != nil && held.used.After(recent))
			candidates = append(candidates, candidate{key: candidateKey, keep: keep, used: held.used})
		}
	}
	slices.SortFunc(candidates, func(x, y candidate) int {
		if x.keep != y.keep {
			if x.keep {
				return 1
			}
			return -1
		}
		return x.used.Compare(y.used)
	})
	for _, candidate := range candidates {
		if c.size <= c.limit*3/4 || (c.size <= c.limit && candidate.keep) {
			break
		}
		c.gone[candidate.key] = weak.Make(c.remove(candidate.key))
	}
	return true
}

func (c *artCache) remove(key string) *artwork {
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

func (c *artCache) keepForReturn(key string) bool {
	_, latest := c.returnPages[0].keys[key]
	_, previous := c.returnPages[1].keys[key]
	return latest || previous
}

func (c *artCache) fitsVisible(key string, size int) bool {
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
func (c *artCache) get(url string, backdrop bool) *artwork {
	return c.request(url, backdrop, true)
}

// Use the previous layout's clipping to avoid loading offscreen scroll
// content. MyGo rebuilds when InView changes, and a warm cover can paint
// on the first frame without waiting for that measurement.
func (c *artCache) getInView(url string, backdrop bool, box ui.Element) *artwork {
	visible := box.InView()
	key := artworkKey(url, backdrop)
	a := c.art[key]
	if visible {
		a = c.get(url, backdrop)
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
		w, h := art.bmp.Size()
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

func (c *artCache) prefetch(url string, backdrop bool) {
	if c.size < c.limit*3/4 && len(c.sem) < cap(c.sem)-1 {
		c.request(url, backdrop, false)
	}
}

func (c *artCache) request(url string, backdrop, visible bool) *artwork {
	return c.requestCover(url, backdrop, visible, loadArt)
}

func (c *artCache) requestCover(url string, backdrop, visible bool, load func(string) (*ui.Bitmap, image.Image, int, error)) *artwork {
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
		if a.failed || now.Before(a.retryAt) || !a.stale && (a.loaded.IsZero() || now.Sub(a.loaded) < artRefreshAfter) {
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
			tint = dominantColor(img)
			size += pixelBytes(bmp)
			if backdrop {
				blurred = blurImage(img)
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
					c.imageChanged(url, netease.ImageRevoked)
					return
				}
				if old := c.art[key]; old != nil && old.bmp != nil {
					old.retryAt = c.now().Add(artRefreshRetry)
					return
				}
				c.add(key, &artwork{failed: true, retryable: netease.RetryableCoverError(err), loaded: c.now(), used: c.now()})
				return
			}
			// The pixels count against the limit as well as the data: they
			// come and go as the GPU needs them, and the limit has to bound
			// both.
			a := &artwork{family: family, loaded: c.now(), bmp: bmp, tint: tint, used: c.now(), size: size}
			if blurred != nil {
				a.backdrop = ui.NewBitmap(blurred)
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

// pixelBytes is how many bytes a bitmap's pixels take, with those of the
// smaller copies it shows from, about a third as many again.
func pixelBytes(b *ui.Bitmap) int {
	w, h := b.Size()
	if w <= 0 || h <= 0 {
		return 0
	}
	pixels := 4 * w * h
	return pixels + pixels/3
}

// loadArt fetches the cover at url, as a bitmap to draw and an image to
// measure, with the bytes of its data.
var loadArt = func(url string) (*ui.Bitmap, image.Image, int, error) {
	b, err := netease.Fetch(url)
	if err != nil {
		return nil, nil, 0, err
	}
	return decodeArtSized(b, netease.CoverRequestSize(url))
}

func decodeArtSized(b []byte, limit int) (*ui.Bitmap, image.Image, int, error) {
	b = stillCover(b)
	img, _, err := image.Decode(bytes.NewReader(b))
	if err != nil {
		return nil, nil, 0, err
	}
	bounds := img.Bounds()
	if w, h := bounds.Dx(), bounds.Dy(); limit > 0 && max(w, h) > limit {
		long := max(w, h)
		small := image.NewRGBA(image.Rect(0, 0, max(1, w*limit/long), max(1, h*limit/long)))
		xdraw.ApproxBiLinear.Scale(small, small.Bounds(), img, bounds, xdraw.Src, nil)
		// The small bitmap owns its pixels. It need not keep the large
		// encoded download around to reconstruct them.
		return ui.NewBitmap(small), small, 0, nil
	}
	bmp, err := ui.DecodeBitmap(b)
	return bmp, img, len(b), err
}

func decodeImage(b []byte) (image.Image, error) {
	img, _, err := image.Decode(bytes.NewReader(stillCover(b)))
	return img, err
}

// stillCover returns the data of a cover that the decoders read. Some
// covers are animated WebP, whatever their name says, which they cannot:
// those show their first frame.
func stillCover(b []byte) []byte {
	if still := firstWebPFrame(b); still != nil {
		return still
	}
	return b
}

// firstWebPFrame returns the first frame of an animated WebP as a still
// WebP of its own, or nil if b is not one.
func firstWebPFrame(b []byte) []byte {
	if len(b) < 12 || string(b[:4]) != "RIFF" || string(b[8:12]) != "WEBP" {
		return nil
	}
	chunk := func(b []byte) (name string, body, rest []byte, ok bool) {
		if len(b) < 8 {
			return "", nil, nil, false
		}
		n := int(binary.LittleEndian.Uint32(b[4:8]))
		if n > len(b)-8 {
			return "", nil, nil, false
		}
		end := min(len(b), 8+n+n&1)
		return string(b[:4]), b[8 : 8+n], b[end:], true
	}
	riff := func(chunks ...[]byte) []byte {
		body := []byte("WEBP")
		for _, c := range chunks {
			body = append(body, c...)
		}
		out := binary.LittleEndian.AppendUint32([]byte("RIFF"), uint32(len(body)))
		return append(out, body...)
	}
	for rest := b[12:]; ; {
		name, body, next, ok := chunk(rest)
		if !ok {
			return nil
		}
		rest = next
		if name != "ANMF" || len(body) < 16 {
			continue
		}
		// A frame: where it sits, its size, how long it shows, then its
		// image as the chunks of a still WebP, its alpha first if any.
		w, h := body[6:9], body[9:12]
		frame := body[16:]
		var alpha []byte
		for len(frame) > 0 {
			name, _, after, ok := chunk(frame)
			if !ok {
				return nil
			}
			whole := frame[:len(frame)-len(after)]
			switch name {
			case "ALPH":
				alpha = whole
			case "VP8L":
				return riff(whole)
			case "VP8 ":
				if alpha == nil {
					return riff(whole)
				}
				// Alpha needs the extended header, which carries the size.
				vp8x := []byte{'V', 'P', '8', 'X', 10, 0, 0, 0, 0x10, 0, 0, 0}
				vp8x = append(append(vp8x, w...), h...)
				return riff(vp8x, alpha, whole)
			}
			frame = after
		}
		return nil
	}
}

// shrink averages img down to n×n pixels.
func shrink(img image.Image, n int) *image.RGBA {
	b := img.Bounds()
	out := image.NewRGBA(image.Rect(0, 0, n, n))
	for y := range n {
		for x := range n {
			x0, x1 := b.Min.X+x*b.Dx()/n, b.Min.X+(x+1)*b.Dx()/n
			y0, y1 := b.Min.Y+y*b.Dy()/n, b.Min.Y+(y+1)*b.Dy()/n
			var r, g, bl, cnt uint32
			for yy := y0; yy < max(y1, y0+1); yy += max(1, (y1-y0)/4) {
				for xx := x0; xx < max(x1, x0+1); xx += max(1, (x1-x0)/4) {
					cr, cg, cb, _ := img.At(xx, yy).RGBA()
					r, g, bl, cnt = r+cr>>8, g+cg>>8, bl+cb>>8, cnt+1
				}
			}
			out.SetRGBA(x, y, color.RGBA{uint8(r / cnt), uint8(g / cnt), uint8(bl / cnt), 255})
		}
	}
	return out
}

// dominantColor returns the most vivid color that covers much of img: the
// hue that its saturated pixels share most, at their mean.
func dominantColor(img image.Image) ui.Color {
	small := shrink(img, 24)
	const buckets = 18
	var weight [buckets]float64
	var sum [buckets][3]float64
	var gray [3]float64
	for i := 0; i < len(small.Pix); i += 4 {
		r, g, b := float64(small.Pix[i])/255, float64(small.Pix[i+1])/255, float64(small.Pix[i+2])/255
		h, s, v := hsv(r, g, b)
		gray[0], gray[1], gray[2] = gray[0]+r, gray[1]+g, gray[2]+b
		w := s * s * v
		if s < 0.15 || v < 0.15 {
			continue
		}
		k := int(h/360*buckets) % buckets
		weight[k] += w
		sum[k][0], sum[k][1], sum[k][2] = sum[k][0]+r*w, sum[k][1]+g*w, sum[k][2]+b*w
	}
	best := 0
	for k := range weight {
		if weight[k] > weight[best] {
			best = k
		}
	}
	if weight[best] < 2 {
		n := float64(len(small.Pix) / 4)
		return ui.RGB(uint8(gray[0]/n*255), uint8(gray[1]/n*255), uint8(gray[2]/n*255))
	}
	w := weight[best]
	return ui.RGB(uint8(sum[best][0]/w*255), uint8(sum[best][1]/w*255), uint8(sum[best][2]/w*255))
}

func hsv(r, g, b float64) (h, s, v float64) {
	mx, mn := math.Max(r, math.Max(g, b)), math.Min(r, math.Min(g, b))
	v = mx
	if mx == 0 {
		return 0, 0, 0
	}
	s = (mx - mn) / mx
	d := mx - mn
	switch {
	case d == 0:
		h = 0
	case mx == r:
		h = math.Mod((g-b)/d, 6) * 60
	case mx == g:
		h = ((b-r)/d + 2) * 60
	default:
		h = ((r-g)/d + 4) * 60
	}
	if h < 0 {
		h += 360
	}
	return h, s, v
}

// blurImage returns img shrunk and blurred, more saturated, to fill a
// backdrop behind what plays, scaled up as smooth as it is.
func blurImage(img image.Image) *image.RGBA {
	const n = 48
	small := shrink(img, n)
	px := make([][3]float64, n*n)
	for i := range px {
		r, g, b := float64(small.Pix[4*i]), float64(small.Pix[4*i+1]), float64(small.Pix[4*i+2])
		// More saturated, as light through colored glass.
		l := 0.3*r + 0.59*g + 0.11*b
		px[i] = [3]float64{l + (r-l)*1.5, l + (g-l)*1.5, l + (b-l)*1.5}
	}
	tmp := make([][3]float64, n*n)
	for range 3 {
		boxBlur(px, tmp, n, 5)
	}
	out := image.NewRGBA(image.Rect(0, 0, n, n))
	for i, p := range px {
		for c := range 3 {
			out.Pix[4*i+c] = uint8(math.Max(0, math.Min(255, p[c])))
		}
		out.Pix[4*i+3] = 255
	}
	return out
}

// boxBlur blurs px of n×n pixels by a box of radius r, across then down,
// clamping at the edges.
func boxBlur(px, tmp [][3]float64, n, r int) {
	at := func(i int) int { return max(0, min(n-1, i)) }
	for y := range n {
		for x := range n {
			var s [3]float64
			for k := -r; k <= r; k++ {
				p := px[y*n+at(x+k)]
				s[0], s[1], s[2] = s[0]+p[0], s[1]+p[1], s[2]+p[2]
			}
			d := float64(2*r + 1)
			tmp[y*n+x] = [3]float64{s[0] / d, s[1] / d, s[2] / d}
		}
	}
	for y := range n {
		for x := range n {
			var s [3]float64
			for k := -r; k <= r; k++ {
				p := tmp[at(y+k)*n+x]
				s[0], s[1], s[2] = s[0]+p[0], s[1]+p[1], s[2]+p[2]
			}
			d := float64(2*r + 1)
			px[y*n+x] = [3]float64{s[0] / d, s[1] / d, s[2] / d}
		}
	}
}

// Visibility is collected during normal drawing, without retaining page objects.
// pageFrame remembers the artwork of pages the listener might return to.
// These keys only affect eviction priority within the existing memory limit.
func (c *artCache) pageFrame(page route, visible bool) {
	if c.pageVisible && (c.page != page || !visible) {
		keys := make(map[string]struct{}, len(c.seen))
		for key := range c.seen {
			if a := c.art[key]; a != nil && a.bmp != nil {
				keys[key] = struct{}{}
			}
		}
		if c.returnPages[0].route != c.page {
			c.returnPages[1] = c.returnPages[0]
		}
		c.returnPages[0] = artPage{route: c.page, keys: keys}
	}
	c.page, c.pageVisible = page, visible
	c.beginFrame()
}

func (c *artCache) beginFrame() {
	if len(c.art) > 512 {
		for key, a := range c.art {
			if a.failed {
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
func (c *artCache) hasFailed(manual bool) bool {
	for key := range c.seen {
		if a := c.art[key]; a != nil && a.failed && (manual || a.retryable) {
			return true
		}
	}
	return false
}
func (c *artCache) retryFailed(manual bool) {
	for key := range c.seen {
		if a := c.art[key]; a != nil && a.failed && (manual || a.retryable) {
			c.remove(key)
		}
	}
}

// A replacement keeps the old bitmap visible until its successor decodes.
func (c *artCache) invalidate(url string, replacement bool) {
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
			if held != nil && held.bmp != nil {
				held.stale = true
				held.retryAt = time.Time{}
				continue
			}
		}
		c.remove(key)
		delete(c.gone, key)
	}
}

func (c *artCache) imageChanged(url string, change netease.ImageChange) {
	c.invalidate(url, change == netease.ImageReplaced)
	if change == netease.ImageRevoked {
		// The disk revalidation may finish before or after a UI decode.
		// Both orders must leave a missing cover, with explicit retry.
		now := c.now()
		for _, backdrop := range []bool{false, true} {
			c.add(artworkKey(url, backdrop), &artwork{failed: true, loaded: now, used: now})
		}
	}
}

func (c *artCache) hasLoading() bool {
	for key := range c.seen {
		if c.busy[key] != 0 {
			return true
		}
	}
	return false
}
