/*
验证源像素解码预算在大小图片混合、超额图片及解码失败时的准入与归还。
*/

package artwork

import (
	"bytes"
	"image"
	"image/png"
	"os"
	"path/filepath"
	"testing"
	"testing/synctest"
)

// Small sources can overlap within the allowance; oversized sources wait for
// every active decoder and then block even a small source until completion.
func TestDecodeBudgetRunsOversizedSourceAlone(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		b := newDecodeBudget(100)
		first, second := b.reserve(40), b.reserve(50)
		large := make(chan func(), 1)
		go func() { large <- b.reserve(200) }()
		synctest.Wait()
		first()
		synctest.Wait()
		if len(large) != 0 {
			t.Fatal("oversized source started beside an active decoder")
		}
		small := make(chan func(), 1)
		go func() { small <- b.reserve(1) }()
		synctest.Wait()
		if len(small) != 0 {
			t.Fatal("small source bypassed the waiting large source")
		}
		second()
		release := <-large
		synctest.Wait()
		if len(small) != 0 {
			t.Fatal("small source started beside the oversized source")
		}
		release()
		(<-small)()
		if b.used != 0 {
			t.Fatal("decoder completion leaked admission bytes")
		}
	})
}

// Admission uses source pixels even when the requested output is a tiny cover;
// extreme header values saturate instead of overflowing or becoming negative.
func TestDecodeCostUsesSourceDimensions(t *testing.T) {
	small, err := decodeCost(image.Config{Width: 600, Height: 600}, 48, 1024)
	if err != nil || small >= decodeByteLimit {
		t.Fatalf("ordinary cover cannot share admission: %d, %v", small, err)
	}
	for _, dims := range [][2]int{{6000, 4000}, {int(^uint(0) >> 1), 2}} {
		cost, err := decodeCost(image.Config{Width: dims[0], Height: dims[1]}, 48, 1024)
		if err != nil || cost != decodeByteLimit {
			t.Fatalf("large source %v escaped admission: %d, %v", dims, cost, err)
		}
	}
	if _, err := decodeCost(image.Config{Width: 0, Height: 10}, 48, 0); err == nil {
		t.Fatal("accepted an empty source")
	}
}

// Both actual decode entry points wait before expanding pixels; malformed
// bodies release the allowance so the following valid decode can finish.
func TestCoverAndPictureShareDecodeAdmission(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		var encoded bytes.Buffer
		if err := png.Encode(&encoded, image.NewRGBA(image.Rect(0, 0, 32, 24))); err != nil {
			t.Fatal(err)
		}
		path := filepath.Join(t.TempDir(), "picture.png")
		if err := os.WriteFile(path, encoded.Bytes(), 0600); err != nil {
			t.Fatal(err)
		}
		release := pictureDecodes.reserve(decodeByteLimit)
		cover, picture := make(chan error, 1), make(chan *Picture, 1)
		go func() { _, _, _, err := DecodeSized(encoded.Bytes(), 16); cover <- err }()
		go func() { picture <- DecodePicture(path) }()
		synctest.Wait()
		if len(cover) != 0 || len(picture) != 0 {
			t.Fatal("a decoder expanded pixels without shared admission")
		}
		release()
		if err := <-cover; err != nil {
			t.Fatal(err)
		}
		if pic := <-picture; pic == nil || pic.W != 32 || pic.H != 24 {
			t.Fatal("background did not finish after admission returned")
		}
		bad := encoded.Bytes()[:33] // Valid dimensions, missing pixel data.
		if _, _, _, err := DecodeSized(bad, 16); err == nil {
			t.Fatal("accepted a truncated cover")
		}
		if err := os.WriteFile(path, bad, 0600); err != nil {
			t.Fatal(err)
		}
		if DecodePicture(path) != nil {
			t.Fatal("accepted a truncated background")
		}
		// This reserve would block forever if either failure leaked bytes.
		pictureDecodes.reserve(decodeByteLimit)()
	})
}
