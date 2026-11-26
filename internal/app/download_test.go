/*
验证下载设置、队列控制、目录选择、重试以及账号切换和退出时的取消清理。
*/

package app

import (
	"context"
	"errors"
	"fmt"
	"image/png"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"testing"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"

	"netease-music-mygo/internal/features/download"
	"netease-music-mygo/internal/features/netease"
	downloadsvm "netease-music-mygo/internal/ui/viewmodel/downloads"
)

func downloadTestApp(t *testing.T) (*app, chan func()) {
	t.Helper()
	isolateUserData(t)
	a := newTestApp()
	a.settings = defaultSettings()
	a.preferences.Section = "download"
	a.settings.Path = filepath.Join(t.TempDir(), "settings.json")
	a.settings.DownloadDirectory = t.TempDir()
	old := saveDownload
	t.Cleanup(func() { saveDownload = old })
	return a, testPosts(a)
}

// The download-quality control saves independently from playback quality,
// and directory/sidecar controls persist the values consumed by new jobs.
func TestDownloadSettingsIndependentQuality(t *testing.T) {
	a, _ := downloadTestApp(t)
	a.settings.DownloadQuality = "standard"
	a.settings.Quality = "lossless"
	tt := ui.NewTester(a.views.settings.Build, 780, 640)
	tt.Frame()
	control, ok := tt.AccessByRole(ui.RolePopUpButton, "默认下载音质")
	if !ok {
		t.Fatal("download quality control missing")
	}
	r := control.Bounds
	tt.ClickAt(r.X+r.W/2, r.Y+r.H/2)
	tt.Frame()
	if err := tt.Click("Hi-Res"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	if a.settings.DownloadQuality != "hires" || a.settings.Quality != "lossless" {
		t.Fatalf("qualities: %s / %s", a.settings.DownloadQuality, a.settings.Quality)
	}
	old := openDownloadDirectory
	defer func() { openDownloadDirectory = old }()
	dir := t.TempDir()
	openDownloadDirectory = func(mygo.OpenDialogOptions) ([]string, error) { return []string{dir}, nil }
	if err := tt.Click("选择目录…"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	clickSettingsSwitch(t, tt, "另存歌词文件")
	saved := readCacheSettings(t, a.settings.Path)
	if saved.DownloadQuality != "hires" || saved.Quality != "lossless" || saved.DownloadDirectory != dir || !saved.DownloadSidecar {
		t.Fatal("download choices not saved")
	}
	showSettingsPage(t, tt, "播放")
	showSettingsPage(t, tt, "下载")
	tt.Frame()
}

// A naming choice persists and is snapshotted by new jobs; changing it does
// not rename an active request, and a distinct output format is not deduplicated.
func TestDownloadSettingsNameFormat(t *testing.T) {
	a, posts := downloadTestApp(t)
	type request struct {
		id     int64
		format string
	}
	started := make(chan request, 4)
	allow := make(chan struct{})
	t.Cleanup(func() {
		select {
		case <-allow:
		default:
			close(allow)
		}
	})
	saveDownload = func(ctx context.Context, tr netease.Track, opts download.Options) (download.Result, error) {
		started <- request{tr.ID, opts.NameFormat}
		<-allow
		return download.Result{Path: "/tmp/song.flac"}, nil
	}
	tt := ui.NewTester(a.views.settings.Build, 780, 720)
	tt.Frame()
	control, ok := tt.AccessByRole(ui.RolePopUpButton, "下载文件名格式")
	if !ok {
		t.Fatal("filename format selector missing")
	}
	r := control.Bounds
	tt.ClickAt(r.X+r.W/2, r.Y+r.H/2)
	tt.Frame()
	if err := tt.Click("歌名 - 歌手"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	if got := readCacheSettings(t, a.settings.Path).DownloadNameFormat; got != download.NameTitleArtist {
		t.Fatal("naming preference not saved", got)
	}
	a.enqueueDownloads([]netease.Track{{ID: 7}})
	first := wait(t, started, "first naming request")
	if first.format != download.NameTitleArtist {
		t.Fatal(first)
	}
	a.settings.DownloadNameFormat = download.NameTitle
	a.enqueueDownloads([]netease.Track{{ID: 7}})
	if len(a.downloads.Jobs) != 2 || a.downloads.Jobs[0].Options.NameFormat != download.NameTitleArtist {
		t.Fatal("new choice changed or suppressed a different output request")
	}
	close(allow)
	refreshPost(t, posts)
	second := wait(t, started, "second naming request")
	if second.format != download.NameTitle {
		t.Fatal(second)
	}
	refreshPost(t, posts)
}

// Duplicate enqueues share one job; only one transfer runs, and cancellation
// or failure releases the queue while retry/reveal/clear act on that same job.
func TestDownloadQueueControls(t *testing.T) {
	a, posts := downloadTestApp(t)
	type call struct {
		ctx  context.Context
		id   int64
		opts download.Options
	}
	calls := make(chan call, 4)
	answers := make(chan error, 4)
	saveDownload = func(ctx context.Context, tr netease.Track, o download.Options) (download.Result, error) {
		calls <- call{ctx, tr.ID, o}
		select {
		case <-ctx.Done():
			return download.Result{}, ctx.Err()
		case err := <-answers:
			if err != nil {
				return download.Result{}, err
			}
			return download.Result{Path: "/tmp/song.flac", Quality: "lossless"}, nil
		}
	}
	tracks := []netease.Track{{ID: 1, Name: "第一首"}, {ID: 2, Name: "第二首"}}
	a.enqueueDownloads(tracks)
	a.enqueueDownloads(tracks)
	first := wait(t, calls, "first download")
	if first.id != 1 || len(a.downloads.Jobs) != 2 || len(calls) != 0 {
		t.Fatal("queue duplicated")
	}
	if first.opts.Quality != a.settings.DownloadQuality {
		t.Fatal("download quality lost")
	}
	tt := ui.NewTester(a.views.settings.Build, 780, 720)
	tt.Frame()
	if err := tt.Click("取消"); err != nil {
		t.Fatal(err)
	}
	refreshPost(t, posts)
	second := wait(t, calls, "next download")
	if second.id != 2 || !a.downloads.Jobs[0].Cancelled {
		t.Fatal("cancel blocked queue")
	}
	answers <- errors.New("transient failure")
	refreshPost(t, posts)
	tt.Frame()
	// Remove the canceled first job so the failure's retry is unambiguous.
	a.downloads.Jobs = a.downloads.Jobs[1:]
	tt.Frame()
	if err := tt.Click("重试"); err != nil {
		t.Fatal(err)
	}
	retry := wait(t, calls, "retry download")
	if retry.id != 2 {
		t.Fatal("retry changed song")
	}
	answers <- nil
	refreshPost(t, posts)
	tt.Frame()
	oldReveal := revealDownload
	defer func() { revealDownload = oldReveal }()
	revealed := ""
	revealDownload = func(path string) { revealed = path }
	if err := tt.Click("显示文件"); err != nil {
		t.Fatal(err)
	}
	if revealed != "/tmp/song.flac" {
		t.Fatal("wrong file revealed")
	}
	if err := tt.Click("清除已完成"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	if len(a.downloads.Jobs) != 0 {
		t.Fatal("finished job retained")
	}
}

// Retrying cannot bypass enqueue deduplication or keep an old failed
// transfer's quality, format and warning while the next attempt is running.
func TestDownloadRetrySharesDedupAndClearsResult(t *testing.T) {
	a, posts := downloadTestApp(t)
	started := make(chan struct{}, 2)
	release := make(chan struct{})
	saveDownload = func(ctx context.Context, tr netease.Track, opts download.Options) (download.Result, error) {
		started <- struct{}{}
		<-release
		return download.Result{Path: "/tmp/song.flac"}, nil
	}
	track := netease.Track{ID: 77}
	failed := &downloadsvm.DownloadJob{Track: track, Options: download.Options{Directory: a.settings.DownloadDirectory, Quality: a.settings.DownloadQuality, NameFormat: download.NameArtistTitle}, Finished: true, Result: download.Result{Quality: "lossless", Format: "flac", Warning: "old warning"}}
	a.downloads.Jobs = []*downloadsvm.DownloadJob{failed}
	a.enqueueDownloads([]netease.Track{track})
	wait(t, started, "new enqueue")
	a.retryDownload(failed)
	if !failed.Finished {
		t.Fatal("retry duplicated an active transfer")
	}
	close(release)
	refreshPost(t, posts)
	a.retryDownload(failed)
	wait(t, started, "retry after other transfer finished")
	if failed.Result != (download.Result{}) || failed.Cancelled || failed.Finished {
		t.Fatal("retry retained a stale result")
	}
	refreshPost(t, posts)
}

// Account transitions cancel active and pending private downloads; an old
// completion must not start the next upload under another account's session.
func TestDownloadsCancelOnAccountChange(t *testing.T) {
	a, posts := downloadTestApp(t)
	started := make(chan struct{}, 1)
	saveDownload = func(ctx context.Context, tr netease.Track, o download.Options) (download.Result, error) {
		started <- struct{}{}
		<-ctx.Done()
		return download.Result{}, ctx.Err()
	}
	a.enqueueDownloads([]netease.Track{cloudTrack(1, "一"), cloudTrack(2, "二")})
	wait(t, started, "download start")
	a.downloads.CancelAll()
	refreshPost(t, posts)
	if a.downloads.Busy() || len(started) != 0 || !a.downloads.Jobs[1].Finished {
		t.Fatal("private download survived account transition")
	}
}

// Quitting waits for cancellation cleanup and prevents queued or newly
// enqueued work from starting before the worker permits the final quit.
func TestDownloadQuitWaitsForCleanup(t *testing.T) {
	a, posts := downloadTestApp(t)
	started, cleaned := make(chan struct{}), make(chan struct{})
	saveDownload = func(ctx context.Context, tr netease.Track, o download.Options) (download.Result, error) {
		close(started)
		<-ctx.Done()
		<-cleaned
		return download.Result{}, ctx.Err()
	}
	old := quitAfterDownload
	defer func() { quitAfterDownload = old }()
	quits := 0
	quitAfterDownload = func() { quits++ }
	a.enqueueDownloads([]netease.Track{{ID: 1}, {ID: 2}})
	wait(t, started, "active download")
	if !a.downloads.DeferQuit() || quits != 0 {
		t.Fatal("quit did not defer")
	}
	a.enqueueDownloads([]netease.Track{{ID: 3}})
	if len(a.downloads.Jobs) != 2 {
		t.Fatal("enqueued while quitting")
	}
	close(cleaned)
	refreshPost(t, posts)
	if quits != 1 || a.downloads.Busy() || a.downloads.DeferQuit() {
		t.Fatal("cleanup did not finish quit")
	}
}

// Both catalogue and private-upload menus enqueue the selected identity and
// leave playback alone.
func TestSongMenusDownloadWithoutPlaying(t *testing.T) {
	for _, cloud := range []bool{false, true} {
		t.Run(map[bool]string{false: "catalogue", true: "cloud"}[cloud], func(t *testing.T) {
			a, posts := downloadTestApp(t)
			called := make(chan netease.Track, 1)
			saveDownload = func(ctx context.Context, tr netease.Track, o download.Options) (download.Result, error) {
				called <- tr
				return download.Result{Path: "/tmp/track"}, nil
			}
			tr := netease.Track{ID: 71, Name: "下载此曲", Cloud: cloud}
			tt := ui.NewTester(func(c *ui.Context) { a.views.songMenu.Button(c, &tr, 18, ui.Hex("#333333")) }, 300, 150)
			tt.Frame()
			if err := tt.Click("更多"); err != nil {
				t.Fatal(err)
			}
			if err := tt.ChooseMenuItem("下载"); err != nil {
				t.Fatal(err)
			}
			got := wait(t, called, "menu download")
			refreshPost(t, posts)
			if got.ID != tr.ID || got.Cloud != cloud || a.pl.Current() != nil {
				t.Fatal("download changed source or started playback")
			}
		})
	}
}

// Downloads without a saved directory use the listener's Music folder without
// opening a picker; a saved custom directory still controls the output request.
func TestDownloadsUseDefaultOrChosenDirectory(t *testing.T) {
	home, err := os.UserHomeDir()
	if err != nil {
		t.Fatal(err)
	}
	for _, custom := range []bool{false, true} {
		t.Run(map[bool]string{false: "default", true: "custom"}[custom], func(t *testing.T) {
			a, posts := downloadTestApp(t)
			a.settings.DownloadDirectory = ""
			want := filepath.Join(home, "Music", "Nyago Music")
			if custom {
				want = t.TempDir()
				a.settings.DownloadDirectory = want
			}
			old := openDownloadDirectory
			t.Cleanup(func() { openDownloadDirectory = old })
			openDownloadDirectory = func(mygo.OpenDialogOptions) ([]string, error) {
				t.Error("download opened an unnecessary directory picker")
				return nil, nil
			}
			called := make(chan string, 1)
			saveDownload = func(ctx context.Context, tr netease.Track, opts download.Options) (download.Result, error) {
				called <- opts.Directory
				return download.Result{Path: filepath.Join(opts.Directory, "song.flac")}, nil
			}
			a.enqueueDownloads([]netease.Track{{ID: 71}})
			if got := wait(t, called, "download directory"); got != want {
				t.Fatalf("download directory = %q, want %q", got, want)
			}
			refreshPost(t, posts)
		})
	}
}

// Canceling or failing the settings picker preserves the existing download
// directory and never creates a transfer as a side effect.
func TestDownloadDirectoryPickerKeepsChoice(t *testing.T) {
	for _, failure := range []error{nil, errors.New("picker unavailable")} {
		t.Run(fmt.Sprint(failure), func(t *testing.T) {
			a, _ := downloadTestApp(t)
			before := a.settings.DownloadDirectory
			old := openDownloadDirectory
			t.Cleanup(func() { openDownloadDirectory = old })
			openDownloadDirectory = func(mygo.OpenDialogOptions) ([]string, error) { return nil, failure }
			tt := ui.NewTester(a.views.settings.Build, 780, 640)
			tt.Frame()
			if err := tt.Click("选择目录…"); err != nil {
				t.Fatal(err)
			}
			tt.Frame()
			if a.settings.DownloadDirectory != before || len(a.downloads.Jobs) != 0 || (a.downloads.Err != "") != (failure != nil) {
				t.Fatal("picker changed the saved directory or enqueued work")
			}
		})
	}
}

// Playlist and album batch actions preserve the source list, skip unsupported
// entries and duplicates, and download both source identities without playback.
func TestDownloadBatchMenus(t *testing.T) {
	for _, page := range []string{"playlist", "album"} {
		t.Run(page, func(t *testing.T) {
			a, posts := downloadTestApp(t)
			tracks := []netease.Track{{ID: 71}, {ID: 71}, {ID: 71, Cloud: true}, {ID: 2, LocalID: 2}, {ID: 3, ProgramID: 3}, {}, {ID: -1}, {ID: 72}}
			before := slices.Clone(tracks)
			called := make(chan netease.Track, len(tracks))
			saveDownload = func(ctx context.Context, tr netease.Track, opts download.Options) (download.Result, error) {
				called <- tr
				return download.Result{Path: "/tmp/downloaded.flac"}, nil
			}
			tt := ui.NewTester(func(c *ui.Context) {
				if page == "playlist" {
					a.views.library.Header(c, &netease.Playlist{ID: 5, Tracks: tracks})
				} else {
					a.views.album.Header(c, &netease.AlbumProfile{Album: netease.Album{ID: 5}, Songs: tracks})
				}
			}, 900, 400)
			tt.Frame()
			if err := tt.Click("更多"); err != nil {
				t.Fatal(err)
			}
			if err := tt.ChooseMenuItem("下载全部"); err != nil {
				t.Fatal(err)
			}
			for _, want := range []netease.Track{{ID: 71}, {ID: 71, Cloud: true}, {ID: 72}} {
				got := wait(t, called, "batch download")
				if got.ID != want.ID || got.Cloud != want.Cloud {
					t.Fatalf("downloaded %+v, want %+v", got, want)
				}
				refreshPost(t, posts)
			}
			tt.Frame()
			if len(a.downloads.Jobs) != 3 || a.downloads.Busy() || a.pl.Current() != nil || !reflect.DeepEqual(tracks, before) {
				t.Fatal("batch changed its source, playback or queue membership")
			}
		})
	}
}

// Worker progress changes UI state only after dispatch, and late callbacks
// cannot revive canceled jobs or start a canceled pending transfer.
func TestDownloadProgressAndPendingCancellation(t *testing.T) {
	a, posts := downloadTestApp(t)
	started := make(chan func(string, int64, int64), 2)
	release := make(chan struct{})
	t.Cleanup(func() {
		select {
		case <-release:
		default:
			close(release)
		}
	})
	saveDownload = func(ctx context.Context, tr netease.Track, opts download.Options) (download.Result, error) {
		started <- opts.Progress
		<-release
		return download.Result{}, ctx.Err()
	}
	a.enqueueDownloads([]netease.Track{{ID: 1}, {ID: 2}})
	progress := wait(t, started, "active transfer")
	active, pending := a.downloads.Jobs[0], a.downloads.Jobs[1]
	progress("test-stage", 20, 100)
	if active.Received != 0 {
		t.Fatal("worker mutated UI state before dispatch")
	}
	refreshPost(t, posts)
	if active.Received != 20 || active.Total != 100 || active.Status != "test-stage" {
		t.Fatal("dispatched progress was lost")
	}
	a.downloads.Cancel(pending)
	a.downloads.Cancel(active)
	status := active.Status
	progress("late-stage", 90, 100)
	refreshPost(t, posts)
	if active.Status != status || active.Received != 20 || !pending.Finished || !pending.Cancelled {
		t.Fatal("late progress revived canceled work")
	}
	close(release)
	refreshPost(t, posts)
	status = active.Status
	progress("after-completion", 100, 100)
	refreshPost(t, posts)
	if active.Status != status || active.Received != 20 || !active.Finished || !active.Cancelled || a.downloads.Busy() || len(started) != 0 {
		t.Fatal("completion restarted canceled work or accepted stale progress")
	}
}

// Native settings rendering is inspected at both window sizes and themes,
// including long source errors and metadata warnings in a populated queue.
func TestDownloadSettingsFrames(t *testing.T) {
	dir := os.Getenv("NM_FRAMES")
	if dir == "" {
		t.Skip()
	}
	isolateUserData(t)
	for _, dark := range []bool{false, true} {
		for _, size := range []struct {
			name string
			w, h int
		}{{"normal", 780, 640}, {"minimum", 640, 420}} {
			a := newTestApp()
			a.settings = defaultSettings()
			a.preferences.Section = "download"
			a.downloads.Jobs = []*downloadsvm.DownloadJob{{Track: netease.Track{Name: "春日影", Artists: []netease.Artist{{Name: "MyGO!!!!!"}}}, Status: "正在下载", Received: 28, Total: 100}, {Track: netease.Track{Name: "没有封面的音乐"}, Status: "下载完成", Finished: true, Result: download.Result{Path: "/tmp/a.flac", Quality: "lossless", Warning: "网易云未提供封面"}}, {Track: netease.Track{Name: "下载失败的歌曲"}, Status: "下载失败", Finished: true, Err: errors.New("获取专辑元数据：网络连接失败，请重试")}}
			tt := ui.NewTester(a.views.settings.Build, size.w, size.h)
			tt.SetDark(dark)
			tt.SetTitleBar(ui.TitleBar{Height: 40, Left: 78})
			tt.Frame()
			name := size.name + "-light"
			if dark {
				name = size.name + "-dark"
			}
			f, err := os.Create(filepath.Join(dir, "downloads-"+name+".png"))
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
