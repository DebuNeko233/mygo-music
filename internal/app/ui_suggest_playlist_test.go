package app

import (
	"errors"
	"fmt"
	"image/png"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/egoist/mygo/ui"

	"netease-music-mygo/internal/netease"
)

func playlistSearchFixture(t *testing.T) (*app, *netease.Playlist, *ui.Tester) {
	t.Helper()
	before := searchSuggest
	searchSuggest = func(string) (netease.Suggestions, error) {
		return netease.Suggestions{}, errors.New("offline")
	}
	a, pl, tt := locateFixture(t, 0, false)
	t.Cleanup(func() {
		a.dismissSuggestions()
		searchSuggest = before
	})
	return a, pl, tt
}

func typePlaylistSearch(t *testing.T, tt *ui.Tester, q string) {
	t.Helper()
	if err := tt.Click("搜索"); err != nil {
		t.Fatal(err)
	}
	tt.Type(q)
	tt.Frame()
}

// Clearing text or a pasted link cancels its suggestions, keeps history and
// playback, and returns focus to the input so the next query can play a match.
func TestSearchClearKeepsFocus(t *testing.T) {
	for _, q := range []string{"250", "   ", "orpheus://album/34"} {
		for _, keyboard := range []bool{false, true} {
			t.Run(fmt.Sprintf("%q/keyboard=%v", q, keyboard), func(t *testing.T) {
				a, _, tt := playlistSearchFixture(t)
				a.settings.SearchHistory = []string{"之前的搜索"}
				typePlaylistSearch(t, tt, q)
				if keyboard {
					tt.Key(0, ui.KeyTab)
					if !tt.Focused("清空搜索") {
						t.Fatal("clear button cannot receive keyboard focus")
					}
					tt.Key(0, ui.KeyEnter)
				} else if err := tt.Click("清空搜索"); err != nil {
					t.Fatal(err)
				}
				tt.Frame()
				if a.query != "" || !tt.Focused("搜索") || a.suggestions.query != "" {
					t.Fatalf("cleared query %q, focused %v, suggestions %q", a.query, tt.Focused("搜索"), a.suggestions.query)
				}
				if _, shown := tt.Find("清空搜索"); shown {
					t.Fatal("clear button remained after clearing")
				}
				if a.route != (route{page: pagePlaylist, id: 7}) || a.pl.Current().ID != 1 || a.pl.Playing() || !slices.Equal(a.settings.SearchHistory, []string{"之前的搜索"}) {
					t.Fatal("clearing changed navigation, playback or saved history")
				}
				tt.Type("251")
				tt.Key(0, ui.KeyDown)
				tt.Key(0, ui.KeyEnter)
				tt.Frame()
				if a.pl.Current().ID != 251 || !a.pl.Playing() {
					t.Fatal("next query lost focus or reused the cleared suggestion")
				}
			})
		}
	}
}

// Local matches work offline before remote suggestions arrive, and pointer or
// keyboard selection plays and reveals the song in the displayed playlist.
func TestPlaylistSuggestionPlaysAndLocates(t *testing.T) {
	for _, keyboard := range []bool{false, true} {
		t.Run(fmt.Sprint(keyboard), func(t *testing.T) {
			a, pl, tt := playlistSearchFixture(t)
			if keyboard {
				a.pl.ToggleShuffle()
			}
			target := pl.Tracks[249]
			typePlaylistSearch(t, tt, "250")
			// Online suggestions may arrive later, but cannot take first place.
			a.suggestions.items = suggestionList(netease.Suggestions{Songs: []netease.Track{{ID: 900, Name: "250 elsewhere"}}})
			tt.Frame()
			if keyboard {
				tt.Key(0, ui.KeyDown)
				tt.Key(0, ui.KeyEnter)
			} else if err := tt.Click("当前歌单：" + target.Name); err != nil {
				t.Fatal(err)
			}
			tt.Frame()
			if a.pl.Current().ID != target.ID || !a.pl.Playing() || !slices.EqualFunc(a.pl.Queue(), pl.Tracks, func(a, b netease.Track) bool { return a.ID == b.ID }) {
				t.Fatal("selection did not play the chosen song with the whole playlist as queue")
			}
			if a.route != (route{page: pagePlaylist, id: 7}) || a.nowPlaying {
				t.Fatal("selection left the displayed playlist")
			}
			locatedRow(t, tt, target.Name, 760)
		})
	}
}

