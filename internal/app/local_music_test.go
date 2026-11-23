package app

import (
	"os"
	"path/filepath"
	"slices"
	"testing"
	"time"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"

	"netease-music-mygo/internal/localcatalog"
	"netease-music-mygo/internal/netease"
	"netease-music-mygo/internal/player"
)

// A local song's queue menu removes its entry instead of offering to add it again.
func TestLocalQueueMenuRemovesEntry(t *testing.T) {
	a := newTestApp()
	a.settings = defaultSettings()
	current := netease.Track{ID: 31, Name: "正在播放的歌"}
	local := netease.Track{LocalID: 1, Name: "本地待播歌曲"}
	const path = "../player/testdata/seek.flac"
	a.local.Replace(localcatalog.Catalog{Songs: []localcatalog.Song{{Track: local, Path: path}}})
	a.pl.Cue([]netease.Track{current})
	a.pl.Hold(17 * time.Second)
	a.pl.SetLyrics(nil)
	a.comments = commentState{track: current.Key(), loaded: true}
	a.nav.Current = route{Page: pageLocal}
	tt := ui.NewTester(a.view, 1200, 800)

	if err := tt.RightClick(local.Name); err != nil {
		t.Fatal(err)
	}
	if err := tt.ChooseMenuItem("下一首播放"); err != nil {
		t.Fatal(err)
	}
	a.nowPlaying, a.npPane = true, paneQueue
	tt.Frame()
	tt.Advance(time.Second)
	if err := tt.RightClick(local.Name); err != nil {
		t.Fatal(err)
	}
	if menu := tt.Menu(); slices.Contains(menu, "下一首播放") || slices.Contains(menu, "加入队列") {
		t.Fatalf("the queued song still offers the library actions: %q", menu)
	}
	if err := tt.ChooseMenuItem("从待播清单移除"); err != nil {
		t.Fatal(err)
	}
	if q := a.pl.Queue(); len(q) != 1 || q[0].ID != current.ID || a.pl.Current().ID != current.ID ||
		!a.pl.Playing() || a.pl.Position() != 17*time.Second || a.local.Song(local.LocalID).Path != path {
		t.Fatalf("removal changed playback or the library: %+v", q)
	}
}

// The library can promote a queued song, remove it, and add it again.
func TestLocalLibraryQueueActions(t *testing.T) {
	a := newTestApp()
	a.settings = defaultSettings()
	current := netease.Track{LocalID: 1, Name: "当前本地歌曲"}
	local := netease.Track{LocalID: 2, Name: "另一首本地歌曲"}
	const path = "../player/testdata/seek.flac"
	a.local.Replace(localcatalog.Catalog{Songs: []localcatalog.Song{{Track: current, Path: path}, {Track: local, Path: path}}})
	a.pl.Cue([]netease.Track{current, {ID: 2, Name: "在线歌曲"}})
	a.nav.Current = route{Page: pageLocal}
	tt := ui.NewTester(a.view, 1200, 800)
	open := func(name string) {
		t.Helper()
		row, ok := tt.AccessByRole(ui.RoleButton, name)
		if !ok {
			t.Fatalf("no library row for %s", name)
		}
		r := row.Bounds
		tt.RightClickAt(r.X+r.W/2, r.Y+r.H/2)
	}
	disabled := func(item string) {
		t.Helper()
		if !slices.Contains(tt.Menu(), item) || tt.ChooseMenuItem(item) == nil {
			t.Fatalf("%s should be visible and disabled", item)
		}
	}

	open(current.Name)
	disabled("下一首播放")
	disabled("加入队列")
	tt.CloseMenu()
	open(local.Name)
	if err := tt.ChooseMenuItem("加入队列"); err != nil {
		t.Fatal(err)
	}
	open(local.Name)
	if err := tt.ChooseMenuItem("下一首播放"); err != nil {
		t.Fatal(err)
	}
	if next, _ := a.pl.AdjacentTracks(); next == nil || next.LocalID != local.LocalID || len(a.pl.Queue()) != 3 {
		t.Fatalf("play next should move the original local entry: %+v", a.pl.Queue())
	}
	open(local.Name)
	disabled("下一首播放")
	if slices.Contains(tt.Menu(), "加入队列") {
		t.Fatal("the queued song still offers to join the queue")
	}
	if err := tt.ChooseMenuItem("移除队列"); err != nil {
		t.Fatal(err)
	}
	if next, _ := a.pl.AdjacentTracks(); len(a.pl.Queue()) != 2 || next == nil || next.ID != 2 ||
		a.pl.Current().LocalID != current.LocalID || a.local.Song(local.LocalID).Path != path {
		t.Fatalf("removing the queued song changed playback or the library: %+v", a.pl.Queue())
	}
	open(local.Name)
	if err := tt.ChooseMenuItem("加入队列"); err != nil {
		t.Fatal(err)
	}
	if len(a.pl.Queue()) != 3 || a.pl.QueuePosition(local.Key()) != 2 {
		t.Fatalf("the removed song could not be requeued: %+v", a.pl.Queue())
	}
}

