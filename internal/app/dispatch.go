package app

import "sync"

// appDispatch owns callbacks until they run or their resources are discarded.
// The mutex protects publication and shutdown, never application state.
type appDispatch struct {
	postMu      sync.Mutex
	postTo      func(func(), bool)
	kept        []func()
	postsClosed bool
	resources   map[*postedResource]struct{}
}

type postedResource struct {
	once    sync.Once
	discard func()
}

func (a *app) post(fn func()) { a.dispatch(fn, true) }

// postResource transfers ownership even when shutdown overtakes a worker.
// discard must be safe on the worker thread and must not change view state.
func (a *app) postResource(fn, discard func()) {
	r := &postedResource{discard: discard}
	a.postMu.Lock()
	if a.postsClosed {
		a.postMu.Unlock()
		r.once.Do(discard)
		return
	}
	if a.resources == nil {
		a.resources = make(map[*postedResource]struct{})
	}
	a.resources[r] = struct{}{}
	a.postMu.Unlock()
	a.dispatch(func() {
		a.postMu.Lock()
		delete(a.resources, r)
		a.postMu.Unlock()
		r.once.Do(fn)
	}, true)
}

// closePosts also releases results already queued in the native event loop.
func (a *app) closePosts() {
	a.postMu.Lock()
	a.postsClosed = true
	a.postTo, a.kept = nil, nil
	resources := a.resources
	a.resources = nil
	a.postMu.Unlock()
	for r := range resources {
		r.once.Do(r.discard)
	}
}

// postIfChanged lets a worker finish bookkeeping without rebuilding the view
// when the result does not change what any window shows.
func (a *app) postIfChanged(update func() bool) {
	a.dispatch(func() {
		if update() {
			a.redraw()
		}
	}, false)
}

// dispatch queues early results until a dispatcher exists. Native windows and
// headless tests each own their queue; a late worker cannot use another app's
// dispatcher. Never invoke callbacks while holding postMu.
func (a *app) dispatch(fn func(), redraw bool) {
	a.postMu.Lock()
	if a.postsClosed {
		a.postMu.Unlock()
		return
	}
	run := func() {
		a.postMu.Lock()
		closed := a.postsClosed
		a.postMu.Unlock()
		if !closed {
			fn()
		}
	}
	send := a.postTo
	if send == nil {
		a.kept = append(a.kept, run)
		a.postMu.Unlock()
		return
	}
	a.postMu.Unlock()
	send(run, redraw)
}

// flushKept runs what post kept while the window was being made.
func (a *app) flushKept() {
	a.postMu.Lock()
	kept := a.kept
	a.kept = nil
	a.postMu.Unlock()
	for _, fn := range kept {
		fn()
	}
	if len(kept) > 0 {
		a.redraw()
	}
}

func (a *app) redraw() {
	if a.quitting {
		return
	}
	if a.win != nil {
		a.win.Invalidate()
	}
	if a.notch != nil && a.notch.win != nil {
		a.notch.win.Invalidate()
	}
	if a.lyrics != nil && a.lyrics.win != nil {
		a.lyrics.win.Invalidate()
	}
	if a.settingsWin != nil {
		a.settingsWin.Invalidate()
	}
	a.redrawLocalFolders()
}