// Keyboard choice used to clear the suggestions before reading the chosen
// option. Online results below the local rows must remain actionable too.
func TestPlaylistSuggestionRemoteKeyboard(t *testing.T) {
	a, _, tt := playlistSearchFixture(t)
	a.playlists[8] = &netease.Playlist{ID: 8, Name: "另一张歌单"}
	typePlaylistSearch(t, tt, "250")
	a.suggestions.items = suggestionList(netease.Suggestions{Playlists: []netease.Playlist{{ID: 8, Name: "另一张歌单"}}})
	tt.Frame()
	tt.Key(0, ui.KeyDown) // Current-playlist match.
	tt.Key(0, ui.KeyDown) // Remote playlist.
	tt.Key(0, ui.KeyEnter)
	tt.Frame()
	if a.route != (route{page: pagePlaylist, id: 8}) || a.pl.Current().ID != 1 || a.pl.Playing() {
		t.Fatalf("remote choice opened %+v and plays %+v", a.route, a.pl.Current())
	}
}

// At the minimum window size, arrows must reveal online results below the
// local matches so they remain reachable by pointer as well as by keyboard.
func TestPlaylistSuggestionSmallWindow(t *testing.T) {
	a, _, tt := playlistSearchFixture(t)
	tt.SetSize(880, 580)
	typePlaylistSearch(t, tt, "测试")
	var remote []netease.Playlist
	for i := range 10 {
		pl := netease.Playlist{ID: int64(20 + i), Name: fmt.Sprintf("远程歌单 %d", i)}
		pl.Creator.Nickname = "创建者"
		a.playlists[pl.ID] = &pl
		remote = append(remote, pl)
	}
	a.suggestions.items = suggestionList(netease.Suggestions{Playlists: remote})
	tt.Frame()
	for range playlistSuggestLimit + len(remote) {
		tt.Key(0, ui.KeyDown)
	}
	tt.Frame()
	label := "歌单：远程歌单 9"
	row, ok := tt.AccessByRole(ui.RoleListItem, label)
	if !ok || row.Offscreen || row.Bounds.Y < 0 || row.Bounds.Y+row.Bounds.H > 580 {
		t.Fatalf("last online result is not visible: %+v, found %v", row, ok)
	}
	if err := tt.Click(label); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	if a.route != (route{page: pagePlaylist, id: 29}) || a.pl.Current().ID != 1 {
		t.Fatal("online choice failed or changed playback")
	}
}

// Enter searches globally whether or not the displayed playlist matches,
// preserving the query action as the first option without changing playback.
func TestPlaylistSuggestionKeepsGlobalSearch(t *testing.T) {
	for _, q := range []string{"missing", "250"} {
		t.Run(q, func(t *testing.T) {
			asked := fakeSearch(t)
			a, _, tt := playlistSearchFixture(t)
			typePlaylistSearch(t, tt, q)
			tt.Key(0, ui.KeyEnter)
			tt.Frame()
			if got := wait(t, asked, "global search"); got.query != q || a.route.page != pageSearch {
				t.Fatalf("searched %+v on %+v", got, a.route)
			}
			if a.pl.Current().ID != 1 || a.pl.Playing() {
				t.Fatal("global search changed playback")
			}
		})
	}
}

