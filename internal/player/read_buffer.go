package player

import (
	"io"
	"sync"
)

const audioReadBuffer = 32 << 10

// Local decoders read short headers and packets, often seeking between them.
// Cache those reads below SectionReader so its position and seeking semantics
// stay intact. Large reads go straight to the file without an extra copy.
type bufferedReaderAt struct {
	source io.ReaderAt
	mu     sync.Mutex // ReaderAt permits concurrent calls
	buf    []byte
	at     int64
	n      int
	err    error
}

func (r *bufferedReaderAt) ReadAt(p []byte, off int64) (int, error) {
	if len(p) == 0 || len(p) >= audioReadBuffer || off < 0 {
		return r.source.ReadAt(p, off)
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	read := 0
	for len(p) > 0 {
		if off < r.at || off >= r.at+int64(r.n) {
			if r.buf == nil {
				r.buf = make([]byte, audioReadBuffer)
			}
			r.at = off
			if d, ok := r.source.(*download); ok {
				r.n, r.err = d.readAheadAt(r.buf, off, min(len(p), len(r.buf)))
			} else {
				r.n, r.err = r.source.ReadAt(r.buf, off)
			}
			if r.n == 0 {
				if r.err != nil {
					return read, r.err
				}
				return read, io.ErrNoProgress
			}
		}
		n := copy(p, r.buf[off-r.at:r.n])
		p, off, read = p[n:], off+int64(n), read+n
		if len(p) > 0 && r.err != nil {
			return read, r.err
		}
	}
	return read, nil
}
