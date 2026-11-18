package app

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"

	"netease-music-mygo/internal/netease"
)

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
	moved := filepath.Join(dir, "relocated.mp3")
	old := openLocalDialog
	openLocalDialog = func(o mygo.OpenDialogOptions) ([]string, error) {
		if o.Directory && o.Multiple {
			return []string{music, nested, music}, nil
		}
		return []string{moved}, nil
	}
	t.Cleanup(func() { openLocalDialog = old })
	a.route = route{page: pageLocal}
	a.pl.Cue([]netease.Track{{ID: 31, Name: "Cloud song"}})
	tt := ui.NewTester(a.view, 1200, 800)
	tt.Frame()
	manager := ui.NewTester(a.localFoldersView, 520, 350)
	manager.Frame()
	if err := manager.Click("添加文件夹"); err != nil {
		t.Fatal(err)
	}
	for a.local.busy {
		wait(t, posts, "local import")()
	}
	if len(a.local.catalog.Songs) != 2 || len(a.local.catalog.Folders) != 2 {
		t.Fatal(a.local.status)
	}
	entry := a.local.catalog.Songs[0]
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
	for a.local.busy {
		wait(t, posts, "folder refresh")()
	}
	if len(a.local.catalog.Songs) != 3 || a.local.song(id).Track.LocalID != id {
		t.Fatal("refresh did not discover the new file or changed an existing ID")
	}
	if err := os.Rename(mp3, moved); err != nil {
		t.Fatal(err)
	}
	a.checkLocalFiles()
	for !a.local.missing[id] {
		wait(t, posts, "file status")()
	}
	tt.Frame()
	if err := tt.Click("重新定位"); err != nil {
		t.Fatal(err)
	}
	for a.local.busy {
		wait(t, posts, "local relink")()
	}
	if song := a.local.song(id); filepath.Base(song.Path) != filepath.Base(moved) || a.pl.Queue()[1].LocalID != id {
		t.Fatal("relink lost the track or its queue identity")
	}
	other := newTestApp()
	other.initLocalMusic()
	if other.local.song(id).Path != a.local.song(id).Path || len(other.local.catalog.Folders) != 2 {
		t.Fatal("relink was not persisted")
	}
	manager.Frame()
	if err := manager.Click("移除 " + a.local.catalog.Folders[0]); err != nil {
		t.Fatal(err)
	}
	for a.local.busy {
		wait(t, posts, "folder removal")()
	}
	if len(a.local.catalog.Folders) != 1 || len(a.local.catalog.Songs) != 3 {
		t.Fatal("removing a parent folder lost overlapping or independently kept songs")
	}
	manager.Frame()
	if err := manager.Click("移除 " + a.local.catalog.Folders[0]); err != nil {
		t.Fatal(err)
	}
	for a.local.busy {
		wait(t, posts, "subfolder removal")()
	}
	if len(a.local.catalog.Songs) != 1 || a.local.song(id).Path == "" {
		t.Fatal("removing the last folder left its songs or lost the relinked track")
	}
	if _, err := os.Stat(flac); err != nil {
		t.Fatal("removing a folder changed the source files", err)
	}
	tt.Frame()
	// Suspend audio loading to exercise the row's queue selection alone.
	a.pl.SuspendCache()
	if err := tt.Click(a.local.song(id).Track.Name); err != nil {
		t.Fatal(err)
	}
	if cur := a.pl.Current(); cur == nil || cur.LocalID != id || len(a.pl.Queue()) != 1 {
		t.Fatal("a single click did not select the local song in the shared list")
	}
	a.pl.Cue([]netease.Track{{ID: 31, Name: "Cloud song"}})
	tt.Frame()
	if err := tt.RightClick(a.local.song(id).Track.Name); err != nil {
		t.Fatal(err)
	}
	if err := tt.ChooseMenuItem("从曲库移除"); err != nil {
		t.Fatal(err)
	}
	for a.local.busy {
		wait(t, posts, "local removal")()
	}
	if a.local.song(id).Path != "" {
		t.Fatal("local song remained in the library")
	}
	if _, err := os.Stat(moved); err != nil {
		t.Fatal("source file was removed", err)
	}
}
