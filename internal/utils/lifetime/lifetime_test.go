/*
验证订阅逆序释放、重复关闭及关闭后新增订阅的处理。
*/

package lifetime

import (
	"slices"
	"testing"
)

// Closing releases subscriptions once in reverse order, including late registrations.
func TestSubscriptionsClosesSubscriptions(t *testing.T) {
	var l Subscriptions
	var calls []int
	l.Listen(func() { calls = append(calls, 1) })
	l.Listen(func() { calls = append(calls, 2) })
	l.Close()
	l.Close()
	l.Listen(func() { calls = append(calls, 3) })
	if !l.Closed || !slices.Equal(calls, []int{2, 1, 3}) {
		t.Fatalf("closed=%v calls=%v", l.Closed, calls)
	}
}
