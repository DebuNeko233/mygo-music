/*
缓存最近访问页面的数据，在容量限制内淘汰旧页面并保留待完成加载。
*/

package pagecache

import "slices"

// Limit bounds the retained pages of each kind; pending loads may temporarily exceed it.
const Limit = 8

// Cache keeps the data of the last pages opened of one kind, dropping
// the ones opened longest ago. Every page of it is read, and changed, on
// one owning thread; callers must dispatch worker results back to that thread.
type Cache[K comparable, V any] struct {
	limit int          // how many pages keep their data
	busy  func(V) bool // whether a page's data is still on its way
	items map[K]V
	order []K // the pages, the one opened longest ago first
}

func New[K comparable, V any](busy func(V) bool) Cache[K, V] {
	return Cache[K, V]{limit: Limit, busy: busy}
}

// Get returns a page's data, if it has any, as the last opened one.
func (c *Cache[K, V]) Get(k K) V {
	v, ok := c.items[k]
	if !ok {
		var zero V
		return zero
	}
	c.use(k)
	return v
}

// Peek returns a page's data without counting the page as opened, for the
// replies of requests made while it loaded.
func (c *Cache[K, V]) Peek(k K) V { return c.items[k] }

// Set keeps a page's data, as the last opened one.
func (c *Cache[K, V]) Set(k K, v V) {
	if c.items == nil {
		c.items = map[K]V{}
	}
	c.items[k] = v
	c.use(k)
}

// use marks a page as opened now, and drops the ones beyond the limit.
func (c *Cache[K, V]) use(k K) {
	if i := slices.Index(c.order, k); i >= 0 {
		c.order = append(slices.Delete(c.order, i, i+1), k)
	} else {
		c.order = append(c.order, k)
	}
	c.evict()
}

// evict drops the pages opened longest ago, past the limit, but those whose
// data is still on its way, and the newest page whose load may start just
// after insertion. Pending work may temporarily exceed the retention limit.
func (c *Cache[K, V]) evict() {
	for len(c.order) > c.limit {
		i := 0
		for i < len(c.order)-1 && c.busy != nil && c.busy(c.items[c.order[i]]) {
			i++
		}
		if i == len(c.order)-1 {
			return
		}
		delete(c.items, c.order[i])
		c.order = slices.Delete(c.order, i, i+1)
	}
}
