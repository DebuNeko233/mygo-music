package components

// NativeLifetime belongs to the UI thread. Each owner releases its native
// subscriptions once, including when startup only created some components.
type NativeLifetime struct {
	Closed bool
	offs   []func()
}

func (l *NativeLifetime) Listen(off func()) {
	if l.Closed {
		off()
		return
	}
	l.offs = append(l.offs, off)
}

func (l *NativeLifetime) Close() {
	if l.Closed {
		return
	}
	l.Closed = true
	for i := len(l.offs) - 1; i >= 0; i-- {
		l.offs[i]()
	}
	l.offs = nil
}
