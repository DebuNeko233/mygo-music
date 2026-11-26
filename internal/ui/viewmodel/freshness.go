/*
定义列表刷新间隔，并判断已缓存内容是否需要重新验证。
*/

package viewmodel

import (
	"time"
)

// ListsFresh is the existing revalidation interval for cached library and recent lists.
const ListsFresh = 30 * time.Second

// ListDue reports whether a list has not been requested or has reached its refresh interval.
func ListDue(at, now time.Time) bool { return at.IsZero() || now.Sub(at) >= ListsFresh }