// The local library can remove a played song that is still listed before the current song.
func TestLocalLibraryRemovesPlayedQueueEntry(t *testing.T) {
	a := newTestApp()
	a.settings = defaultSettings()
	a.nav.Current = route{Page: pageLocal}
	played := netease.Track{LocalID: 1, Name: "已播本地歌曲"}
	current := netease.Track{ID: 1, Name: "当前在线歌曲"}
	next := netease.Track{ID: 2, Name: "下一首在线歌曲"}
	const path = "../player/testdata/seek.flac"
	a.local.Replace(localcatalog.Catalog{Songs: []localcatalog.Song{{Track: played, Path: path}}})
	a.pl.SuspendCache()
	a.pl.Restore(player.Session{
		Queue: []netease.Track{current, next, played}, Order: []int{2, 0, 1}, At: 1, Shuffle: true,
	})
	a.pl.Hold(17 * time.Second)
	tt := ui.NewTester(a.view, 1200, 800)
	if err := tt.RightClick(played.Name); err != nil {
		t.Fatal(err)
	}
	if err := tt.ChooseMenuItem("移除队列"); err != nil {
		t.Fatal(err)
	}
	order, at := a.pl.PlayOrder()
	if !slices.Equal(order, []int{0, 1}) || at != 0 || a.pl.Current().Key() != current.Key() ||
		!a.pl.Playing() || a.pl.Position() != 17*time.Second || a.local.Song(played.LocalID).Path != path {
		t.Fatalf("removal changed playback or the library: order %v, current %d at %v", order, at, a.pl.Position())
	}
}

