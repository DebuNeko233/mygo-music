package player

// This file is what moves the song cache's folder under the player: the
// fetches into it stop, the files they made go, and what plays is let go of
// at where it is, to play on from there once the folder has moved.

// track puts d under the cache's eye, so that a move of its folder stops it
// first. It comes back as it was given, for download and lookup to return.
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
	gone := d.users <= 0 // let go of already: nothing to stop
	d.onRelease = func() {
		c.mu.Lock()
		delete(c.active, d)
		c.mu.Unlock()
	}
	d.mu.Unlock()
	if gone {
		// What it says was missed: take it back out.
		c.mu.Lock()
		delete(c.active, d)
		c.mu.Unlock()
	}
	return d
}

// Quiesce stops every fetch into the cache, and waits for it to stop, so that
// its folder may move. It then keeps the cache taking nothing: the folder it
// moves goes, and what comes late would write into a folder that is gone. The
// closure it returns lets the cache take songs again, for a move that failed.
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
	// Not under the lock: a fetch let go of takes it, to say it has gone.
	for _, d := range active {
		d.halt()
	}
	return func() {
		c.mu.Lock()
		c.blocked = false
		c.mu.Unlock()
	}
}

// SuspendCache stops what plays, for the cache's folder to move: the song is
// let go of, its file closed even as the transcription reads it, and where it
// was is kept, to play on from there once the folder has moved. Until then
// the queue takes a next track, a seek or a toggle, which apply on resume,
// the closure it returns, on the main thread.
func (p *Player) SuspendCache() func() {
	position := p.Position()
	p.cacheChanging = true
	// What loads now is stale: it would read the folder that moves.
	p.gen++
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
