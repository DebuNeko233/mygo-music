/*
按图片头声明的源像素量预留共享解码额度，限制封面和背景同时展开的临时内存。
*/

package artwork

import (
	"errors"
	"image"
	"sync"
)

const decodeByteLimit int64 = 64 << 20

// Covers and backgrounds share this allowance, in addition to their worker
// and retained-cache limits. A source larger than the allowance runs alone;
// this is an admission estimate, not a bound on a decoder or the process RSS.
var pictureDecodes = newDecodeBudget(decodeByteLimit)

type decodeBudget struct {
	ready       *sync.Cond
	used, limit int64
	next, head  uint64
}

func newDecodeBudget(limit int64) *decodeBudget {
	return &decodeBudget{ready: sync.NewCond(&sync.Mutex{}), limit: limit}
}

func (b *decodeBudget) reserve(bytes int64) func() {
	bytes = min(max(1, bytes), b.limit)
	b.ready.L.Lock()
	ticket := b.next
	b.next++
	// FIFO admission lets a waiting large background drain the allowance;
	// a stream of small covers must not keep taking its remaining space.
	for ticket != b.head || b.used > b.limit-bytes {
		b.ready.Wait()
	}
	b.used += bytes
	b.head++
	b.ready.Broadcast()
	b.ready.L.Unlock()
	return func() {
		b.ready.L.Lock()
		b.used -= bytes
		b.ready.Broadcast()
		b.ready.L.Unlock()
	}
}

// Charge up to two full source buffers (including 16-bit formats), a scaled
// destination and its bitmap reconstruction, and the input bytes. Saturate
// before multiplying so a corrupt or very large header cannot wrap the cost.
func decodeCost(cfg image.Config, side int, encoded int64) (int64, error) {
	w, h := int64(cfg.Width), int64(cfg.Height)
	if w <= 0 || h <= 0 {
		return 0, errors.New("invalid image dimensions")
	}
	if w > decodeByteLimit/16 || h > decodeByteLimit/16/w {
		return decodeByteLimit, nil
	}
	source := 16 * w * h
	if side > 0 && max(w, h) > int64(side) {
		long := max(w, h)
		w, h = max(1, w*int64(side)/long), max(1, h*int64(side)/long)
	}
	return min(decodeByteLimit, source+8*w*h+min(max(encoded, 0), decodeByteLimit)), nil
}