// A file chosen before startup finishes plays after restoring the old queue,
// and a corrupt file leaves the playing song and the library alone.
func TestLocalTemporaryOpenAfterRestore(t *testing.T) {
	isolateUserData(t)
	a := newTestApp()
	a.pl = player.New(a.post)
	a.initLocalMusic()
	a.pl.SetVolume(0)
	posts := testPosts(a)
	t.Cleanup(func() { a.pl.SuspendCache() })

	original, err := filepath.Abs("../player/testdata/seek.flac")
	if err != nil {
		t.Fatal(err)
	}
	song, ok := localcatalog.Inspect(original)
	if !ok {
		t.Fatal("cannot open the original audio fixture")
	}
	song.Track.LocalID = 1
	a.local.Replace(localcatalog.Catalog{Songs: []localcatalog.Song{song}})
	path, err := sessionPath()
	if err != nil || !writeSessionJSON(path, player.Session{Queue: []netease.Track{song.Track}, Order: []int{0}}) {
		t.Fatal("cannot save the original queue", err)
	}

	dir := t.TempDir()
	b, err := os.ReadFile(original)
	if err != nil {
		t.Fatal(err)
	}
	file := filepath.Join(dir, "临时试听.FLAC")
	if err := os.WriteFile(file, b, 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "临时试听.lrc"), []byte("[00:00.10]试听歌词"), 0600); err != nil {
		t.Fatal(err)
	}
	old := openLocalDialog
	openLocalDialog = func(o mygo.OpenDialogOptions) ([]string, error) {
		if o.Directory || o.Multiple {
			t.Fatal("temporary playback should pick one file")
		}
		return []string{file}, nil
	}
	t.Cleanup(func() { openLocalDialog = old })
	a.chooseTemporaryAudio()
	if a.pl.Current() != nil {
		t.Fatal("opened before session restore")
	}
	a.restoreSession()
	for a.pl.Current() == nil || !a.pl.Current().Temporary || !a.pl.Ready() || a.pl.MetadataLoading() {
		wait(t, posts, "temporary audio and lyrics")()
	}
	cur := a.pl.Current()
	words, loaded := a.pl.Lyrics()
	if !loaded || len(words) != 1 || words[0].Text != "试听歌词" || cur.LocalID == song.Track.LocalID ||
		len(a.pl.Queue()) != 2 || len(a.local.Catalog.Songs) != 1 || len(a.local.Catalog.Folders) != 0 {
		t.Fatalf("unexpected temporary playback or catalog: %+v, %+v", cur, a.local.Catalog)
	}
	a.saveSession()
	a.sessionSave.wait()
	if s, ok := readSession(path); !ok || len(s.Queue) != 1 || s.Queue[0].LocalID != song.Track.LocalID {
		t.Fatalf("temporary file leaked into the saved queue: %+v", s)
	}
	if _, err := os.Stat(a.local.Path); !os.IsNotExist(err) {
		t.Fatalf("temporary playback wrote to the library: %v", err)
	}

	bad := filepath.Join(dir, "corrupt.mp3")
	if err := os.WriteFile(bad, []byte("not audio"), 0600); err != nil {
		t.Fatal(err)
	}
	a.openTemporaryAudio(bad)
	for a.toast == "" {
		wait(t, posts, "invalid audio result")()
	}
	if a.pl.Current() != cur || !a.pl.Playing() || len(a.pl.Queue()) != 2 {
		t.Fatal("an invalid file interrupted playback")
	}
}

