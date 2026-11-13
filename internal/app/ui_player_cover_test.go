package app

import (
	"errors"
	"image"
	"image/color"
	"image/draw"
	"testing"
	"testing/synctest"

	"github.com/egoist/mygo/ui"

	"netease-music-mygo/internal/netease"
	"netease-music-mygo/internal/player"
)

// When switching rapidly, a delayed cover must not clear the bar or replace
// the latest selection. Each visible transition releases its predecessor.
func TestPlayerBarCoverFollowsLatestTrack(t *testing.T) {
	a := newTestApp()
	a.network.offline = true
	a.settings = defaultSettings()
	a.pl.SuspendCache()
	posts := testPosts(a)
	a.art.post = a.post
	q := []netease.Track{
		{ID: 1, Album: netease.Album{PicURL: "test://first"}},
		{ID: 2, Album: netease.Album{PicURL: "test://second"}},
		{ID: 3, Album: netease.Album{PicURL: "test://third"}},
		{ID: 4, Album: netease.Album{PicURL: "test://fourth"}},
	}
	url := func(i int) string { return netease.Sized(q[i].Album.PicURL, barCoverPixels) }
	var arts [4]*artwork
	for i := range q {
		if i != 1 {
			arts[i] = &artwork{bmp: ui.NewBitmap(image.NewRGBA(image.Rect(0, 0, 2, 2)))}
			a.art.add(url(i), arts[i])
		}
	}
	oldLoad := loadArt
	defer func() { loadArt = oldLoad }()
	loadArt = func(string) (*ui.Bitmap, image.Image, int, error) {
		img := image.NewRGBA(image.Rect(0, 0, 2, 2))
		return ui.NewBitmap(img), img, 0, nil
	}
	a.pl.Cue(q)
	tt := ui.NewTester(a.playerBar, 1000, 96)
	tt.Advance(2 * barCoverFade)
	if a.barCover.current != arts[0] {
		t.Fatal("bar did not show its initial cover")
	}
	if err := tt.Click("下一首"); err != nil {
		t.Fatal(err)
	}
	if a.pl.Current().ID != q[1].ID {
		t.Fatal("Next did not select the track whose cover is pending")
	}
	tt.Frame()
	if a.barCover.current != arts[0] || a.barCover.previous != nil {
		t.Fatal("waiting for a cover changed what was shown")
	}
	a.pl.Jump(2)
	refreshPost(t, posts)
	if bitmap(a.art.art[url(1)]) == nil {
		t.Fatal("the delayed cover did not reach the cache")
	}
	tt.Frame()
	if a.barCover.current != arts[2] || a.barCover.previous != arts[0] {
		t.Fatal("bar did not fade from the held cover to the latest selection")
	}

	a.pl.Jump(3)
	tt.Advance(barCoverFade / 2)
	if a.barCover.current != arts[2] || a.barCover.previous != arts[0] {
		t.Fatal("a new selection interrupted the visible blend")
	}
	tt.Advance(barCoverFade)
	if a.barCover.current != arts[3] || a.barCover.previous != arts[2] {
		t.Fatal("the fade did not pick up the latest selection")
	}
	tt.Advance(barCoverFade)
	if a.barCover.previous != nil || !a.barCover.changedAt.IsZero() {
		t.Fatal("finished fade kept its predecessor")
	}

	tt.SetPreferences(ui.Preferences{ReduceMotion: true})
	a.pl.Jump(0)
	tt.Frame()
	if a.barCover.current != arts[0] || a.barCover.previous != nil {
		t.Fatal("reduced motion should show the ready cover immediately")
	}
}

// A ready cover used to replace the old one immediately, with no frame
// showing both pictures during the change.
func TestPlayerBarCoverBlendKeepsBothPicturesVisible(t *testing.T) {
	a := newTestApp()
	a.settings = defaultSettings()
	a.network.offline = true
	a.pl.SuspendCache()
	q := []netease.Track{
		{ID: 1, Name: "First", Album: netease.Album{PicURL: "test://red"}},
		{ID: 2, Name: "Second", Album: netease.Album{PicURL: "test://blue"}},
	}
	for i, c := range []color.RGBA{{R: 255, A: 255}, {B: 255, A: 255}} {
		img := image.NewRGBA(image.Rect(0, 0, 48, 48))
		draw.Draw(img, img.Bounds(), image.NewUniform(c), image.Point{}, draw.Src)
		a.art.add(netease.Sized(q[i].Album.PicURL, barCoverPixels), &artwork{bmp: ui.NewBitmap(img)})
	}
	a.pl.Cue(q)
	tt := ui.NewTester(a.playerBar, 1000, 96)
	tt.Advance(barCoverFade)
	r, ok := tt.Find("打开正在播放")
	if !ok {
		t.Fatal("playback info button is missing")
	}
	center := func() color.RGBA {
		img := tt.Image()
		scale := float32(img.Bounds().Dx()) / 1000
		return img.RGBAAt(int((r.X+24)*scale), int((r.Y+r.H/2)*scale))
	}
	if got := center(); got.R < 240 || got.B > 15 {
		t.Fatalf("old image did not cover the center: %v", got)
	}
	a.pl.Next()
	tt.Frame()
	tt.Advance(barCoverFade / 2)
	if got := center(); got.R < 15 || got.B < 15 || got.G > 15 {
		t.Fatalf("both images should cover the center during the blend: %v", got)
	}
	tt.Advance(barCoverFade)
	if got := center(); got.B < 240 || got.R > 15 {
		t.Fatalf("new image did not cover the center: %v", got)
	}
}

