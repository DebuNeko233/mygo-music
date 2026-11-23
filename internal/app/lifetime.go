package app

// nativeLifetime belongs to the UI thread. Each owner releases its native
// subscriptions once, including when startup only created some components.
type nativeLifetime struct {
	closed bool
	offs   []func()
}

func (l *nativeLifetime) listen(off func()) {
	if l.closed {
		off()
		return
	}
	l.offs = append(l.offs, off)
}

func (l *nativeLifetime) close() {
	if l.closed {
		return
	}
	l.closed = true
	for i := len(l.offs) - 1; i >= 0; i-- {
		l.offs[i]()
	}
	l.offs = nil
}

type appLifetime struct {
	native        nativeLifetime
	released      bool
	initialized   bool
	sessionCancel func()
}