// Managed folders scan recursively, deduplicate overlaps, and retain a song's queue ID after relinking it.
func TestLocalPageManagesFoldersAndRelinksSongs(t *testing.T) {
	isolateUserData(t)
	a := newTestApp()
	a.initLocalMusic()
	posts := testPosts(a)
	dir := t.TempDir()
	music := filepath.Join(dir, "Music")
	nested := filepath.Join(music, "Subfolder")
	if err := os.MkdirAll(nested, 0700); err != nil {
		t.Fatal(err)
	}
	file := func(folder, name, destination string) string {
		t.Helper()
		b, err := os.ReadFile(filepath.Join("../player/testdata", name))
		if err != nil {
			t.Fatal(err)
		}
		path := filepath.Join(folder, destination)
		if err := os.WriteFile(path, b, 0600); err != nil {
			t.Fatal(err)
		}
		return path
	}
	mp3 := file(music, "seek.mp3", "First.mp3")
	flac := file(nested, "seek.flac", "Second.FLAC")
	for _, name := range []string{"seek.ogg", "seek.m4a", "seek.aac"} {
		file(nested, name, name)
	}
	moved := filepath.Join(dir, "relocated.mp3")
	old := openLocalDialog
	openLocalDialog = func(o mygo.OpenDialogOptions) ([]string, error) {
		if o.Directory && o.Multiple {
			return []string{music, nested, music}, nil
		}
		return []string{moved}, nil
	}
	t.Cleanup(func() { openLocalDialog = old })
	a.nav.Current = route{Page: pageLocal}
	a.pl.Cue([]netease.Track{{ID: 31, Name: "Cloud song"}})
	tt := ui.NewTester(a.view, 1200, 800)
	tt.Frame()
	manager := ui.NewTester(a.localFoldersView, 520, 350)
	manager.Frame()
	if err := manager.Click("添加文件夹"); err != nil {
		t.Fatal(err)
	}
	for a.local.Busy {
		wait(t, posts, "local import")()
	}
	if len(a.local.Catalog.Songs) != 5 || len(a.local.Catalog.Folders) != 2 {
		t.Fatal(a.local.Status)
	}
	entry := a.local.Catalog.Songs[0]
	id, name := entry.Track.LocalID, entry.Track.Name
	tt.Frame()
	if err := tt.RightClick(name); err != nil {
		t.Fatal(err)
	}
	if err := tt.ChooseMenuItem("下一首播放"); err != nil {
		t.Fatal(err)
	}
	if q := a.pl.Queue(); len(q) != 2 || q[1].LocalID != id {
		t.Fatal("local song did not enter the cloud queue")
	}
	file(nested, "seek.mp3", "New.mp3")
	if err := tt.Click("刷新曲库"); err != nil {
		t.Fatal(err)
	}
	for a.local.Busy {
		wait(t, posts, "folder refresh")()
	}
	if len(a.local.Catalog.Songs) != 6 || a.local.Song(id).Track.LocalID != id {
		t.Fatal("refresh did not discover the new file or changed an existing ID")
	}
	if err := os.Rename(mp3, moved); err != nil {
		t.Fatal(err)
	}
	a.refreshLocalSongs()
	for a.local.Busy {
		wait(t, posts, "missing file after refresh")()
	}
	if !a.local.Missing[id] {
		t.Fatal("refresh did not flag the removed file")
	}
	tt.Frame()
	if err := tt.Click("重新定位"); err != nil {
		t.Fatal(err)
	}
	for a.local.Busy {
		wait(t, posts, "local relink")()
	}
	if song := a.local.Song(id); filepath.Base(song.Path) != filepath.Base(moved) || a.pl.Queue()[1].LocalID != id {
		t.Fatal("relink lost the track or its queue identity")
	}
	other := newTestApp()
	other.initLocalMusic()
	if other.local.Song(id).Path != a.local.Song(id).Path || len(other.local.Catalog.Folders) != 2 {
		t.Fatal("relink was not persisted")
	}
	manager.Frame()
	if err := manager.Click("移除 " + a.local.Catalog.Folders[0]); err != nil {
		t.Fatal(err)
	}
	for a.local.Busy {
		wait(t, posts, "folder removal")()
	}
	if len(a.local.Catalog.Folders) != 1 || len(a.local.Catalog.Songs) != 6 {
		t.Fatal("removing a parent folder lost overlapping or independently kept songs")
	}
	manager.Frame()
	if err := manager.Click("移除 " + a.local.Catalog.Folders[0]); err != nil {
		t.Fatal(err)
	}
	for a.local.Busy {
		wait(t, posts, "subfolder removal")()
	}
	if len(a.local.Catalog.Songs) != 1 || a.local.Song(id).Path == "" {
		t.Fatal("removing the last folder left its songs or lost the relinked track")
	}
	if _, err := os.Stat(flac); err != nil {
		t.Fatal("removing a folder changed the source files", err)
	}
	tt.Frame()
	// Suspend audio loading to exercise the row's queue selection alone.
	a.pl.SuspendCache()
	if err := tt.Click(a.local.Song(id).Track.Name); err != nil {
		t.Fatal(err)
	}
	if cur := a.pl.Current(); cur == nil || cur.LocalID != id || len(a.pl.Queue()) != 1 {
		t.Fatal("a single click did not select the local song in the shared list")
	}
	a.pl.Cue([]netease.Track{{ID: 31, Name: "Cloud song"}})
	tt.Frame()
	if err := tt.RightClick(a.local.Song(id).Track.Name); err != nil {
		t.Fatal(err)
	}
	if err := tt.ChooseMenuItem("从曲库移除"); err != nil {
		t.Fatal(err)
	}
	for a.local.Busy {
		wait(t, posts, "local removal")()
	}
	if a.local.Song(id).Path != "" {
		t.Fatal("local song remained in the library")
	}
	if _, err := os.Stat(moved); err != nil {
		t.Fatal("source file was removed", err)
	}
}
