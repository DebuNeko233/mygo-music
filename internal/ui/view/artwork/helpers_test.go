/*
提供封面缓存测试使用的位图、缓存实例及异步结果等待辅助方法。
*/

package artwork

import (
	"testing"
	"time"

	"github.com/egoist/mygo/ui"
)

func newArtCache(post func(func()), resources ...func(func(), func())) *Cache {
	options := Options{Post: post}
	if len(resources) > 0 {
		options.PostResource = resources[0]
	}
	return New(options)
}
func bitmap(value *Image) *ui.Bitmap { return value.ImageBitmap() }
func wait[T any](t *testing.T, ch chan T, what string) T {
	t.Helper()
	select {
	case v := <-ch:
		return v
	case <-time.After(time.Second):
		t.Fatalf("%s wasn't asked for", what)
		panic("unreachable")
	}
}
func refreshPost(t *testing.T, posts chan func()) {
	t.Helper()
	select {
	case f := <-posts:
		f()
	case <-time.After(3 * time.Second):
		t.Fatal("refresh never posted")
	}
}
