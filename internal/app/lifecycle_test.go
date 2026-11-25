package app

import (
	"context"
	"os"
	"path/filepath"
	"slices"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/egoist/mygo"

	playbackview "netease-music-mygo/internal/desktop/view/playback"
	"netease-music-mygo/internal/download"
	"netease-music-mygo/internal/netease"
	"netease-music-mygo/internal/preferences"
)

// Dock reopening respects utility windows, but an overlay alone cannot consume a reopen request.
func TestActivationRespectsUtilityWindows(t *testing.T) {
	a := newApp()
	a.settingsWin = new(mygo.Window)
	a.activate()
	if a.startup.show {
		t.Fatal("settings activation requested the main window")
	}
	a.settingsWin = nil
	a.localView.window = new(mygo.Window)
	a.activate()
	if a.startup.show {
		t.Fatal("folder activation requested the main window")
	}
	if !a.windowOpen() {
		t.Fatal("folder manager did not keep the Dock available")
	}
	a.localView.window = nil
	a.notch = &playbackview.Notch{A: &a.views.playback}
	a.lyrics = &playbackview.DeskLyrics{A: &a.views.playback}
	a.activate()
	if !a.startup.show {
		t.Fatal("overlay prevented main-window reopening")
	}
}

// The OS may force quit while cache cleanup is pending; the last committed preferences still save.
func TestLifecycleSessionEndingSavesCommittedState(t *testing.T) {
	isolateUserData(t)
	a := newTestApp()
	a.initialized = true
	a.settings = defaultSettings()
	a.settings.Path = filepath.Join(t.TempDir(), "settings.json")
	a.playbackState.Volume = 0.37
	a.cacheSettings.Moving = true
	if !a.requestQuit(true) || !a.quitting {
		t.Fatal("OS session ending was deferred")
	}
	if !a.requestQuit(false) {
		t.Fatal("repeated quit reentered deferral")
	}
	saved, ok := preferences.Load(a.settings.Path)
	if !ok || saved.Volume != 0.37 {
		t.Fatal("forced quit skipped the last committed preferences")
	}
	a.release()
}

// A periodic save already queued when quitting cannot overwrite the final snapshot or rearm its timer.
func TestLifecycleStopsPeriodicSessionSave(t *testing.T) {
	a := newTestApp()
	old := sessionAfter
	t.Cleanup(func() { sessionAfter = old })
	var due func()
	stops, starts := 0, 0
	sessionAfter = func(_ time.Duration, fn func()) func() {
		starts++
		due = fn
		return func() { stops++ }
	}
	a.keepSession()
	if due == nil {
		t.Fatal("session timer did not start")
	}
	a.release()
	due()
	a.keepSession()
	if stops != 1 || starts != 1 || a.sessionDue || a.sessionSavePath != "" {
		t.Fatalf("late session save survived shutdown: stops=%d, starts=%d, path=%s", stops, starts, a.sessionSavePath)
	}
}

// A file and URL share startup ordering; neither can overwrite a newer intent after restoration.
func TestStartupMixedOpeningOrder(t *testing.T) {
	for _, fileLast := range []bool{false, true} {
		t.Run(map[bool]string{false: "link last", true: "file last"}[fileLast], func(t *testing.T) {
			a := musicLinkApp(t)
			posts := testPosts(a)
			a.startup.waiting = true
			file := filepath.Join("..", "player", "testdata", "seek.flac")
			if fileLast {
				a.openMusicInput("orpheus://album/34")
				a.openTemporaryAudio(file)
			} else {
				a.openTemporaryAudio(file)
				a.openMusicInput("orpheus://album/34")
			}
			if a.pl.Current() != nil || a.nav.Current.Page != pageHome {
				t.Fatal("opening ran before restoration")
			}
			a.restoreSession()
			a.finishStartup()
			if fileLast {
				refreshPost(t, posts)
				if a.pl.Current() == nil || !a.pl.Current().Temporary || a.nav.Current.Page == pageAlbum {
					t.Fatal("older link displaced the last file")
				}
			} else if a.nav.Current != (route{Page: pageAlbum, ID: 34}) || a.pl.Current() != nil {
				t.Fatal("older file displaced the last link")
			}
			// Another account acceptance cannot consume the startup intent twice.
			a.nav.Current = route{Page: pageLocal}
			a.finishStartup()
			if a.nav.Current.Page != pageLocal {
				t.Fatal("startup replayed an already consumed intent")
			}
		})
	}
}

// Shutdown discards both native-queued and early callbacks, releasing each transferred resource once.
func TestDispatchShutdownOwnsPendingResources(t *testing.T) {
	for _, attached := range []bool{false, true} {
		a := newApp()
		var posts chan func()
		if attached {
			posts = testPosts(a)
		}
		runs, releases := 0, 0
		a.postResource(func() { runs++ }, func() { releases++ })
		a.post(func() { runs++ })
		a.closePosts()
		a.closePosts()
		a.flushKept()
		if attached {
			for len(posts) > 0 {
				(<-posts)()
			}
		}
		a.postResource(func() { runs++ }, func() { releases++ })
		a.post(func() { runs++ })
		if runs != 0 || releases != 2 {
			t.Fatalf("attached=%v: ran %d, released %d", attached, runs, releases)
		}
	}
}

// A worker finishing concurrently with dispatcher closure cannot leak or double-release its result.
func TestDispatchConcurrentShutdown(t *testing.T) {
	a := newApp()
	var released atomic.Int32
	var workers sync.WaitGroup
	start := make(chan struct{})
	for range 100 {
		workers.Go(func() {
			<-start
			a.postResource(func() { t.Error("ran after shutdown") }, func() { released.Add(1) })
		})
	}
	close(start)
	a.closePosts()
	workers.Wait()
	a.flushKept()
	if released.Load() != 100 {
		t.Fatalf("released %d of 100 results", released.Load())
	}
}