// Matching ignores case, spans title/artist/album/aliases, and keeps playlist
// order while bounding the number of rows ahead of global suggestions.
func TestPlaylistSuggestionMatches(t *testing.T) {
	a := newTestApp()
	track := netease.Track{ID: 1, Name: "春日影", Artists: []netease.Artist{{Name: "MyGO!!!!!"}}, Album: netease.Album{Name: "迷跡波"}, Alias: []string{"Haruhikage"}}
	pl := &netease.Playlist{ID: 7, Tracks: []netease.Track{track}}
	a.playlists[7], a.route = pl, route{page: pagePlaylist, id: 7}
	for _, tc := range []struct {
		query string
		want  bool
	}{
		{"春日", true}, {"mygo", true}, {"迷跡", true}, {"HARUHIKAGE", true},
		{"mygo  春日\t迷跡波", true}, {"MyGO missing", false}, {"other", false}, {"", false},
	} {
		if got := len(a.playlistSuggestions(tc.query)) > 0; got != tc.want {
			t.Errorf("%q matched %v, want %v", tc.query, got, tc.want)
		}
	}
	for i := 2; i <= 10; i++ {
		tr := track
		tr.ID = int64(i)
		pl.Tracks = append(pl.Tracks, tr)
	}
	got := a.playlistSuggestions("春日")
	if len(got) != playlistSuggestLimit {
		t.Fatalf("got %d local suggestions", len(got))
	}
	for i, sg := range got {
		if sg.song != int64(i+1) {
			t.Fatal("local suggestions changed playlist order")
		}
	}
}

// Reused results must update when the query, playlist data or viewed page
// changes, including replacing tracks on the same playlist object after edits.
func TestPlaylistSuggestionRefreshes(t *testing.T) {
	a := newTestApp()
	a.route = route{page: pagePlaylist, id: 7}
	a.playlists[7] = &netease.Playlist{ID: 7, Tracks: []netease.Track{{ID: 1, Name: "old"}}}
	a.playlistSuggestions("old")
	a.playlists[7] = &netease.Playlist{ID: 7, Tracks: []netease.Track{{ID: 2, Name: "new"}}}
	if len(a.playlistSuggestions("old")) != 0 {
		t.Fatal("refresh kept a removed match")
	}
	if got := a.playlistSuggestions("new"); len(got) != 1 || got[0].song != 2 {
		t.Fatal("new query missed refreshed tracks")
	}
	a.playlists[7].Tracks = []netease.Track{{ID: 3, Name: "new"}}
	if got := a.playlistSuggestions("new"); len(got) != 1 || got[0].song != 3 {
		t.Fatal("track replacement reused old results")
	}
	a.playlists[7].Tracks[0].Name = "renamed"
	a.listRefresh.revision++
	if len(a.playlistSuggestions("new")) != 0 {
		t.Fatal("edit kept stale results")
	}
	a.playlists[8] = &netease.Playlist{ID: 8, Tracks: []netease.Track{{ID: 4, Name: "new"}}}
	a.route.id = 8
	if got := a.playlistSuggestions("new"); len(got) != 1 || got[0].song != 4 {
		t.Fatal("page change searched the previous playlist")
	}
	a.route.page = pageAlbum
	if len(a.playlistSuggestions("new")) != 0 {
		t.Fatal("album page searched a playlist with the same ID")
	}
}

// A stale option resolves the current song ID rather than its old index; a
// removed, newly unavailable or no longer displayed song must never play.
func TestPlaylistSuggestionRevalidatesSelection(t *testing.T) {
	for _, change := range []string{"reorder", "removed", "unavailable", "other playlist", "other page", "now playing", "new query"} {
		t.Run(change, func(t *testing.T) {
			isolateUserData(t)
			a := newTestApp()
			a.pl.SuspendCache()
			a.pl.Cue([]netease.Track{{ID: 99, Name: "before"}})
			a.route = route{page: pagePlaylist, id: 7}
			pl := &netease.Playlist{ID: 7, Tracks: []netease.Track{{ID: 1, Name: "match"}, {ID: 2, Name: "other"}}}
			a.playlists[7] = pl
			sg := a.playlistSuggestions("match")[0]
			q := "match"
			switch change {
			case "reorder":
				slices.Reverse(pl.Tracks)
			case "removed":
				pl.Tracks = pl.Tracks[1:]
			case "unavailable":
				pl.Tracks[0].Fee = 1
			case "other playlist":
				a.route.id = 8
			case "other page":
				a.route.page = pageAlbum
			case "now playing":
				a.nowPlaying = true
			case "new query":
				q = "other"
			}
			a.choose(sg.key, q)
			want := int64(99)
			if change == "reorder" {
				want = 1
			}
			if a.pl.Current().ID != want {
				t.Fatalf("%s played %d, want %d", change, a.pl.Current().ID, want)
			}
		})
	}
}

