/*
验证背景图片编码结果不会保留可增长缓冲区的多余容量。
*/

package artwork

import (
	"image"
	"math/rand/v2"
	"os"
	"path/filepath"
	"testing"
)

// Long-lived encoded backgrounds retain at most allocator rounding, for both
// opaque JPEG and transparent PNG, rather than the encoder's growth capacity.
func TestEncodedPictureDoesNotRetainGrowthBuffer(t *testing.T) {
	for _, alpha := range []byte{255, 128} {
		img := image.NewRGBA(image.Rect(0, 0, 400, 300))
		r := rand.New(rand.NewPCG(1, 2))
		for i := 0; i < len(img.Pix); i += 4 {
			img.Pix[i] = byte(r.Uint32()) & alpha
			img.Pix[i+1] = byte(r.Uint32()) & alpha
			img.Pix[i+2] = byte(r.Uint32()) & alpha
			img.Pix[i+3] = alpha
		}
		data := encodePicture(img)
		if len(data) == 0 || cap(data)-len(data) >= 8192 {
			t.Fatalf("alpha=%d retained %d spare bytes for %d encoded bytes", alpha, cap(data)-len(data), len(data))
		}
		decoded, err := DecodeImage(data)
		if err != nil || decoded.Bounds() != img.Bounds() {
			t.Fatalf("encoding no longer reconstructs picture: %v", err)
		}
	}
}

// Background admission must preserve the animated-WebP first-frame fallback
// even though the initial file header is not a standalone decodable image.
func TestAnimatedWebPPicture(t *testing.T) {
	b, err := os.ReadFile("testdata/animated.webp")
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), "animated.webp")
	if err := os.WriteFile(path, b, 0600); err != nil {
		t.Fatal(err)
	}
	if pic := DecodePicture(path); pic == nil || pic.W != 24 || pic.H != 24 {
		t.Fatal("background no longer shows the first WebP frame")
	}
}