// Early callbacks may enqueue more work without deadlocking or crossing into a different app.
func TestDispatchStartupReentrancy(t *testing.T) {
	a, other := newApp(), newApp()
	var got []int
	a.post(func() { got = append(got, 1); a.post(func() { got = append(got, 3) }) })
	a.post(func() { got = append(got, 2) })
	other.flushKept()
	if len(got) != 0 {
		t.Fatal("another app drained the startup queue")
	}
	a.flushKept()
	a.flushKept()
	if !slices.Equal(got, []int{1, 2, 3}) {
		t.Fatalf("lost or reordered callbacks: %v", got)
	}
}

// Releasing a partially initialized app detaches subscriptions and rejects late native events once.
func TestLifecyclePartialStartupRelease(t *testing.T) {
	a := newApp()
	var events []string
	a.native.Listen(func() { events = append(events, "app") })
	a.network.stop = func() { events = append(events, "network") }
	a.notch = &playbackview.Notch{A: &a.views.playback, Unwatch: func() { events = append(events, "pointer") }}
	a.notch.Lifetime.Listen(func() { events = append(events, "notch") })
	a.lyrics = &playbackview.DeskLyrics{A: &a.views.playback}
	a.lyrics.Lifetime.Listen(func() { events = append(events, "lyrics") })
	a.release()
	a.release()
	a.showMain()
	a.openSettings()
	a.openLocalFolders()
	a.openMusicInput("orpheus://album/34")
	a.openTemporaryAudio("song.flac")
	if !slices.Equal(events, []string{"network", "notch", "pointer", "lyrics", "app"}) || a.startup.show || a.startup.pending != nil {
		t.Fatalf("partial startup left live resources: %v, %+v", events, a.startup)
	}
}

// A delayed old window closure must not clear the replacement window's interaction state.
func TestSettingsClosureKeepsReplacement(t *testing.T) {
	a := newApp()
	old, next := new(mygo.Window), new(mygo.Window)
	a.settingsWin, a.Recording = next, "global/playPause"
	a.cacheSettings.Confirm.Open = true
	a.settingsClosed(old)
	if a.settingsWin != next || a.Recording == "" || !a.cacheSettings.Confirm.Open {
		t.Fatal("old closure reset the replacement")
	}
	a.quitting = true // no native Dock in a headless test
	a.settingsClosed(next)
	if a.settingsWin != nil || a.Recording != "" || a.cacheSettings.Confirm.Open {
		t.Fatal("closed window retained interaction state")
	}
}

// Quit must keep result dispatch alive until the downloader has released its tag writer and files.
func TestLifecycleQuitWaitsForDownload(t *testing.T) {
	a, posts := downloadTestApp(t)
	started, canceled, cleaned := make(chan struct{}), make(chan struct{}), make(chan struct{})
	saveDownload = func(ctx context.Context, _ netease.Track, _ download.Options) (download.Result, error) {
		close(started)
		<-ctx.Done()
		close(canceled)
		<-cleaned
		return download.Result{}, ctx.Err()
	}
	oldQuit := quitAfterDownload
	t.Cleanup(func() { quitAfterDownload = oldQuit })
	quits := 0
	quitAfterDownload = func() { quits++ }
	a.enqueueDownloads([]netease.Track{{ID: 1}})
	wait(t, started, "download")
	a.cacheSettings.Moving = true
	if a.requestQuit(false) || a.requestQuit(false) || a.quitting {
		t.Fatal("closed dispatch before downloader cleanup")
	}
	wait(t, canceled, "download cancellation while cache cleanup is pending")
	close(cleaned)
	refreshPost(t, posts)
	a.cacheSettings.Moving = false
	if quits != 1 || !a.requestQuit(false) || !a.quitting {
		t.Fatal("did not finish deferred quit")
	}
	a.release()
}

// Quitting before readiness must not replace saved user settings with a zero-value snapshot.
func TestLifecycleQuitBeforeReadyPreservesSettings(t *testing.T) {
	isolateUserData(t)
	path := filepath.Join(t.TempDir(), "settings.json")
	const original = "saved settings"
	if err := os.WriteFile(path, []byte(original), 0600); err != nil {
		t.Fatal(err)
	}
	a := newApp()
	a.settings.Path = path
	if !a.requestQuit(false) {
		t.Fatal("empty startup prevented quit")
	}
	a.release()
	b, err := os.ReadFile(path)
	if err != nil || string(b) != original {
		t.Fatalf("early quit overwrote preferences: %q, %v", b, err)
	}
}

// A notch disposed with a queued timer cannot restart polling or read the system cursor.
func TestNotchCloseRejectsQueuedPoll(t *testing.T) {
	a := newApp()
	posts := testPosts(a)
	n := &playbackview.Notch{A: &a.views.playback}
	a.settings.Notch = true
	old := cursorPoint
	t.Cleanup(func() { cursorPoint = old })
	cursorPoint = func() mygo.Point { t.Fatal("closed notch polled the cursor"); return mygo.Point{} }
	a.dispatch(n.Poll, false)
	n.PollTimer = time.AfterFunc(time.Hour, func() { t.Error("timer survived close") })
	n.Close()
	n.Close()
	(<-posts)()
	if n.Polling || n.Watched || n.Win != nil {
		t.Fatal("notch remained active after close")
	}
}
