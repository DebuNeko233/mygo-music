package netease

import (
	"context"
	"errors"
	"io"
	"net/http"
	"sync"
)

var ErrOffline = errors.New("network is offline")

var requests = struct {
	sync.RWMutex
	offline bool
	ctx     context.Context
	cancel  context.CancelCauseFunc
}{}

func init() { requests.ctx, requests.cancel = context.WithCancelCause(context.Background()) }

// SetOffline cancels requests and body reads of the old connection together.
// Reconnection gets a fresh context; a cancelled request cannot become live
// again. Local audio, image and page cache reads never pass through this gate.
func SetOffline(offline bool) {
	requests.Lock()
	defer requests.Unlock()
	if requests.offline == offline {
		return
	}
	requests.offline = offline
	if offline {
		requests.cancel(ErrOffline)
	} else {
		requests.ctx, requests.cancel = context.WithCancelCause(context.Background())
	}
}

func Offline() bool { requests.RLock(); defer requests.RUnlock(); return requests.offline }

// DoRequest keeps cancellation attached until the response body is closed,
// including downloads that already received headers before disconnecting.
func DoRequest(client *http.Client, req *http.Request) (*http.Response, error) {
	requests.RLock()
	gate, offline := requests.ctx, requests.offline
	requests.RUnlock()
	if offline {
		return nil, ErrOffline
	}
	ctx, cancel := context.WithCancelCause(req.Context())
	stop := context.AfterFunc(gate, func() { cancel(ErrOffline) })
	cleanup := func() { stop(); cancel(context.Canceled) }
	res, err := client.Do(req.WithContext(ctx))
	if err != nil {
		cause := context.Cause(ctx)
		cleanup()
		if errors.Is(cause, ErrOffline) {
			return nil, ErrOffline
		}
		return nil, err
	}
	res.Body = &networkBody{ReadCloser: res.Body, ctx: ctx, cleanup: cleanup}
	return res, nil
}

type networkBody struct {
	io.ReadCloser
	ctx     context.Context
	cleanup func()
}

func (b *networkBody) Read(p []byte) (int, error) {
	n, err := b.ReadCloser.Read(p)
	if err != nil && errors.Is(context.Cause(b.ctx), ErrOffline) {
		err = ErrOffline
	}
	return n, err
}
func (b *networkBody) Close() error { b.cleanup(); return b.ReadCloser.Close() }
