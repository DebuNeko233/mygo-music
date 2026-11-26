/*
在共享位图预算内保留最近歌曲列表的可见小封面。
*/

package artwork

import (
	"image"
	"slices"

	"github.com/egoist/mygo/ui"

	"netease-music-mygo/internal/navigation"
)

const (
	listReturnPages  = 3
	listReturnCovers = 30
	listReturnBytes  = 8 << 20
)

type listArtPage struct {
	route navigation.Route
	keys  []string
}

type listArtwork struct {
	pages   []listArtPage // oldest first; keys never hold unaccounted Image references
	current listArtPage
	active  bool
}

// GetListInView uses the same URL cache as other views, but remembers only
// visible list covers for bounded return-page retention. Call on the owner thread.
func (c *Cache) GetListInView(page navigation.Route, url string, box ui.Element) *Image {
	return c.RequestListCover(page, url, box, c.load)
}

// RequestListCover also supports local embedded artwork with the same retention
// and viewport rules. The loader must capture immutable input, never box or page UI.
func (c *Cache) RequestListCover(page navigation.Route, key string, box ui.Element, load func(string) (*ui.Bitmap, image.Image, int, error)) *Image {
	visible := box.InView()
	if visible && key != "" {
		c.noteListCover(page, key)
	}
	return c.getInView(key, false, visible, load)
}

func (c *Cache) noteListCover(page navigation.Route, key string) {
	l := &c.lists
	if l.active && l.current.route != page {
		c.finishListFrame()
	}
	l.current.route, l.active = page, true
	if len(l.current.keys) < listReturnCovers && !slices.Contains(l.current.keys, key) {
		l.current.keys = append(l.current.keys, key)
	}
}

// BeginFrame captures the last built viewport, replacing rather than accumulating
// a page's scrolled-past covers. Repeated builds and non-list pages add no history.
func (c *Cache) finishListFrame() {
	l := &c.lists
	if !l.active {
		return
	}
	if n := len(l.pages); n > 0 && l.pages[n-1].route == l.current.route && slices.Equal(l.pages[n-1].keys, l.current.keys) {
		l.current.keys = l.current.keys[:0]
		l.active = false
		return
	}
	l.pages = slices.DeleteFunc(l.pages, func(p listArtPage) bool { return p.route == l.current.route })
	l.pages = append(l.pages, listArtPage{route: l.current.route, keys: slices.Clone(l.current.keys)})
	if len(l.pages) > listReturnPages {
		l.pages = slices.Delete(l.pages, 0, len(l.pages)-listReturnPages)
	}
	l.current.keys = l.current.keys[:0]
	l.active = false
}

// Retention is a priority inside art, not another bitmap cache. Newer pages get
// the byte allowance first; failed, missing and oversized entries receive none.
// Hard cache pressure can still retire the oldest protected page's images.
func (c *Cache) retainedListKeys() map[string]int {
	l := &c.lists
	kept := make(map[string]int)
	budget, pages := min(listReturnBytes, c.limit), 0
	add := func(p listArtPage) {
		pages++
		for _, key := range p.keys {
			a := c.art[key]
			if kept[key] != 0 || a == nil || a.Bitmap == nil || a.Failed || a.size > budget {
				continue
			}
			kept[key] = listReturnPages + 1 - pages
			budget -= a.size
		}
	}
	if l.active {
		add(l.current)
	}
	for i := len(l.pages) - 1; i >= 0 && pages < listReturnPages; i-- {
		if !l.active || l.pages[i].route != l.current.route {
			add(l.pages[i])
		}
	}
	return kept
}