// An unavailable local match must not start playback through keyboard choice,
// just as clicking its disabled playlist row would not start it.
func TestPlaylistSuggestionUnavailable(t *testing.T) {
	a, pl, tt := playlistSearchFixture(t)
	pl.Tracks[249].Fee = 1
	typePlaylistSearch(t, tt, "250")
	tt.Key(0, ui.KeyDown)
	tt.Key(0, ui.KeyEnter)
	tt.Frame()
	if a.pl.Current().ID != 1 || a.pl.Playing() {
		t.Fatal("unavailable suggestion changed playback")
	}
}

// With NM_FRAMES set, inspect long titles, the local badge and remote rows in
// both themes, then the selected song revealed far down the virtual playlist.
func TestPlaylistSuggestionFrames(t *testing.T) {
	dir := os.Getenv("NM_FRAMES")
	if dir == "" {
		t.Skip()
	}
	for _, dark := range []bool{false, true} {
		a, pl, tt := playlistSearchFixture(t)
		pl.Name = "MyGO!!!!! 精选"
		for i, name := range []string{"春日影", "春日影（Live Version）", "春日影 · " + strings.Repeat("很长的歌曲标题", 8)} {
			pl.Tracks[249+i].Name = name
			pl.Tracks[249+i].Artists = []netease.Artist{{Name: "MyGO!!!!!"}}
		}
		tt.SetDark(dark)
		typePlaylistSearch(t, tt, "春日影")
		a.suggestions.items = suggestionList(netease.Suggestions{Songs: []netease.Track{{ID: 901, Name: "春日影", Artists: []netease.Artist{{Name: "CRYCHIC"}}}}, Albums: []netease.AlbumProfile{{Album: netease.Album{ID: 902, Name: "迷跡波"}}}})
		theme := "light"
		if dark {
			theme = "dark"
		}
		for _, stage := range []string{"suggestions", "located"} {
			if stage == "located" {
				if err := tt.Click("当前歌单：春日影"); err != nil {
					t.Fatal(err)
				}
			}
			tt.Move(100, 100)
			tt.Advance(time.Second)
			tt.Frame()
			f, err := os.Create(filepath.Join(dir, "playlist-search-"+theme+"-"+stage+".png"))
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

// Typing in a large playlist does one bounded scan per query/source change;
// rebuilding the same popup during animations should not scan or allocate.
func BenchmarkPlaylistSuggestions(b *testing.B) {
	a := newTestApp()
	a.route = route{page: pagePlaylist, id: 7}
	pl := &netease.Playlist{ID: 7, Tracks: make([]netease.Track, 20000)}
	for i := range pl.Tracks {
		pl.Tracks[i] = netease.Track{ID: int64(i + 1), Name: "歌曲", Artists: []netease.Artist{{Name: "MyGO!!!!!"}}, Album: netease.Album{Name: "迷跡波"}}
	}
	pl.Tracks[len(pl.Tracks)-1].Name = "春日影"
	a.playlists[7] = pl
	for _, cold := range []bool{false, true} {
		b.Run(fmt.Sprintf("cold=%v", cold), func(b *testing.B) {
			a.playlistSuggestions("春日影")
			b.ReportAllocs()
			b.ResetTimer()
			for b.Loop() {
				if cold {
					a.listRefresh.revision++
				}
				a.playlistSuggestions("春日影")
			}
		})
	}
}
