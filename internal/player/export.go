/*
复制独立完整的音频用于导出，复用匹配缓存且不修改正在播放的资源。
*/

package player

import (
	"context"
	"errors"
	"io"
	"os"
	"time"

	"netease-music-mygo/internal/netease"
)

// CopyAudio copies an independent, complete stream. It never borrows the
// playing decoder or writes into a retained cache file. Download permission
// must have been checked before calling, even when the bytes are cached.
func (c *Cache) CopyAudio(ctx context.Context, id int64, st *netease.Stream, dst *os.File, progress func(int64, int64)) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	var d *download
	if c != nil && keepable(st.Level, st.Type) && !c.stopped() {
		if cached, info := c.openKept(id, st.Level, ""); cached != nil {
			if st.Size > 0 && info.Size == st.Size && info.Type == st.Type {
				d = cached
			} else {
				cached.Close()
			}
		}
	}
	if d == nil {
		d = startDownload(st.URL, st.Size, nil, nil)
	}
	// This worker may wait for I/O cleanup before reporting cancellation or quit.
	defer d.halt()
	stop := context.AfterFunc(ctx, d.Close)
	defer stop()
	buf := make([]byte, 64<<10)
	var off int64
	var last time.Time
	for {
		n, err := d.ReadAt(buf, off)
		if e := ctx.Err(); e != nil {
			return e
		}
		if n > 0 {
			written, e := dst.Write(buf[:n])
			if e != nil {
				return e
			}
			if written != n {
				return io.ErrShortWrite
			}
			off += int64(n)
			if progress != nil && time.Since(last) >= 150*time.Millisecond {
				progress(off, st.Size)
				last = time.Now()
			}
		}
		if errors.Is(err, io.EOF) {
			if e := d.Err(); e != nil {
				return e
			}
			if off == 0 || st.Size > 0 && off != st.Size {
				return io.ErrUnexpectedEOF
			}
			if progress != nil {
				progress(off, off)
			}
			return nil
		}
		if err != nil {
			return err
		}
	}
}
