/*
使用隔离图片服务和磁盘缓存验证列表封面离线恢复及异步结果的显示顺序。
*/

package app

import (
	"bytes"
	"image"
	"image/color"
	"image/draw"
	"image/png"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/egoist/mygo/ui"

	artcache "netease-music-mygo/internal/desktop/artwork"
	"netease-music-mygo/internal/netease"
)

// A disk-only cached cover stays usable with the server gone and appears without a recovery fade.
func TestListCoverRestoresFromDiskOffline(t *testing.T) {
	isolateUserData(t)
	img := image.NewRGBA(image.Rect(0, 0, 32, 32))
	draw.Draw(img, img.Bounds(), image.NewUniform(color.RGBA{R: 255, A: 255}), image.Point{}, draw.Src)
	var encoded bytes.Buffer
	if err := png.Encode(&encoded, img); err != nil {
		t.Fatal(err)
	}
	var requests atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		requests.Add(1)
		w.Header().Set("Cache-Control", "max-age=3600")
		w.Write(encoded.Bytes())
	}))
	t.Cleanup(server.Close)
	store := netease.NewArtworkStore(t.TempDir(), 1<<20)
	netease.UseImageCache(store, nil)
	t.Cleanup(func() { netease.UseImageCache(nil, nil); store.Close() })
	tr := netease.Track{ID: 1, Album: netease.Album{PicURL: server.URL + "/cover.png"}}
	// Sized upgrades CDN URLs to HTTPS; this injected loader maps only the
	// isolated fixture back to its HTTP listener, leaving production TLS intact.
	load := func(url string) (*ui.Bitmap, image.Image, int, error) {
		return artcache.LoadURL(strings.Replace(url, "https://", "http://", 1))
	}
	url := strings.Replace(netease.Sized(tr.Album.PicURL, 96), "https://", "http://", 1)
	if _, err := netease.Fetch(url); err != nil {
		t.Fatal(err)
	}
	server.Close()
	a := newTestApp()
	posts := make(chan func(), 8)
	a.art = artcache.New(artcache.Options{Post: func(fn func()) { posts <- fn }, Load: load})
	t.Cleanup(a.art.Close)
	view := ui.NewTester(func(c *ui.Context) {
		a.art.PageFrame(a.nav.Current, true)
		a.trackCover(c, &tr, 96, 38, 6).Label("列表封面")
	}, 200, 100)
	wait(t, posts, "disk cover decode")()
	view.Frame()
	assertListCoverRed(t, view)
	s := a.art.Stats()
	if requests.Load() != 1 || s.Loads != 1 || s.ListPlaceholders == 0 {
		t.Fatalf("disk recovery did not exercise one asynchronous cache restore: HTTP=%d stats=%+v", requests.Load(), s)
	}
	t.Logf("disk restore: HTTP=%d stats=%+v", requests.Load(), s)
}
