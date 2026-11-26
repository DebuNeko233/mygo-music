/*
验证不同背景图片格式的读取及最大尺寸限制。
*/

package artwork

import (
	"image"
	"image/color"
	"image/gif"
	"image/jpeg"
	"image/png"
	"os"
	"path/filepath"
	"testing"
)

// A picture picked is read, kept as the pixels of its size, and no longer
// on its longer side than a picture takes.
func TestPictureIsReadAtItsSize(t *testing.T) {
	path := filepath.Join(t.TempDir(), "wide.png")
	writePicture(t, path, 4000, 100, color.RGBA{R: 255, G: 255, B: 255, A: 255})
	pic := DecodePicture(path)
	if pic == nil {
		t.Fatal("could not read the picture")
	}
	if pic.W != 2000 || pic.H != 50 {
		t.Errorf("kept %v×%v, want 2000×50", pic.W, pic.H)
	}
	if DecodePicture(filepath.Join(t.TempDir(), "missing.png")) != nil {
		t.Error("read a picture that is not there")
	}
}

// Every format the picker offers is read, so a picture picked is never one
// the app cannot show.
func TestEveryPictureFormatIsRead(t *testing.T) {
	dir := t.TempDir()
	img := image.NewRGBA(image.Rect(0, 0, 3, 2))
	for i := range img.Pix {
		img.Pix[i] = 255
	}
	for _, name := range []string{"still.png", "still.jpg", "still.gif"} {
		path := filepath.Join(dir, name)
		f, err := os.Create(path)
		if err != nil {
			t.Fatal(err)
		}
		switch filepath.Ext(name) {
		case ".png":
			err = png.Encode(f, img)
		case ".jpg":
			err = jpeg.Encode(f, img, nil)
		default:
			err = gif.Encode(f, img, nil)
		}
		f.Close()
		if err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		pic := DecodePicture(path)
		if pic == nil {
			t.Errorf("%s: could not read the picture", name)
			continue
		}
		if pic.W != 3 || pic.H != 2 {
			t.Errorf("%s: kept %v×%v, want 3×2", name, pic.W, pic.H)
		}
	}
}

// writePicture writes a picture of w×h pixels of one colour at path.
func writePicture(t *testing.T, path string, w, h int, fill color.RGBA) {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			img.SetRGBA(x, y, fill)
		}
	}
	f, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	if err := png.Encode(f, img); err != nil {
		t.Fatal(err)
	}
}
