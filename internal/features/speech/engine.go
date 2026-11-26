/*
串行管理原生转写器的使用、模型移除和关闭，避免转写与释放并发冲突。
*/

package speech

import (
	"context"
	"sync"
	"time"
)

// Transcriber is the recognizer capability shared by the model lifecycle and episode jobs.
type Transcriber interface {
	Transcribe(context.Context, Source, time.Duration, time.Duration, func(Segment), func(time.Duration)) error
	Close()
}

// Engine serializes access to the native recognizer, including deletion and release.
type Engine struct {
	mu         sync.Mutex
	recognizer Transcriber
	closed     bool
}

func (e *Engine) Transcribe(ctx context.Context, dir string, src Source, from, until time.Duration, newRecognizer func(string) (Transcriber, error), said func(Segment), heard func(time.Duration)) error {
	e.mu.Lock()
	defer e.mu.Unlock()
	if e.closed {
		return context.Canceled
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	if e.recognizer == nil {
		var err error
		e.recognizer, err = newRecognizer(dir)
		if err != nil {
			return err
		}
	}
	return e.recognizer.Transcribe(ctx, src, from, until, said, heard)
}
func (e *Engine) Remove(dir string, remove func(string) error) error {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.close()
	return remove(dir)
}
func (e *Engine) close() {
	if e.recognizer != nil {
		e.recognizer.Close()
		e.recognizer = nil
	}
}
func (e *Engine) Release() { go func() { e.mu.Lock(); defer e.mu.Unlock(); e.close() }() }

// Close waits for a canceled transcription to leave the native recognizer.
// Unlike Release, it also prevents queued work from reopening the model.
func (e *Engine) Close() {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.closed = true
	e.close()
}
