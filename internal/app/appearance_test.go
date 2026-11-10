package app

import (
	"fmt"
	"image"
	"image/color"
	"image/gif"
	"image/jpeg"
	"image/png"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/egoist/mygo/ui"
)

// Choosing an accent in the settings colours the app with it, in the
// lightness of whichever appearance it is in.
func TestChoosingAccentColorsIt(t *testing.T) {
	old := accentChosen
	defer func() { accentChosen = old }()
	a := newTestApp()
	a.settings = defaultSettings()
	tt := ui.NewTester(a.settingsView, 780, 640)
	showSettingsPage(t, tt, "外观")
	tt.Frame()
	// The select opens on a click, and the option under the pointer picks
	// itself.
	if err := tt.Click("选择主题颜色"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	if err := tt.Click("湖蓝"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	if a.settings.Accent != "湖蓝" {
		t.Fatalf("chose %q", a.settings.Accent)
	}
	if got, want := paletteFor(&ui.Theme{}).accent, accentColor("湖蓝", false); got != want {
		t.Errorf("light accent %v, want %v", got, want)
	}
	if got, want := paletteFor(&ui.Theme{Dark: true}).accent, accentColor("湖蓝", true); got != want {
		t.Errorf("dark accent %v, want %v", got, want)
	}
	// Following the appearance again gives its own accent back.
	if err := tt.Click("选择主题颜色"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	if err := tt.Click("跟随外观"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	if a.settings.Accent != "" {
		t.Errorf("still %q", a.settings.Accent)
	}
	if got, want := paletteFor(&ui.Theme{}).accent, ui.Oklch(0.6, 0.21, 24); got != want {
		t.Errorf("accent %v, want the appearance's %v", got, want)
	}
}

// The toast uses the app's palette in each appearance: it used the default
// MyGo theme instead, so its dark-mode fill did not match the app.
func TestToastUsesAppearancePalette(t *testing.T) {
	for _, dark := range []bool{false, true} {
		var theme ui.Theme
		tt := ui.NewTesterWith(func(c *ui.Context) {
			a := newTestApp()
			a.settings = defaultSettings()
			a.view(c)
			theme = *c.Theme()
			if ui.Button(c, "Save").Clicked() {
				c.Toast("Saved")
			}
		}, 800, 600, ui.TesterOptions{Dark: dark})
		if err := tt.Click("Save"); err != nil {
			t.Fatal(err)
		}
		tt.Advance(time.Second)
		tt.Frame()
		r, ok := tt.Find("Saved")
		if !ok || r.W == 0 || r.H == 0 {
			t.Fatalf("dark %v: toast bounds %+v", dark, r)
		}
		px := tt.Image().RGBAAt(int(r.X)-3, int(r.Y+r.H/2))
		got := ui.Color{R: px.R, G: px.G, B: px.B, A: px.A}
		if want := paletteFor(&ui.Theme{Dark: dark}).text; got != want {
			t.Errorf("dark %v: toast fill %v, want %v", dark, got, want)
		}
		if theme.Inverse != got || theme.InverseText != paletteFor(&ui.Theme{Dark: dark}).bg {
			t.Errorf("dark %v: inverse colors %v on %v", dark, theme.InverseText, theme.Inverse)
		}
	}
}

// A page's pictures keep the ones chosen once each, in the order they were
// chosen, and take no file that is not a picture.
func TestAddingPicturesKeepsPicturesOnce(t *testing.T) {
	dir := t.TempDir()
	pic := filepath.Join(dir, "cover.PNG")
	writePicture(t, pic, 4, 2, color.RGBA{R: 255, G: 255, B: 255, A: 255})
	other := filepath.Join(dir, "other.jpg")
	writePicture(t, other, 2, 2, color.RGBA{R: 255, G: 255, B: 255, A: 255})
	notes := filepath.Join(dir, "notes.txt")
	if err := os.WriteFile(notes, []byte("not a picture"), 0o600); err != nil {
		t.Fatal(err)
	}
	old := accentChosen
	defer func() { accentChosen = old }()
	a := newTestApp()
	a.settings = defaultSettings()
	var list []string
	a.addPictures([]string{notes, pic, other, pic}, &list)
	if len(list) != 2 || list[0] != pic || list[1] != other {
		t.Fatalf("kept %v", list)
	}
}

// A picture picked is read, kept as the pixels of its size, and no longer
// on its longer side than a picture takes.
func TestPictureIsReadAtItsSize(t *testing.T) {
	path := filepath.Join(t.TempDir(), "wide.png")
	writePicture(t, path, 4000, 100, color.RGBA{R: 255, G: 255, B: 255, A: 255})
	pic := decodePicture(path)
	if pic == nil {
		t.Fatal("could not read the picture")
	}
	if pic.w != 2000 || pic.h != 50 {
		t.Errorf("kept %v×%v, want 2000×50", pic.w, pic.h)
	}
	if decodePicture(filepath.Join(t.TempDir(), "missing.png")) != nil {
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
		pic := decodePicture(path)
		if pic == nil {
			t.Errorf("%s: could not read the picture", name)
			continue
		}
		if pic.w != 3 || pic.h != 2 {
			t.Errorf("%s: kept %v×%v, want 3×2", name, pic.w, pic.h)
		}
	}
}

// A picture covers the page it stands on, centred, keeping its own shape:
// what is outside the page is cut off, never squeezed in.
func TestCoverSourceKeepsTheShape(t *testing.T) {
	for _, tc := range []struct {
		name string
		page ui.Rect
		w, h float32
		want ui.Rect
	}{
		{"a wide picture in a tall page", ui.Rect{W: 100, H: 400}, 200, 100, ui.Rect{X: 87.5, W: 25, H: 100}},
		{"a wide picture in a wide page", ui.Rect{W: 400, H: 100}, 200, 100, ui.Rect{Y: 25, W: 200, H: 50}},
		{"the same shape", ui.Rect{W: 200, H: 100}, 200, 100, ui.Rect{W: 200, H: 100}},
	} {
		src := coverSource(tc.page, tc.w, tc.h)
		if src != tc.want {
			t.Errorf("%s: source %+v, want %+v", tc.name, src, tc.want)
		}
		// The part taken is as wide for its height as the page it fills.
		if got, want := src.W/src.H, tc.page.W/tc.page.H; got-want > 0.001 || want-got > 0.001 {
			t.Errorf("%s: source %v:1 in a page %v:1", tc.name, got, want)
		}
	}
}

// The picture a page is given covers it, and shows as deep as the opacity
// says.
func TestBackdropPictureCoversThePage(t *testing.T) {
	old := accentChosen
	defer func() { accentChosen = old }()
	path := filepath.Join(t.TempDir(), "red.png")
	writePicture(t, path, 8, 8, color.RGBA{R: 255, A: 255})
	a := newTestApp()
	a.settings = defaultSettings()
	a.settings.BackdropHome = []string{path}
	a.settings.BackdropOpacity = 1
	// Read, as the app reads them: off the main thread, kept here.
	a.pics = map[string]*backdropPicture{path: decodePicture(path)}
	tt := ui.NewTester(a.view, 900, 600)
	tt.Frame()
	const w, h = 900, 600
	if got := redPixels(tt.Image()); got < w*h/2 {
		t.Fatalf("the picture covers %d of %d pixels at full depth", got, w*h)
	}
	// As faint as the slider goes, the page's own colour shows instead.
	a.settings.BackdropOpacity = 0.05
	tt.Frame()
	if got := redPixels(tt.Image()); got > w*h/10 {
		t.Errorf("the picture covers %d of %d pixels at the faintest", got, w*h)
	}
}

// The home page's picture shows after a restart: the page asks for it while
// the window is being made, before there is a window to read it back into,
// and the read was dropped there and never asked for again.
func TestHomeBackdropLandsAfterTheWindowIsMade(t *testing.T) {
	path := filepath.Join(t.TempDir(), "red.png")
	writePicture(t, path, 8, 8, color.RGBA{R: 255, A: 255})
	a := newTestApp()
	a.settings = defaultSettings()
	a.settings.BackdropHome = []string{path}
	a.settings.BackdropOpacity = 1
	const w, h = 900, 600
	tt := ui.NewTester(a.view, w, h)
	// The window is still being made here: the page asks for the picture and
	// it is read off the main thread.
	tt.Frame()
	for waited := time.Duration(0); ; waited += 2 * time.Millisecond {
		a.postMu.Lock()
		ready := len(a.kept) > 0
		a.postMu.Unlock()
		if ready {
			break
		}
		if waited > 5*time.Second {
			t.Fatal("the picture was read and what it landed in was dropped")
		}
		time.Sleep(2 * time.Millisecond)
	}
	a.flushKept()
	tt.Frame()
	if got := redPixels(tt.Image()); got < w*h/2 {
		t.Fatalf("the picture covers %d of %d pixels after a restart", got, w*h)
	}
}

// Now Playing stands on the picture the listener chose for it, in place of
// the cover's own colours.
func TestPlayingBackdropCoversTheStage(t *testing.T) {
	path := filepath.Join(t.TempDir(), "red.png")
	writePicture(t, path, 8, 8, color.RGBA{R: 255, A: 255})
	a := newTestApp()
	a.settings = defaultSettings()
	a.settings.BackdropPlaying = []string{path}
	a.settings.BackdropOpacity = 1
	a.pics = map[string]*backdropPicture{path: decodePicture(path)}
	const w, h = 400, 300
	tt := ui.NewTester(func(c *ui.Context) {
		ui.Box(c).Fill().Background(ui.Hex("#0b0b0d"))
		a.backdrop(c, nil, ui.RGB(120, 120, 130))
	}, w, h)
	tt.Frame()
	if got := redPixels(tt.Image()); got < w*h/2 {
		t.Fatalf("the picture covers %d of %d pixels", got, w*h)
	}
}

// The settings sliders draw their track and knob: a bare SliderBase paints
// nothing of its own, and the row looked empty while it still dragged.
func TestSettingsSlidersPaintTheirTrack(t *testing.T) {
	isolateUserData(t)
	a := newTestApp()
	a.settings = defaultSettings()
	tt := ui.NewTester(a.settingsView, 780, 640)
	tt.Frame()
	for _, label := range []string{"调整背景透明度", "调整歌词字号"} {
		page := "外观"
		if label == "调整歌词字号" {
			page = "桌面歌词"
		}
		showSettingsPage(t, tt, page)
		img := tt.Image()
		r, ok := tt.Find(label)
		if !ok {
			t.Fatalf("%s: not on the page", label)
		}
		if n := shades(img, r); n < 3 {
			t.Errorf("%s: %d shades over %v, wanted a track and a knob", label, n, r)
		}
	}
}

// shades counts the colours img shows in the part r covers.
func shades(img *image.RGBA, r ui.Rect) int {
	seen := map[color.RGBA]struct{}{}
	b := img.Bounds()
	for y := max(int(r.Y), b.Min.Y); y < min(int(r.Y+r.H), b.Max.Y); y++ {
		for x := max(int(r.X), b.Min.X); x < min(int(r.X+r.W), b.Max.X); x++ {
			seen[img.RGBAAt(x, y)] = struct{}{}
		}
	}
	return len(seen)
}

// redPixels counts the pixels of img that are mostly red.
func redPixels(img *image.RGBA) int {
	n := 0
	for y := img.Bounds().Min.Y; y < img.Bounds().Max.Y; y++ {
		for x := img.Bounds().Min.X; x < img.Bounds().Max.X; x++ {
			if r, g, b, _ := img.At(x, y).RGBA(); r > 200<<8 && g < 40<<8 && b < 40<<8 {
				n++
			}
		}
	}
	return n
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

// The pictures no page stands on anymore go, and past the limit those the
// page drawn now does not show, so that choosing other pictures does not
// keep every one ever chosen decoded; one still being read stays, as its
// pixels land where the page looks for them.
func TestBackdropPicturesKeepOnlyThoseChosen(t *testing.T) {
	a := newTestApp()
	a.settings = defaultSettings()
	pic := func() *backdropPicture { return &backdropPicture{w: 2, h: 2} }
	a.pics = map[string]*backdropPicture{}
	a.settings.BackdropHome = []string{"chosen.png"}
	a.settings.BackdropPlaying = []string{"reading.png"}
	a.pics["chosen.png"] = pic()
	a.pics["reading.png"] = nil // on its way
	for i := 0; i < backdropLimit+4; i++ {
		a.pics[fmt.Sprintf("gone%d.png", i)] = pic()
	}
	a.pictures(a.settings.BackdropHome)
	if len(a.pics) != 2 || a.pics["chosen.png"] == nil || a.pics["reading.png"] != nil {
		t.Fatalf("kept %d pictures, want the two chosen", len(a.pics))
	}
	// Chosen by the dozen, the page drawn now keeps every picture it stands
	// on, and the other page's go past the limit: they are read again when
	// they show.
	a.pics = map[string]*backdropPicture{}
	var home []string
	for i := 0; i < backdropLimit+4; i++ {
		path := fmt.Sprintf("pick%d.png", i)
		home = append(home, path)
		a.pics[path] = pic()
	}
	a.pics["playing.png"] = pic()
	a.settings.BackdropHome, a.settings.BackdropPlaying = home, []string{"playing.png"}
	a.pictures(home)
	for _, path := range home {
		if a.pics[path] == nil {
			t.Fatalf("dropped %q, which the page drawn now stands on", path)
		}
	}
	if a.pics["playing.png"] != nil {
		t.Error("kept the other page's pictures over the limit")
	}
}
