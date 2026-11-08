package netease

import (
	"context"
	"errors"
	"io"
	"net"
)

// responseError preserves a status code without parsing user-facing messages.
// Both HTTP gateways and API bodies can report temporary service failures.
type responseError struct {
	code   int
	status string
}

func (e responseError) Error() string { return e.status }

// RetryableError allows automatic recovery only for connection failures and
// temporary service responses. Missing, refused and malformed data waits for
// an explicit refresh; an online system route alone cannot make it recoverable.
func RetryableError(err error) bool {
	if err == nil || errors.Is(err, context.Canceled) {
		return false
	}
	var status responseError
	if errors.As(err, &status) {
		return status.code >= 500 && status.code < 600 || status.code == 408 || status.code == 429
	}
	var network net.Error
	return errors.Is(err, ErrOffline) || errors.As(err, &network) || errors.Is(err, io.ErrUnexpectedEOF) || errors.Is(err, io.EOF)
}