// A permanently failed image must eventually show the placeholder rather
// than leave the previous track's cover on the bar indefinitely.
func TestPlayerBarCoverFallsBackAfterFailure(t *testing.T) {
	a := newTestApp()
	a.settings = defaultSettings()
	a.network.offline = true
	a.pl.SuspendCache()
	q := []netease.Track{
		{ID: 1, Album: netease.Album{PicURL: "test://first"}},
		{ID: 2, Album: netease.Album{PicURL: "test://unavailable"}},
	}
	oldArt := &artwork{bmp: ui.NewBitmap(image.NewRGBA(image.Rect(0, 0, 2, 2)))}
	a.art.add(netease.Sized(q[0].Album.PicURL, barCoverPixels), oldArt)
	posts := testPosts(a)
	a.art.post = a.post
	oldLoad := loadArt
	defer func() { loadArt = oldLoad }()
	loadArt = func(string) (*ui.Bitmap, image.Image, int, error) {
		return nil, nil, 0, errors.New("invalid cover")
	}
	a.pl.Cue(q)
	tt := ui.NewTester(a.playerBar, 1000, 96)
	tt.Advance(2 * barCoverFade)
	a.pl.Next()
	tt.Frame()
	if a.barCover.current != oldArt {
		t.Fatal("old cover disappeared before the load finished")
	}
	refreshPost(t, posts)
	tt.Frame()
	if a.barCover.previous != oldArt || a.barCover.current != nil {
		t.Fatal("the bar did not transition from the old cover to the placeholder")
	}
	tt.Advance(2 * barCoverFade)
	if a.barCover.current != nil || a.barCover.previous != nil {
		t.Fatal("the bar held the previous track's cover after the failure")
	}
}

// Prefetches must respect the play order, radio boundaries and duplicate
// artwork URLs, and must not start speculative requests offline.
func TestPlayerBarPrefetchesAdjacentCovers(t *testing.T) {
	const root = "test://"
	for _, tc := range []struct {
		name           string
		covers         []string
		order          []int
		at             int
		radio, offline bool
		want           []string
	}{
		{name: "shuffled wrap", covers: []string{"one", "two", "three", "four"}, order: []int{2, 3, 0, 1}, at: 3, want: []string{"one", "three"}},
		{name: "radio end", covers: []string{"one", "two", "three", "four"}, order: []int{2, 3, 0, 1}, at: 3, radio: true, want: []string{"one"}},
		{name: "shared neighbor cover", covers: []string{"one", "two", "two"}, order: []int{0, 1, 2}, want: []string{"two"}},
		{name: "same as current", covers: []string{"one", "one"}, order: []int{0, 1}},
		{name: "missing neighbor cover", covers: []string{"one", ""}, order: []int{0, 1}},
		{name: "offline", covers: []string{"one", "two"}, order: []int{0, 1}, offline: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			synctest.Test(t, func(t *testing.T) {
				a := newTestApp()
				a.network.offline = tc.offline
				a.pl.SuspendCache()
				q := make([]netease.Track, len(tc.covers))
				for i, cover := range tc.covers {
					q[i].ID = int64(i + 1)
					if cover != "" {
						q[i].Album.PicURL = root + cover
					}
				}
				a.pl.Restore(player.Session{Queue: q, Order: tc.order, At: tc.at, Shuffle: true})
				if tc.radio {
					a.pl.AttachRadio("test", func(func([]netease.Track)) {})
				}
				asked, posts := make(chan string, artJobs), make(chan func(), artJobs)
				a.art = newArtCache(func(fn func()) { posts <- fn })
				defer a.art.close()
				old := loadArt
				defer func() { loadArt = old }()
				loadArt = func(url string) (*ui.Bitmap, image.Image, int, error) {
					asked <- url
					return nil, nil, 0, errors.New("fixture")
				}
				current := a.pl.Current().Album.PicURL
				a.art.add(netease.Sized(current, barCoverPixels), &artwork{})
				a.playbackArtwork(current, barCoverRequest)
				a.playbackArtwork(current, barCoverRequest)
				synctest.Wait()
				if len(asked) != len(tc.want) {
					t.Fatalf("prefetched %d covers, want %v", len(asked), tc.want)
				}
				want := make(map[string]bool, len(tc.want))
				for _, cover := range tc.want {
					want[netease.Sized(root+cover, barCoverPixels)] = true
				}
				for range len(tc.want) {
					if url := <-asked; !want[url] {
						t.Fatalf("unexpected prefetch %s", url)
					} else {
						delete(want, url)
					}
					(<-posts)()
				}
			})
		})
	}
}
