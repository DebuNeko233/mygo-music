package player

// Cache relocation closes readers and downloads before moving files, while
// retaining playback state for resumption on the main thread.

// track registers a shared download so relocation can close every file handle.
func (c *Cache) track(d *download) *download {
	if c == nil || d == nil {
		return d
	}
	c.mu.Lock()
	if c.active == nil {
		c.active = map[*download]struct{}{}
	}
	c.active[d] = struct{}{}
	c.mu.Unlock()
	d.mu.Lock()
	gone := d.users <= 0
	d.onRelease = func() {
		c.mu.Lock()
		delete(c.active, d)
		c.mu.Unlock()
	}
	d.mu.Unlock()
	if gone {
		// Release may have finished before onRelease was installed.
		c.mu.Lock()
		delete(c.active, d)
		c.mu.Unlock()
	}
	return d
}

// Quiesce blocks new cache access and waits for active downloads to close.
// The returned closure reopens the original cache if relocation fails.
func (c *Cache) Quiesce() func() {
	if c == nil {
		return func() {}
	}
	c.mu.Lock()
	c.blocked = true
	active := make([]*download, 0, len(c.active))
	for d := range c.active {
		active = append(active, d)
	}
	c.mu.Unlock()
	// halt invokes onRelease, which takes the same lock.
	for _, d := range active {
		d.halt()
	}
	return func() {
		c.mu.Lock()
		c.blocked = false
		c.mu.Unlock()
	}
}

// SuspendCache closes audio before relocation and retains its position.
// Queue, seek and play controls still update state; the returned closure
// applies that state after relocation. Both calls belong on the main thread.
func (p *Player) SuspendCache() func() {
	position := p.Position()
	// Relocating files does not end the Dawn session. Settle only the audible
	// interval so the inactive reload cannot add wall-clock time.
	p.pause()
	p.cacheChanging = true
	// Old callbacks may refer to the moving directory. Invalidate their
	// generation and settle busy flags even if resume does not reload audio.
	p.gen++
	p.lyricBusy, p.coverBusy = false, false
	if p.stream != nil {
		if dl, _ := p.stream.source(); dl != nil {
			dl.Close()
		}
	}
	if p.out != nil {
		p.out.Close()
	}
	p.out, p.stream, p.info = nil, nil, nil
	p.fade.attach(nil, 1)
	p.seeking = false
	p.pending = position
	p.loading = p.track != nil
	p.sync()
	return func() {
		p.cacheChanging = false
		if p.awaiting || p.track == nil || p.at < 0 || p.at >= len(p.order) {
			p.loading = false
			p.sync()
			return
		}
		p.loadSkipping(p.at, 0, p.playing, p.pending)
	}
}
