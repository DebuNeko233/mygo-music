/*
验证本地和网盘封面匹配、离线复用、失败恢复及旧歌曲结果隔离。
*/

package app

import (
	"bytes"
	"errors"
	"fmt"
	"image"
	"image/png"
	"io"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/egoist/mygo"

	diskcache "netease-music-mygo/internal/features/cache/disk"
	"netease-music-mygo/internal/features/localcatalog"
	"netease-music-mygo/internal/features/netease"
	"netease-music-mygo/internal/features/player"
)

// A missing cover waits for connectivity and reuses its cached match on
// restart. An embedded cover bypasses matching.
func TestLocalArtworkSearchAndOfflineReuse(t *testing.T) {
	const url = "https://p1.music.126.net/test/1.jpg"
	store := diskcache.New(t.TempDir(), 1<<20)
	t.Cleanup(store.Close)
	song := localcatalog.Song{Track: netease.Track{LocalID: 4, Name: "Band - Song", Duration: 180000}, Path: "Band - Song.mp3"}
	queries, images := make(chan string, 4), make(chan string, 4)
	oldSearch, oldFetch := searchTrackArtwork, fetchTrackArtwork
	searchTrackArtwork = func(q string, _ int) ([]netease.Track, error) {
		queries <- q
		return []netease.Track{{
			Name: "Song", Duration: 180600, Artists: []netease.Artist{{Name: "Band"}}, Album: netease.Album{PicURL: url},
		}}, nil
	}
	cover := artworkTestPNG(t)
	fetchTrackArtwork = func(url string) ([]byte, error) { images <- url; return cover, nil }
	t.Cleanup(func() { searchTrackArtwork, fetchTrackArtwork = oldSearch, oldFetch })
	makeApp := func(song localcatalog.Song) (*app, chan func()) {
		a := newTestApp()
		a.browseCache, a.network.offline = store, true
		a.local.Replace(localcatalog.Catalog{Songs: []localcatalog.Song{song}})
		player.Cue(a.pl, []netease.Track{song.Track})
		return a, testPosts(a)
	}
	a, posts := makeApp(song)
	a.syncTrackArtwork()
	wait(t, posts, "offline cache miss")()
	if a.hasTrackCover(a.pl.Current()) {
		t.Fatal("showed a cover without an embedded or cached image")
	}
	a.network.offline = false
	a.syncTrackArtwork()
	a.syncTrackArtwork()
	if q := wait(t, queries, "album artwork search"); q != "Song Band" {
		t.Fatalf("searched for %q", q)
	}
	wait(t, posts, "matched artwork")()
	if got := wait(t, images, "system artwork request"); got != netease.Sized(url, nowPlayingCoverPixels) {
		t.Fatalf("requested artwork URL %q", got)
	}
	refreshPost(t, posts)
	if !a.hasTrackCover(a.pl.Current()) {
		t.Fatal("the match was not available to the player artwork")
	}

	reopened, posts := makeApp(song)
	reopened.syncTrackArtwork()
	wait(t, posts, "cached artwork")()
	wait(t, images, "cached system artwork request")
	refreshPost(t, posts)
	if got := reopened.localArtworkURL(reopened.pl.Current(), song); got != url {
		t.Fatalf("reopened with cover %q", got)
	}

	song.Cover = true
	embedded, _ := makeApp(song)
	embedded.network.offline = false
	embedded.syncTrackArtwork()
	if embedded.trackArt.searching || embedded.localArtworkURL(embedded.pl.Current(), song) != "" {
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
	oldSearch, oldFetch := searchTrackArtwork, fetchTrackArtwork
	queries, images := make(chan string, 4), make(chan string, 4)
	searchTrackArtwork = func(q string, _ int) ([]netease.Track, error) {
		queries <- q
		return []netease.Track{{Name: "Song", Duration: 180600, Artists: []netease.Artist{{Name: "Band"}}, Album: netease.Album{PicURL: matched}}}, nil
	}
	cover := artworkTestPNG(t)
	fetchTrackArtwork = func(url string) ([]byte, error) { images <- url; return cover, nil }
	t.Cleanup(func() { searchTrackArtwork, fetchTrackArtwork = oldSearch, oldFetch })
	store := diskcache.New(t.TempDir(), 1<<20)
	t.Cleanup(store.Close)
	tr := netease.Track{ID: 700, Cloud: true, Name: "Song", Duration: 180000, Artists: []netease.Artist{{Name: "Band"}}, Album: netease.Album{PicURL: placeholder}}
	makeApp := func() (*app, chan func()) {
		a := newTestApp()
		a.account.Profile = &netease.Profile{UserID: 1}
		a.browseCache = store
		player.Cue(a.pl, []netease.Track{tr})
		return a, testPosts(a)
	}
	a, posts := makeApp()
	if a.hasTrackCover(a.pl.Current()) {
		t.Fatal("service placeholder counted as a real cover")
	}
	a.syncTrackArtwork()
	a.syncTrackArtwork()
	if got := wait(t, queries, "shared cover search"); got != "Song Band" {
		t.Fatalf("query %q", got)
	}
	refreshPost(t, posts)
	wait(t, images, "resolved system cover")
	refreshPost(t, posts)
	if a.trackArtworkURL(a.pl.Current()) != matched || !a.hasTrackCover(a.pl.Current()) {
		t.Fatal("matched cover was not shared by playback views")
	}
	reopened, posts := makeApp()
	reopened.network.offline = true
	reopened.syncTrackArtwork()
	refreshPost(t, posts)
	wait(t, images, "cached system cover")
	refreshPost(t, posts)
	if reopened.trackArtworkURL(reopened.pl.Current()) != matched {
		t.Fatal("cached cover was not reused offline")
	}
	reopened.account.Profile = &netease.Profile{UserID: 2}
	if reopened.hasTrackCover(reopened.pl.Current()) {
		t.Fatal("matched private metadata leaked to a different account")
	}
	tr.Album.PicURL = "https://p1.music.126.net/custom/upload.jpg"
	custom, _ := makeApp()
	custom.syncTrackArtwork()
	if custom.trackArtworkURL(custom.pl.Current()) != tr.Album.PicURL {
		t.Fatal("custom upload cover was replaced")
	}
	select {
	case <-queries:
		t.Fatal("real cover triggered another match")
	default:
	}
}

func artworkRecoveryFixture(t *testing.T, local bool) (*app, chan func()) {
	t.Helper()
	a := newTestApp()
	a.account.Profile = &netease.Profile{UserID: 1}
	a.network.known = true
	tr := netease.Track{ID: 700, Cloud: true, Name: "Song", Duration: 180000, Artists: []netease.Artist{{Name: "Band"}}}
	if local {
		tr.ID, tr.Cloud, tr.LocalID = 0, false, 4
		a.local.Replace(localcatalog.Catalog{Songs: []localcatalog.Song{{Track: tr, Path: "Song.mp3"}}})
	}
	player.Cue(a.pl, []netease.Track{tr})
	player.SetLyrics(a.pl, nil)
	return a, testPosts(a)
}

// Transient lookup failures used to become permanent misses; retries now use the existing backoff without restarting playback.
func TestTrackArtworkScheduledRecovery(t *testing.T) {
	for _, local := range []bool{false, true} {
		t.Run(fmt.Sprintf("local=%v", local), func(t *testing.T) {
			a, posts := artworkRecoveryFixture(t, local)
			search, fetch, after := searchTrackArtwork, fetchTrackArtwork, recoveryAfter
			var callbacks []func()
			var timers []*time.Timer
			t.Cleanup(func() {
				searchTrackArtwork, fetchTrackArtwork, recoveryAfter = search, fetch, after
				for _, timer := range timers {
					timer.Stop()
				}
			})
			recoveryAfter = func(_ time.Duration, fn func()) *time.Timer {
				callbacks = append(callbacks, fn)
				timer := time.NewTimer(time.Hour)
				timers = append(timers, timer)
				return timer
			}
			var calls atomic.Int32
			searchTrackArtwork = func(string, int) ([]netease.Track, error) {
				if calls.Add(1) <= 2 {
					return nil, io.ErrUnexpectedEOF
				}
				return []netease.Track{{Name: "Song", Duration: 180000, Artists: []netease.Artist{{Name: "Band"}}, Album: netease.Album{PicURL: "https://p1.music.126.net/test/cover.jpg"}}}, nil
			}
			images := make(chan struct{}, 1)
			cover := artworkTestPNG(t)
			fetchTrackArtwork = func(string) ([]byte, error) { images <- struct{}{}; return cover, nil }
			current := a.pl.Current()
			a.recovery.active = true
			a.syncTrackArtwork()
			refreshPost(t, posts)
			for attempt := 0; attempt < 2; attempt++ {
				a.syncTrackArtwork()
				if calls.Load() != int32(attempt+1) || a.trackArt.searching || !a.needsRecovery() {
					t.Fatal("failed lookup was forgotten or retried without backoff")
				}
				a.recoveryFrame(true)
				a.recoveryFrame(true)
				if len(callbacks) != attempt+1 || a.recovery.timer == nil {
					t.Fatal("lookup did not keep exactly one recovery timer")
				}
				callbacks[attempt]()
				refreshPost(t, posts) // Recovery starts on the app's UI queue.
				refreshPost(t, posts) // The lookup publishes its result.
			}
			wait(t, images, "recovered system artwork")
			refreshPost(t, posts)
			a.recoveryFrame(true)
			if calls.Load() != 3 || a.needsRecovery() || a.recovery.timer != nil || !a.hasTrackCover(current) {
				t.Fatal("successful lookup did not complete recovery")
			}
			if a.pl.Current() != current || a.pl.Playing() || len(a.pl.Queue()) != 1 {
				t.Fatal("artwork recovery changed playback")
			}
		})
	}
}

// Reconnecting retries a failed match even when the background recovery view is inactive.
func TestTrackArtworkReconnectRecovery(t *testing.T) {
	for _, local := range []bool{false, true} {
		t.Run(fmt.Sprintf("local=%v", local), func(t *testing.T) {
			a, posts := artworkRecoveryFixture(t, local)
			search, fetch, offline := searchTrackArtwork, fetchTrackArtwork, netease.Offline()
			t.Cleanup(func() { searchTrackArtwork, fetchTrackArtwork = search, fetch; netease.SetOffline(offline) })
			calls := 0
			searchTrackArtwork = func(string, int) ([]netease.Track, error) {
				calls++
				if calls == 1 {
					return nil, io.ErrUnexpectedEOF
				}
				return []netease.Track{{Name: "Song", Duration: 180000, Artists: []netease.Artist{{Name: "Band"}}, Album: netease.Album{PicURL: "https://p1.music.126.net/test/cover.jpg"}}}, nil
			}
			images := make(chan struct{}, 1)
			cover := artworkTestPNG(t)
			fetchTrackArtwork = func(string) ([]byte, error) { images <- struct{}{}; return cover, nil }
			a.syncTrackArtwork()
			refreshPost(t, posts)
			a.networkChanged(mygo.NetworkStatus{Online: false})
			a.networkChanged(mygo.NetworkStatus{Online: true})
			refreshPost(t, posts)
			wait(t, images, "reconnected artwork")
			refreshPost(t, posts)
			if calls != 2 || !a.hasTrackCover(a.pl.Current()) || a.trackArtworkFailure() != nil {
				t.Fatal("reconnection did not retry the failed artwork search")
			}
		})
	}
}

// A genuine miss stays cached; permanent request errors wait for an explicit refresh instead of automatic retries.
func TestTrackArtworkMissAndPermanentFailure(t *testing.T) {
	for _, failure := range []error{nil, errors.New("permission denied")} {
		t.Run(fmt.Sprint(failure), func(t *testing.T) {
			a, posts := artworkRecoveryFixture(t, false)
			search := searchTrackArtwork
			t.Cleanup(func() { searchTrackArtwork = search })
			calls := 0
			searchTrackArtwork = func(string, int) ([]netease.Track, error) { calls++; return nil, failure }
			a.syncTrackArtwork()
			refreshPost(t, posts)
			a.syncTrackArtwork()
			a.retryLoads(false)
			if calls != 1 || a.trackArt.searching || a.needsRecovery() {
				t.Fatal("miss or permanent error triggered an automatic retry")
			}
			a.retryLoads(true)
			if failure != nil {
				refreshPost(t, posts)
				if calls != 2 {
					t.Fatal("explicit refresh did not retry the failed match")
				}
			} else if calls != 1 || a.trackArt.searching {
				t.Fatal("successful empty search was fetched again")
			}
		})
	}
}

// Retrying other resources must not create an unrelated artwork job or consume another action's completion callback.
func TestTrackArtworkRecoveryOnlyRetriesFailures(t *testing.T) {
	a, _ := artworkRecoveryFixture(t, false)
	search := searchTrackArtwork
	searchTrackArtwork = func(string, int) ([]netease.Track, error) { return nil, nil }
	t.Cleanup(func() { searchTrackArtwork = search })
	a.retryLoads(false)
	a.retryLoads(true)
	if a.trackArt.searching {
		t.Fatal("recovery started a lookup that had not failed")
	}
}

func artworkTestPNG(t *testing.T) []byte {
	t.Helper()
	var b bytes.Buffer
	if err := png.Encode(&b, image.NewRGBA(image.Rect(0, 0, 1, 1))); err != nil {
		t.Fatal(err)
	}
	return b.Bytes()
}

// Failed system-cover downloads reuse the matched URL and existing recovery paths without becoming permanently announced.
func TestTrackArtworkPublicationRecovery(t *testing.T) {
	for _, local := range []bool{false, true} {
		for _, recovery := range []string{"scheduled", "reconnect", "manual"} {
			t.Run(fmt.Sprintf("local=%v/%s", local, recovery), func(t *testing.T) {
				a, posts := artworkRecoveryFixture(t, local)
				mygo.NowPlaying.Clear()
				t.Cleanup(mygo.NowPlaying.Clear)
				current := a.pl.Current()
				const url = "https://p1.music.126.net/test/cover.jpg"
				a.trackArt.known = map[trackArtworkKey]trackArtworkResult{a.artworkKey(current): {url: url}}
				fetch, search, after, offline := fetchTrackArtwork, searchTrackArtwork, recoveryAfter, netease.Offline()
				t.Cleanup(func() {
					fetchTrackArtwork, searchTrackArtwork, recoveryAfter = fetch, search, after
					netease.SetOffline(offline)
				})
				searchTrackArtwork = func(string, int) ([]netease.Track, error) {
					t.Error("publication retry repeated metadata search")
					return nil, nil
				}
				cover := artworkTestPNG(t)
				requested := make(chan string, 4)
				calls := atomic.Int32{}
				fetchTrackArtwork = func(url string) ([]byte, error) {
					n := calls.Add(1)
					requested <- url
					if n == 1 {
						return nil, io.ErrUnexpectedEOF
					}
					return cover, nil
				}
				a.syncTrackArtwork()
				if got := wait(t, requested, "first system cover"); got != netease.Sized(url, nowPlayingCoverPixels) {
					t.Fatalf("URL %q", got)
				}
				a.syncTrackArtwork()
				a.syncTrackArtwork()
				if calls.Load() != 1 || !a.trackArt.publication.loading || a.trackArt.publication.done {
					t.Fatal("in-flight publication was duplicated or marked complete")
				}
				refreshPost(t, posts)
				if a.trackArtworkFailure() == nil || a.trackArt.publication.done || !a.needsRecovery() {
					t.Fatal("failed publication was forgotten")
				}
				if _, shown := mygo.NowPlaying.Info(); shown {
					t.Fatal("failed cover was published")
				}
				a.syncTrackArtwork()
				if calls.Load() != 1 {
					t.Fatal("publication retried without recovery/backoff")
				}
				switch recovery {
				case "scheduled":
					var callback func()
					timer := time.NewTimer(time.Hour)
					t.Cleanup(func() { timer.Stop() })
					recoveryAfter = func(_ time.Duration, fn func()) *time.Timer { callback = fn; return timer }
					a.recovery.active = true
					a.recoveryFrame(true)
					if callback == nil {
						t.Fatal("publication failure did not schedule recovery")
					}
					callback()
					refreshPost(t, posts)
				case "reconnect":
					a.networkChanged(mygo.NetworkStatus{Online: false})
					a.networkChanged(mygo.NetworkStatus{Online: true})
				case "manual":
					a.retryLoads(true)
				}
				wait(t, requested, "retried system cover")
				refreshPost(t, posts)
				info, shown := mygo.NowPlaying.Info()
				if calls.Load() != 2 || !a.trackArt.publication.done || a.trackArt.publication.loading || a.trackArtworkFailure() != nil || !shown || info.Title != current.Name || !bytes.Equal(info.Artwork, cover) {
					t.Fatal("cover did not reach system metadata after recovery")
				}
				a.syncTrackArtwork()
				if calls.Load() != 2 {
					t.Fatal("successful publication was downloaded twice")
				}
				if a.pl.Current() != current || a.pl.Playing() {
					t.Fatal("cover recovery changed playback")
				}
			})
		}
	}
}

// Empty cover responses remain retryable, while permanent download failures wait for an explicit refresh.
func TestTrackArtworkPublicationErrorKinds(t *testing.T) {
	for _, permanent := range []bool{false, true} {
		t.Run(fmt.Sprint(permanent), func(t *testing.T) {
			a, posts := artworkRecoveryFixture(t, false)
			mygo.NowPlaying.Clear()
			t.Cleanup(mygo.NowPlaying.Clear)
			a.trackArt.known = map[trackArtworkKey]trackArtworkResult{a.artworkKey(a.pl.Current()): {url: "https://p1.music.126.net/test/cover.jpg"}}
			fetch := fetchTrackArtwork
			t.Cleanup(func() { fetchTrackArtwork = fetch })
			cover := artworkTestPNG(t)
			calls := atomic.Int32{}
			fetchTrackArtwork = func(string) ([]byte, error) {
				if calls.Add(1) == 1 {
					if permanent {
						return nil, errors.New("permission denied")
					}
					return nil, nil
				}
				return cover, nil
			}
			a.syncTrackArtwork()
			refreshPost(t, posts)
			if a.needsRecovery() == permanent {
				t.Fatal("wrong automatic retry classification")
			}
			if permanent {
				a.retryLoads(false)
				if calls.Load() != 1 {
					t.Fatal("permanent error retried automatically")
				}
			}
			a.retryLoads(true)
			refreshPost(t, posts)
			info, shown := mygo.NowPlaying.Info()
			if calls.Load() != 2 || !shown || !bytes.Equal(info.Artwork, cover) {
				t.Fatal("manual publication retry failed")
			}
		})
	}
}

// An old song's late cover failure cannot clear or replace the current song's pending or completed publication.
func TestTrackArtworkPublicationRejectsOldSong(t *testing.T) {
	for _, oldFirst := range []bool{false, true} {
		t.Run(fmt.Sprint(oldFirst), func(t *testing.T) {
			a, posts := artworkRecoveryFixture(t, false)
			mygo.NowPlaying.Clear()
			t.Cleanup(mygo.NowPlaying.Clear)
			first := a.pl.Current()
			const firstURL = "https://p1.music.126.net/test/first.jpg"
			const nextURL = "https://p1.music.126.net/test/second.jpg"
			a.trackArt.known = map[trackArtworkKey]trackArtworkResult{a.artworkKey(first): {url: firstURL}}
			fetch := fetchTrackArtwork
			t.Cleanup(func() { fetchTrackArtwork = fetch })
			firstRelease, nextRelease := make(chan struct{}), make(chan struct{})
			releaseFirst := sync.OnceFunc(func() { close(firstRelease) })
			releaseNext := sync.OnceFunc(func() { close(nextRelease) })
			defer releaseFirst()
			defer releaseNext()
			requested := make(chan string, 2)
			cover := artworkTestPNG(t)
			fetchTrackArtwork = func(url string) ([]byte, error) {
				requested <- url
				if url == netease.Sized(firstURL, nowPlayingCoverPixels) {
					<-firstRelease
					return nil, io.ErrUnexpectedEOF
				}
				<-nextRelease
				return cover, nil
			}
			a.syncTrackArtwork()
			wait(t, requested, "first cover started")
			second := *first
			second.ID++
			second.Name = "Second song"
			player.Cue(a.pl, []netease.Track{second})
			player.SetLyrics(a.pl, nil)
			current := a.pl.Current()
			a.trackArt.known[a.artworkKey(current)] = trackArtworkResult{url: nextURL}
			a.syncTrackArtwork()
			wait(t, requested, "second cover started")
			if oldFirst {
				releaseFirst()
				refreshPost(t, posts)
				if a.trackArt.publication.track != current || !a.trackArt.publication.loading || a.trackArtworkFailure() != nil {
					t.Fatal("old error changed the current publication")
				}
				releaseNext()
				refreshPost(t, posts)
			} else {
				releaseNext()
				refreshPost(t, posts)
				releaseFirst()
				refreshPost(t, posts)
			}
			info, shown := mygo.NowPlaying.Info()
			if !shown || info.Title != "Second song" || !bytes.Equal(info.Artwork, cover) || a.trackArtworkFailure() != nil || !a.trackArt.publication.done {
				t.Fatal("late response replaced the new song's artwork")
			}
		})
	}
}
