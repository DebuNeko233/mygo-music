package player

import (
	"cmp"
	"errors"
	"io"
	"math"
)

var errAudioOffset = errors.New("audio: invalid byte offset")

// ReadAt waits for the requested bytes, independent of memory/file backing or
// the order in which HTTP ranges arrive. Sparse cache holes are never decoded.
func (d *download) ReadAt(p []byte, off int64) (int, error) {
	return d.readAheadAt(p, off, len(p))
}

// readAheadAt waits for only the caller's bytes, then fills any already available
// read-ahead space. Small codec headers must not wait for a whole 32 KiB buffer.
func (d *download) readAheadAt(p []byte, off int64, need int) (int, error) {
	if off < 0 || int64(len(p)) > math.MaxInt64-off {
		return 0, errAudioOffset
	}
	if len(p) == 0 {
		return 0, nil
	}
	d.mu.Lock()
	defer d.mu.Unlock()
	end := off + int64(need)
	for {
		wanted := end
		if d.length >= 0 {
			wanted = min(wanted, d.length)
		}
		if d.rangeEnd(off) >= wanted || d.done || d.closed {
			break
		}
		// Ordinary sequential reads follow the current response. Only a real jump
		// (e.g. MP4/Ogg tail metadata) changes the existing downloader's priority.
		if off > d.rangeEnd(d.next)+2*audioReadBuffer || off < d.next && d.rangeEnd(off) == off {
			d.seekToLocked(off)
		}
		d.cond.Wait()
	}
	if d.closed {
		return 0, cmp.Or(d.err, io.ErrClosedPipe)
	}
	n, err := d.readAt(p, off)
	if err != nil && err != io.EOF {
		return n, err
	}
	if n < need {
		return n, cmp.Or(d.err, io.EOF)
	}
	return n, nil
}

// decoderReader supplies the same seekable view to local files, retained cache
// files and in-flight downloads. Indexed containers need their total byte size.
func (d *download) decoderReader() (*io.SectionReader, error) {
	d.mu.Lock()
	for d.length < 0 && !d.done && !d.closed {
		d.cond.Wait()
	}
	if d.closed {
		d.mu.Unlock()
		return nil, io.ErrClosedPipe
	}
	size := d.length
	if size < 0 {
		size = d.rangeEnd(0)
	}
	if size <= 0 {
		err := cmp.Or(d.err, io.ErrUnexpectedEOF)
		d.mu.Unlock()
		return nil, err
	}
	d.mu.Unlock()
	return io.NewSectionReader(d.decoderSource(), 0, size), nil
}

// decoderSource needs no transport length. Sequential containers can use their
// own headers or EOF while indexed containers add a bounded SectionReader.
func (d *download) decoderSource() *bufferedReaderAt {
	d.mu.Lock()
	defer d.mu.Unlock()
	var source io.ReaderAt = d
	if d.local && d.file != nil {
		source = d.file
	}
	return &bufferedReaderAt{source: source}
}
