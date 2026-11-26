/*
验证本地搜索的播放与定位，并在选择时重新检查文件和曲库状态。
*/

package app

import (
	"fmt"
	"image/png"
	"os"
	"path/filepath"
	"testing"

	"github.com/egoist/mygo/ui"

	"netease-music-mygo/internal/features/localcatalog"
	"netease-music-mygo/internal/features/netease"
	"netease-music-mygo/internal/features/player"
)

func localSearchFixture(t *testing.T) (*app, []localcatalog.Song, *ui.Tester) {
	t.Helper()
	before := searchSuggest
	searchSuggest = func(string) (netease.Suggestions, error) { return netease.Suggestions{}, netease.ErrOffline }
	a, songs, tt := localLocateFixture(t, false)
	t.Cleanup(func() { a.searches.DismissSuggestions(); searchSuggest = before })
	// Real local files need not have a catalogue ID; keep every one at zero.
	for i := range songs {
		songs[i].Track.ID = 0
		songs[i].Track.Artists = []netease.Artist{{Name: "MyGO!!!!!"}}
		songs[i].Track.Album.Name = "迷跡波"
	}
	songs[149].Track.Alias = []string{"珍藏现场"}
	a.local.Replace(localcatalog.Catalog{Songs: songs})
	player.Cue(a.pl, []netease.Track{songs[0].Track, {ID: 999, Name: "在线歌曲"}})
	tt.Frame()
	return a, songs, tt
}

// Mouse and keyboard choices from the shared sidebar play and locate the correct local file even when all catalogue IDs are zero.
func TestLocalSidebarSearchPlaysAndLocates(t *testing.T) {
	for _, keyboard := range []bool{false, true} {
		t.Run(fmt.Sprint(keyboard), func(t *testing.T) {
			a, songs, tt := localSearchFixture(t)
			typePlaylistSearch(t, tt, "MYGO 珍藏")
			if dir := os.Getenv("NM_FRAMES"); dir != "" && !keyboard {
				f, err := os.Create(filepath.Join(dir, "local-search.png"))
				if err != nil {
					t.Fatal(err)
				}
				err = png.Encode(f, tt.Image())
				f.Close()
				if err != nil {
					t.Fatal(err)
				}
			}
			if keyboard {
				tt.Key(0, ui.KeyDown)
				tt.Key(0, ui.KeyEnter)
			} else if err := tt.Click("本地：" + songs[149].Track.Name); err != nil {
				t.Fatal(err)
			}
			tt.Frame()
			if cur := a.pl.Current(); cur == nil || cur.LocalID != 150 || cur.ID != 0 || !a.pl.Playing() || len(a.pl.Queue()) != len(songs) || a.nav.Current.Page != pageLocal {
				t.Fatal("sidebar did not play the selected local file with the library queue")
			}
			for i, tr := range a.pl.Queue() {
				if tr.LocalID != int64(i+1) {
					t.Fatal("search changed library queue order")
				}
			}
			locatedRow(t, tt, songs[149].Track.Name, 800)
		})
	}
}

// Missing files invalidate cached suggestions, cannot be chosen from an old popup, and do not hide available matches.
func TestLocalSearchRevalidatesMissingFiles(t *testing.T) {
	a, _, _ := localSearchFixture(t)
	songs := make([]localcatalog.Song, 4)
	for i := range songs {
		songs[i] = localcatalog.Song{Track: netease.Track{LocalID: int64(i + 1), Name: "匹配歌曲"}, Path: "../features/player/testdata/seek.flac"}
	}
	a.local.Replace(localcatalog.Catalog{Songs: songs})
	player.Cue(a.pl, []netease.Track{{ID: 999, Name: "正在播放"}})
	old := a.trackSuggestions("匹配")[0].Key
	for _, id := range []int64{1, 2, 3} {
		a.local.MarkMissing(id)
	}
	got := a.trackSuggestions("匹配")
	if len(got) != 3 || got[0].Song != -4 || !got[0].Playable {
		t.Fatalf("available local match hidden: %+v", got)
	}
	a.chooseTrackSuggestion(old, "匹配")
	if a.pl.Current().ID != 999 || len(a.pl.Queue()) != 1 {
		t.Fatal("a stale missing-file choice changed playback")
	}
	a.local.SetMissing(map[int64]bool{})
	if got := a.trackSuggestions("匹配"); len(got) == 0 || got[0].Song != -1 || !got[0].Playable {
		t.Fatal("restored files stayed disabled in the cached query")
	}
}

// Refreshing, reordering or removing a local file resolves a popup choice by local identity instead of the previous index.
func TestLocalSearchRevalidatesCatalogChanges(t *testing.T) {
	for _, removed := range []bool{false, true} {
		t.Run(fmt.Sprint(removed), func(t *testing.T) {
			a, songs, _ := localSearchFixture(t)
			key := a.trackSuggestions("珍藏")[0].Key
			target := songs[149]
			reordered := append([]localcatalog.Song{target}, append(append([]localcatalog.Song(nil), songs[:149]...), songs[150:]...)...)
			if removed {
				reordered = reordered[1:]
			}
			a.local.Replace(localcatalog.Catalog{Songs: reordered})
			a.chooseTrackSuggestion(key, "珍藏")
			if removed {
				if a.pl.Current().LocalID != 1 || len(a.pl.Queue()) != 2 {
					t.Fatal("removed local file was played")
				}
			} else if a.pl.Current().LocalID != 150 || a.pl.Queue()[0].LocalID != 150 || len(a.pl.Queue()) != 250 {
				t.Fatal("reordered source played a different local file")
			}
		})
	}
}
