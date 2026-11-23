package app

import (
	"fmt"
	"image/png"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/egoist/mygo/ui"

	"netease-music-mygo/internal/localcatalog"
	"netease-music-mygo/internal/netease"
	"netease-music-mygo/internal/viewmodel"
)

func localPerfApp(count int) *app {
	a := newTestApp()
	a.settings = defaultSettings()
	a.nav.Current = route{Page: pageLocal}
	songs := make([]localcatalog.Song, count)
	for i := range songs {
		songs[i] = localcatalog.Song{Track: netease.Track{LocalID: int64(i + 1), Name: fmt.Sprintf("本地歌曲 %05d", i), Artists: []netease.Artist{{Name: "MyGO!!!!!"}}, Duration: 180000}, Path: "/music/song.flac"}
	}
	a.local.Replace(localcatalog.Catalog{Songs: songs, Folders: []string{"/music"}})
	a.local.Path = "/test/local-music.json"
	return a
}

// A large local library draws visible rows without copying its catalog each frame.
func BenchmarkLocalViewFrame(b *testing.B) {
	for _, count := range []int{1000, 10000} {
		b.Run(fmt.Sprint(count), func(b *testing.B) {
			a := localPerfApp(count)
			tt := ui.NewTester(func(c *ui.Context) { a.pal = paletteFor(c.Theme()); a.localPage(c) }, 1000, 700)
			tt.Frame()
			i := 0
			b.ReportAllocs()
			for b.Loop() {
				a.trackList.ScrollTo((i*17)%count+1, ui.Center)
				tt.Frame()
				i++
			}
		})
	}
}

// An unchanged local search reuses results, while a changed catalog triggers one bounded scan.
func BenchmarkLocalSuggestions(b *testing.B) {
	for _, cold := range []bool{false, true} {
		b.Run(fmt.Sprintf("cold=%v", cold), func(b *testing.B) {
			a := localPerfApp(20000)
			a.trackSuggestions("19999")
			b.ReportAllocs()
			b.ResetTimer()
			for b.Loop() {
				if cold {
					a.local.Revision++
				}
				a.trackSuggestions("19999")
			}
		})
	}
}

// A paused local list must stay asleep without a polling loop.
func TestLocalViewFrameBudget(t *testing.T) {
	a := localPerfApp(1000)
	builds := 0
	tt := ui.NewTester(func(c *ui.Context) { builds++; a.pal = paletteFor(c.Theme()); a.localPage(c) }, 1000, 700)
	tt.Frame()
	tt.Advance(time.Second)
	before := builds
	tt.Advance(2 * time.Second)
	if builds != before {
		t.Fatalf("idle local list requested %d extra builds", builds-before)
	}
}

// Local list and folder-manager layouts can be compared visually across the refactor.
func TestLocalLibraryFrames(t *testing.T) {
	dir := os.Getenv("NM_FRAMES")
	if dir == "" {
		t.Skip("set NM_FRAMES to save local-library views")
	}
	for _, dark := range []bool{false, true} {
		for _, folders := range []bool{false, true} {
			a := localPerfApp(1000)
			if dark {
				a.settings.Appearance = "dark"
			} else {
				a.settings.Appearance = "light"
			}
			tt := ui.NewTester(func(c *ui.Context) {
				theme := a.settings.uiTheme(c.Theme())
				c.SetTheme(&theme)
				a.pal = paletteFor(&theme)
				if folders {
					a.localFoldersView(c)
				} else {
					a.localPage(c)
				}
			}, 1000, 700)
			tt.SetDark(dark)
			tt.Frame()
			path := filepath.Join(dir, fmt.Sprintf("local-dark=%v-folders=%v.png", dark, folders))
			f, err := os.Create(path)
			if err != nil {
				t.Fatal(err)
			}
			err = png.Encode(f, tt.Image())
			f.Close()
			if err != nil {
				t.Fatal(err)
			}
		}
	}
}

// The matching dialog keeps its layout while form data and scroll state get separate owners.
func TestLocalLyricMatchFrames(t *testing.T) {
	dir := os.Getenv("NM_FRAMES")
	if dir == "" {
		t.Skip("set NM_FRAMES to save the lyric matching dialog")
	}
	for _, dark := range []bool{false, true} {
		for _, searching := range []bool{false, true} {
			a := newTestApp()
			track := netease.Track{LocalID: 7, Name: "試唱 recording", Artists: []netease.Artist{{Name: "MyGO!!!!!"}}, Duration: 180000}
			a.pl.Cue([]netease.Track{track})
			a.local.LyricMatch = &viewmodel.LocalLyricMatch{Open: true, Track: a.pl.Current(), Query: "試唱 MyGO!!!!!", Searching: searching}
			if !searching {
				a.local.LyricMatch.Results = []netease.Track{{ID: 1, Name: "录音室版", Duration: 180000}, {ID: 2, Name: "现场版", Duration: 190000}, {ID: 3, Name: "instrumental", Duration: 180000}}
				a.local.LyricMatch.Selected = 1
			}
			a.settings = defaultSettings()
			if dark {
				a.settings.Appearance = "dark"
			} else {
				a.settings.Appearance = "light"
			}
			tt := ui.NewTester(func(c *ui.Context) {
				theme := a.settings.uiTheme(c.Theme())
				c.SetTheme(&theme)
				a.pal = paletteFor(&theme)
				a.localLyricMatchDialog(c)
			}, 780, 600)
			tt.SetDark(dark)
			tt.Frame()
			f, err := os.Create(filepath.Join(dir, fmt.Sprintf("local-match-dark=%v-searching=%v.png", dark, searching)))
			if err != nil {
				t.Fatal(err)
			}
			err = png.Encode(f, tt.Image())
			f.Close()
			if err != nil {
				t.Fatal(err)
			}
		}
	}
}
