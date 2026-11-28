/*
验证预取大封面首次显示时不重新解码，并保留小封面的像素释放行为。
*/

package artwork

import (
	"bytes"
	"fmt"
	"image"
	"image/color"
	"image/png"
	"io"
	"sync/atomic"
	"testing"
	"testing/synctest"
	"time"

	"github.com/egoist/mygo/ui"
)

var pixelFixtureID atomic.Uint64

// A prefetched large cover stays decoded even when its entrance is much later
// than the small-cover release delay. Small covers still release their pixels.
func TestPrefetchedCoverPixelsSurviveUntilEntrance(t *testing.T) {
	for _, backdrop := range []bool{false, true} {
		t.Run(fmt.Sprintf("backdrop=%v", backdrop), func(t *testing.T) {
			synctest.Test(t, func(t *testing.T) {
				var decodes atomic.Int64
				// A PNG wrapper counts real decoder work without inspecting
				// private Bitmap fields or adding production test hooks.
				magic := fmt.Sprintf("NYAGO-PIXELS-%d:", pixelFixtureID.Add(1))
				skip := func(r io.Reader) error {
					_, err := io.CopyN(io.Discard, r, int64(len(magic)))
					return err
				}
				image.RegisterFormat(magic, magic, func(r io.Reader) (image.Image, error) {
					if err := skip(r); err != nil {
						return nil, err
					}
					decodes.Add(1)
					return png.Decode(r)
				}, func(r io.Reader) (image.Config, error) {
					if err := skip(r); err != nil {
						return image.Config{}, err
					}
					return png.DecodeConfig(r)
				})
				src := image.NewRGBA(image.Rect(0, 0, 16, 16))
				for y := range 16 {
					for x := range 16 {
						src.SetRGBA(x, y, color.RGBA{R: 180, G: 60, B: 90, A: 255})
					}
				}
				var encoded bytes.Buffer
				encoded.WriteString(magic)
				if err := png.Encode(&encoded, src); err != nil {
					t.Fatal(err)
				}
				posts := make(chan func(), 8)
				c := New(Options{Post: func(fn func()) { posts <- fn }, Load: func(string) (*ui.Bitmap, image.Image, int, error) {
					bmp, err := ui.DecodeBitmap(encoded.Bytes())
					return bmp, src, encoded.Len(), err
				}})
				defer c.Close()
				c.Prefetch("fixture://entrance", backdrop)
				(<-posts)()
				time.Sleep(3 * artRelease)
				synctest.Wait()
				for len(posts) > 0 {
					(<-posts)()
				}
				art := c.Get("fixture://entrance", backdrop)
				if art == nil || art.Bitmap == nil {
					t.Fatal("prefetched cover was not available")
				}
				before := decodes.Load()
				tt := ui.NewTester(func(ctx *ui.Context) { ui.Image(ctx, art.Bitmap).Size(16, 16) }, 16, 16)
				if got := tt.Image().RGBAAt(8, 8); got != src.RGBAAt(8, 8) {
					t.Fatalf("first entrance lost the decoded cover: %v", got)
				}
				want := int64(1)
				if backdrop {
					want = 0
				}
				if got := decodes.Load() - before; got != want {
					t.Fatalf("first drawing decoded %d times, want %d", got, want)
				}
			})
		})
	}
}
