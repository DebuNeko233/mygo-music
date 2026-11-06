package player

import (
	"errors"
	"io"
	"sync"
)

type decodeError struct{ err error }

func (e *decodeError) Error() string { return "audio decode: " + e.err.Error() }
func (e *decodeError) Unwrap() error { return e.err }

// decoderFailure carries a decoder's failure from the audio worker to the UI.
// The audio library's end callback alone cannot distinguish an error from EOF.
type decoderFailure struct {
	mu  sync.Mutex
	err error
}

func (f *decoderFailure) record(err error) {
	if err == nil || errors.Is(err, io.EOF) {
		return
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.err == nil {
		f.err = &decodeError{err}
	}
}

func (f *decoderFailure) load() error {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.err
}

func (f *decoderFailure) clear() {
	f.mu.Lock()
	f.err = nil
	f.mu.Unlock()
}
