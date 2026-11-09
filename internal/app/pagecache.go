package app

import "slices"

// pageLimit is how many pages of each kind keep the data they loaded: one
// opened again loads it from the cache on disk, so a listener who browses
// far and wide keeps a few screens' worth, not every page of the session.
const pageLimit = 8

// pageCache keeps the data of the last pages opened of one kind, dropping
// the ones opened longest ago. Every page of it is read, and changed, on
// the main thread, as the rest of the app's state is.
type pageCache[K comparable, V any] struct {
	limit int          // how many pages keep their data
	busy  func(V) bool // whether a page's data is still on its way
	items map[K]V
	order []K // the pages, the one opened longest ago first
}

func newPageCache[K comparable, V any](busy func(V) bool) pageCache[K, V] {
	return pageCache[K, V]{limit: pageLimit, busy: busy}
}

// get returns a page's data, if it has any, as the last opened one.
func (c *pageCache[K, V]) get(k K) V {
	v, ok := c.items[k]
	if !ok {
		var zero V
		return zero
	}
	c.use(k)
	return v
}

// peek returns a page's data without counting the page as opened, for the
// replies of requests made while it loaded.
func (c *pageCache[K, V]) peek(k K) V { return c.items[k] }

// set keeps a page's data, as the last opened one.
func (c *pageCache[K, V]) set(k K, v V) {
	if c.items == nil {
		c.items = map[K]V{}
	}
	c.items[k] = v
	c.use(k)
}

// reset forgets every page, as signing out does.
func (c *pageCache[K, V]) reset() {
	c.items, c.order = nil, nil
}

// use marks a page as opened now, and drops the ones beyond the limit.
func (c *pageCache[K, V]) use(k K) {
	if i := slices.Index(c.order, k); i >= 0 {
		c.order = append(slices.Delete(c.order, i, i+1), k)
	} else {
		c.order = append(c.order, k)
	}
	c.evict()
}

// evict drops the pages opened longest ago, past the limit, but those whose
// data is still on its way: their reply lands where its page still is.
func (c *pageCache[K, V]) evict() {
	for len(c.order) > c.limit {
		i := 0
		for i < len(c.order) && c.busy != nil && c.busy(c.items[c.order[i]]) {
			i++
		}
		if i == len(c.order) {
			return
		}
		delete(c.items, c.order[i])
		c.order = slices.Delete(c.order, i, i+1)
	}
}

// artistBusy reports whether an artist's page is still loading.
func artistBusy(d *artistData) bool {
	return d != nil && (d.loading || d.latestLoading || d.albumsPage.loading || d.eventsPage.loading)
}

// albumBusy reports whether an album's page is still loading.
func albumBusy(d *albumData) bool {
	return d != nil && d.loading
}

// userBusy reports whether a user's page is still loading.
func userBusy(d *userData) bool {
	return d != nil && (d.loading > 0 || d.subsLoading > 0)
}

// podcastBusy reports whether a podcast's page is still loading.
func podcastBusy(d *podcastData) bool {
	return d != nil && (d.loading || d.page.loading || d.subscribing)
}

// searchBusy reports whether a search's page is still loading.
func searchBusy(d *searchData) bool {
	return d != nil && d.page.loading
}
