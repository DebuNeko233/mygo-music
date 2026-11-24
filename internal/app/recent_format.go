package app

import (
	"time"
)

// playedAt says when and on what the listener played something, as
// "12分钟前 · 手机".
func playedAt(at time.Time, device string, now time.Time) string {
	when := agoLabel(now.Sub(at))
	if where := deviceName(device); where != "" {
		return when + " · " + where
	}
	return when
}
