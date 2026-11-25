package lifetime

// Subscriptions belongs to the UI thread. Each owner releases its native
// subscriptions once, including when startup only created some components.
type Subscriptions struct {
	Closed bool
	offs   []func()
}

func (l *Subscriptions) Listen(off func()) {
	if l.Closed {
		off()
		return
	}
	l.offs = append(l.offs, off)
}

func (l *Subscriptions) Close() {
	if l.Closed {
		return
	}
	l.Closed = true
	for i := len(l.offs) - 1; i >= 0; i-- {
		l.offs[i]()
	}
	l.offs = nil
}
