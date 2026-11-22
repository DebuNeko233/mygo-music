package app

import (
	"testing"

	"netease-music-mygo/internal/diskcache"
	"netease-music-mygo/internal/netease"
)

// A close recording is eligible, while conflicting editions or different
// artists don't silently supply the cover.
func TestLocalArtworkMatchesRecording(t *testing.T) {
	song := netease.Track{Name: "Ｓｏｎｇ", Duration: 180000, Artists: []netease.Artist{{Name: "Band"}}}
	studio := netease.Track{Name: "Song", Duration: 180600, Artists: song.Artists, Album: netease.Album{Name: "Studio", PicURL: "studio"}}
	live := studio
	live.Duration, live.Album = 224000, netease.Album{Name: "Live", PicURL: "live"}
	cover := studio
	cover.Artists, cover.Album.PicURL = []netease.Artist{{Name: "Other"}}, "cover"
	if got := chooseLocalArtwork(&song, []netease.Track{live, cover, studio}); got != "studio" {
		t.Fatalf("matched %q, want the studio version", got)
	}
	live.Duration = 180100
	if got := chooseLocalArtwork(&song, []netease.Track{live, studio}); got != "" {
		t.Fatalf("picked %q for two versions with nearly identical times", got)
	}
	song.Album.Name = "Studio"
	if got := chooseLocalArtwork(&song, []netease.Track{live, studio}); got != "studio" {
		t.Fatalf("matched %q with an exact album tag", got)
	}
}

// A missing cover waits for connectivity and reuses its cached match on
// restart. An embedded cover bypasses matching.
func TestLocalArtworkSearchAndOfflineReuse(t *testing.T) {
	const url = "https://p1.music.126.net/test/1.jpg"
	store := diskcache.New(t.TempDir(), 1<<20)
	t.Cleanup(store.Close)
	song := localSong{Track: netease.Track{LocalID: 4, Name: "Band - Song", Duration: 180000}, Path: "Band - Song.mp3"}
	queries, images := make(chan string, 4), make(chan string, 4)
	oldSearch, oldFetch := searchLocalArtwork, fetchLocalArtwork
	searchLocalArtwork = func(q string, _ int) ([]netease.Track, error) {
		queries <- q
		return []netease.Track{{
			Name: "Song", Duration: 180600, Artists: []netease.Artist{{Name: "Band"}}, Album: netease.Album{PicURL: url},
		}}, nil
	}
	fetchLocalArtwork = func(url string) ([]byte, error) { images <- url; return nil, nil }
	t.Cleanup(func() { searchLocalArtwork, fetchLocalArtwork = oldSearch, oldFetch })
	makeApp := func(song localSong) (*app, chan func()) {
		a := newTestApp()
		a.browseCache, a.network.offline = store, true
		a.local.replace(localCatalog{Songs: []localSong{song}})
		a.pl.Cue([]netease.Track{song.Track})
		return a, testPosts(a)
	}
	a, posts := makeApp(song)
	a.syncLocalArtwork()
	wait(t, posts, "offline cache miss")()
	if a.hasTrackCover(a.pl.Current()) {
		t.Fatal("showed a cover without an embedded or cached image")
	}
	a.network.offline = false
	a.syncLocalArtwork()
	a.syncLocalArtwork()
	if q := wait(t, queries, "album artwork search"); q != "Song Band" {
		t.Fatalf("searched for %q", q)
	}
	wait(t, posts, "matched artwork")()
	if got := wait(t, images, "system artwork request"); got != netease.Sized(url, nowPlayingCoverPixels) {
		t.Fatalf("requested artwork URL %q", got)
	}
	if !a.hasTrackCover(a.pl.Current()) {
		t.Fatal("the match was not available to the player artwork")
	}

	reopened, posts := makeApp(song)
	reopened.syncLocalArtwork()
	wait(t, posts, "cached artwork")()
	wait(t, images, "cached system artwork request")
	if got := reopened.localArtworkURL(reopened.pl.Current(), song); got != url {
		t.Fatalf("reopened with cover %q", got)
	}

	song.Cover = true
	embedded, _ := makeApp(song)
	embedded.network.offline = false
	embedded.syncLocalArtwork()
	if embedded.local.artwork.searching || embedded.localArtworkURL(embedded.pl.Current(), song) != "" {
		t.Fatal("searched for a song that already had an embedded cover")
	}
	select {
	case q := <-queries:
		t.Fatalf("unexpected search %q", q)
	default:
	}
}

// Cloud tracks with the service's generic image reuse local matching and caching, while real custom covers bypass lookup.
func TestCloudArtworkUsesSharedResolver(t *testing.T) {
	const placeholder = "https://p1.music.126.net/example/109951171180358170.jpg"
	const matched = "https://p1.music.126.net/test/recording.jpg"
	oldSearch, oldFetch := searchLocalArtwork, fetchLocalArtwork
	queries, images := make(chan string, 4), make(chan string, 4)
	searchLocalArtwork = func(q string, _ int) ([]netease.Track, error) {
		queries <- q
		return []netease.Track{{Name: "Song", Duration: 180600, Artists: []netease.Artist{{Name: "Band"}}, Album: netease.Album{PicURL: matched}}}, nil
	}
	fetchLocalArtwork = func(url string) ([]byte, error) { images <- url; return nil, nil }
	t.Cleanup(func() { searchLocalArtwork, fetchLocalArtwork = oldSearch, oldFetch })
	store := diskcache.New(t.TempDir(), 1<<20)
	t.Cleanup(store.Close)
	tr := netease.Track{ID: 700, Cloud: true, Name: "Song", Duration: 180000, Artists: []netease.Artist{{Name: "Band"}}, Album: netease.Album{PicURL: placeholder}}
	makeApp := func() (*app, chan func()) {
		a := newTestApp()
		a.profile = &netease.Profile{UserID: 1}
		a.browseCache = store
		a.pl.Cue([]netease.Track{tr})
		return a, testPosts(a)
	}
	a, posts := makeApp()
	if a.hasTrackCover(a.pl.Current()) {
		t.Fatal("service placeholder counted as a real cover")
	}
	a.syncLocalArtwork()
	a.syncLocalArtwork()
	if got := wait(t, queries, "shared cover search"); got != "Song Band" {
		t.Fatalf("query %q", got)
	}
	refreshPost(t, posts)
	wait(t, images, "resolved system cover")
	if a.trackArtworkURL(a.pl.Current()) != matched || !a.hasTrackCover(a.pl.Current()) {
		t.Fatal("matched cover was not shared by playback views")
	}
	reopened, posts := makeApp()
	reopened.network.offline = true
	reopened.syncLocalArtwork()
	refreshPost(t, posts)
	wait(t, images, "cached system cover")
	if reopened.trackArtworkURL(reopened.pl.Current()) != matched {
		t.Fatal("cached cover was not reused offline")
	}
	reopened.profile = &netease.Profile{UserID: 2}
	if reopened.hasTrackCover(reopened.pl.Current()) {
		t.Fatal("matched private metadata leaked to a different account")
	}
	tr.Album.PicURL = "https://p1.music.126.net/custom/upload.jpg"
	custom, _ := makeApp()
	custom.syncLocalArtwork()
	if custom.trackArtworkURL(custom.pl.Current()) != tr.Album.PicURL {
		t.Fatal("custom upload cover was replaced")
	}
	select {
	case <-queries:
		t.Fatal("real cover triggered another match")
	default:
	}
}
