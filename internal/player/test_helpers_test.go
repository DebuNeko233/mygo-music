package player

// downloaded returns an in-memory download that has all of b in already.
func downloaded(b []byte) *download {
	d := opened(nil, int64(len(b)))
	d.pages = make(map[int64][]byte)
	for off := 0; off < len(b); off += downloadPageSize {
		d.pages[int64(off/downloadPageSize)] = b[off:min(off+downloadPageSize, len(b))]
	}
	return d
}

// prune is the test's equivalent of maintenance after resetting spare space.
func (c *Cache) prune(keep string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.spare = 0
	c.pruneLocked(keep, 0)
}
