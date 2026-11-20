package app

import (
	"errors"
	"image/png"
	"os"
	"path/filepath"
	"slices"
	"testing"
	"time"

	"github.com/egoist/mygo/ui"

	"netease-music-mygo/internal/netease"
)

// editTestApp is signed in, with liked songs, two playlists of their own
// and one saved, and shows their playlist 10.
func editTestApp() *app {
	a := newTestApp()
	a.settings = defaultSettings()
	a.profile = &netease.Profile{UserID: 1, Nickname: "小明"}
	a.mine = []netease.Playlist{
		{ID: 5, Name: "小明喜欢的音乐", SpecialType: 5},
		{ID: 10, Name: "通勤"},
		{ID: 11, Name: "睡前"},
	}
	a.saved = []netease.Playlist{{ID: 20, Name: "飙升榜"}}
	a.playlists[10] = &netease.Playlist{ID: 10, Name: "通勤", Tracks: []netease.Track{
		{ID: 1, Name: "一张相片", Artists: []netease.Artist{{Name: "林宥嘉"}}, Duration: 200000},
		{ID: 2, Name: "宝船", Artists: []netease.Artist{{Name: "DOUDOU"}}, Duration: 200000},
	}}
	a.route = route{page: pagePlaylist, id: 10}
	return a
}

// Choosing one of the listener's playlists from a song's menu adds the
// song to it, and the menu offers neither the liked songs nor the
// playlist the song is listed in.
func TestAddSongToPlaylist(t *testing.T) {
	type add struct{ pid, song int64 }
	calls := make(chan add, 1)
	addToPlaylist = func(pid int64, songs ...int64) error { calls <- add{pid, songs[0]}; return nil }
	defer func() { addToPlaylist = netease.AddToPlaylist }()

	a := editTestApp()
	tt := ui.NewTester(a.view, 1200, 800)
	tt.Frame()
	if err := tt.RightClick("宝船"); err != nil {
		t.Fatal(err)
	}
	if err := tt.ChooseMenuItem("添加到歌单", "通勤"); err == nil {
		t.Error("offered to add the song to the playlist it is in")
	}
	if err := tt.ChooseMenuItem("添加到歌单", "小明喜欢的音乐"); err == nil {
		t.Error("offered to add the song to the liked songs")
	}
	if err := tt.ChooseMenuItem("添加到歌单", "睡前"); err != nil {
		t.Fatalf("%v; the menu shows %q", err, tt.Menu())
	}
	tt.Frame()
	if got := <-calls; got != (add{11, 2}) {
		t.Errorf("added %+v, want song 2 to playlist 11", got)
	}
}

// Adding a song while its playlist is visible keeps the old rows until
// NetEase supplies the updated list order.
func TestAddSongKeepsVisiblePlaylistDuringRefresh(t *testing.T) {
	a := editTestApp()
	posts := testPosts(a)
	id := a.route.id
	original := a.playlists[id]
	a.touchPlaylist(id)
	added := netease.Track{ID: 3, Name: "新歌曲"}
	next := copyPlaylist(*original)
	next.Tracks = append([]netease.Track{added}, next.Tracks...)

	oldAdd, oldDetail, oldMine := addToPlaylist, playlistDetail, userPlaylists
	t.Cleanup(func() { addToPlaylist, playlistDetail, userPlaylists = oldAdd, oldDetail, oldMine })
	addToPlaylist = func(int64, ...int64) error { return nil }
	fetching, release := make(chan struct{}), make(chan struct{}, 1)
	defer close(release)
	playlistDetail = func(int64) (*netease.Playlist, error) {
		close(fetching)
		<-release
		return &next, nil
	}
	mine, saved := a.mine, a.saved
	userPlaylists = func(int64) ([]netease.Playlist, []netease.Playlist, error) { return mine, saved, nil }

	a.addTo(id, &added)
	refreshPost(t, posts)
	select {
	case <-fetching:
	case <-time.After(3 * time.Second):
		t.Fatal("add did not revalidate the recently loaded playlist")
	}
	if a.playlists[id] != original || a.loadingPl[id] {
		t.Fatal("add blanked the existing playlist while the refresh was pending")
	}
	release <- struct{}{}
	refreshPost(t, posts)
	refreshPost(t, posts)
	if pl := a.playlists[id]; len(pl.Tracks) != 3 || pl.Tracks[0].ID != added.ID {
		t.Fatal("add did not publish the server's updated list order")
	}
}

// Removing a song from the listener's playlist takes its row away at once,
// and asks NetEase to take it out.
func TestRemoveSongFromPlaylist(t *testing.T) {
	type del struct{ pid, song int64 }
	calls := make(chan del, 1)
	removeFromPlaylist = func(pid int64, songs ...int64) error { calls <- del{pid, songs[0]}; return nil }
	defer func() { removeFromPlaylist = netease.RemoveFromPlaylist }()

	a := editTestApp()
	// Playing the id: the queue keeps the song.
	a.pl.Cue(a.playlists[10].Tracks)
	tt := ui.NewTester(a.view, 1200, 800)
	tt.Frame()
	if err := tt.RightClick("宝船"); err != nil {
		t.Fatal(err)
	}
	if err := tt.ChooseMenuItem("从歌单中移除"); err != nil {
		t.Fatalf("%v; the menu shows %q", err, tt.Menu())
	}
	tt.Frame()
	if got := <-calls; got != (del{10, 2}) {
		t.Errorf("removed %+v, want song 2 from playlist 10", got)
	}
	if _, ok := tt.Find("宝船"); ok {
		t.Error("the song's row is still listed")
	}
	if q := a.pl.Queue(); len(q) != 2 || q[1].ID != 2 {
		t.Errorf("removing the song changed the queue playing: %v", q)
	}
}

// A failed removal restores its song in order without restoring another removal that already succeeded.
func TestConcurrentPlaylistRemovalRollback(t *testing.T) {
	a := editTestApp()
	posts := testPosts(a)
	pid := a.route.id
	original := a.playlists[pid]
	original.Tracks = append(original.Tracks, netease.Track{ID: 3})
	original.TrackIDs = []netease.TrackID{{ID: 1}, {ID: 2}, {ID: 3}}
	original.TrackCount = 3
	a.pl.Cue(original.Tracks)

	oldRemove, oldDetail, oldMine := removeFromPlaylist, playlistDetail, userPlaylists
	t.Cleanup(func() { removeFromPlaylist, playlistDetail, userPlaylists = oldRemove, oldDetail, oldMine })
	results := map[int64]chan error{1: make(chan error, 1), 2: make(chan error, 1)}
	for _, result := range results {
		defer close(result)
	}
	offline := errors.New("playlist unavailable")
	removeFromPlaylist = func(_ int64, songs ...int64) error { return <-results[songs[0]] }
	playlistDetail = func(int64) (*netease.Playlist, error) { return nil, offline }
	userPlaylists = func(int64) ([]netease.Playlist, []netease.Playlist, error) { return nil, nil, offline }

	a.removeFrom(pid, &original.Tracks[0])
	a.removeFrom(pid, &original.Tracks[1])
	if pl := a.playlists[pid]; len(pl.Tracks) != 1 || pl.Tracks[0].ID != 3 {
		t.Fatal("both rows should be hidden while their requests are pending")
	}
	results[2] <- nil
	refreshPost(t, posts) // successful removal
	refreshPost(t, posts) // sidebar refresh
	results[1] <- offline
	refreshPost(t, posts) // failed removal

	// The failed removal must be reflected before a server refresh can land.
	pl := a.playlists[pid]
	want := []netease.TrackID{{ID: 1}, {ID: 3}}
	if !slices.Equal(pl.TrackIDs, want) || pl.TrackCount != 2 ||
		len(pl.Tracks) != 2 || pl.Tracks[0].ID != 1 || pl.Tracks[1].ID != 3 {
		t.Errorf("rollback produced tracks %v, IDs %v, count %d", pl.Tracks, pl.TrackIDs, pl.TrackCount)
	}
	a.retryLibraryRefresh()
	refreshPost(t, posts)
	if a.playlists[pid] != pl || len(a.pl.Queue()) != 3 {
		t.Fatal("a failed refresh changed the playlist or its playback queue")
	}
}

// A removal made during an older lookup keeps its local edit and queues
// a fresh lookup even if the user has already left the playlist.
func TestRemoveSongRefreshesOffscreenPlaylist(t *testing.T) {
	a := editTestApp()
	posts := testPosts(a)
	id := a.route.id
	original := a.playlists[id]
	original.TrackCount = 2
	original.TrackIDs = []netease.TrackID{{ID: 1}, {ID: 2}}
	a.route = route{page: pageHome}
	a.pl.Cue(original.Tracks)
	a.pl.Hold(10 * time.Second)
	revision := a.pl.QueueRevision()

	oldRemove, oldDetail, oldMine := removeFromPlaylist, playlistDetail, userPlaylists
	t.Cleanup(func() { removeFromPlaylist, playlistDetail, userPlaylists = oldRemove, oldDetail, oldMine })
	removeFromPlaylist = func(int64, ...int64) error { return nil }
	requests := make(chan int64, 2)
	replies := make(chan *netease.Playlist, 2)
	defer close(replies)
	playlistDetail = func(pid int64) (*netease.Playlist, error) {
		requests <- pid
		return <-replies, nil
	}
	mine, saved := a.mine, a.saved
	userPlaylists = func(int64) ([]netease.Playlist, []netease.Playlist, error) { return mine, saved, nil }

	a.loadPlaylist(id, false)
	wait(t, requests, "the earlier playlist lookup")
	a.removeFrom(id, &original.Tracks[0])
	refreshPost(t, posts) // the removal
	refreshPost(t, posts) // the sidebar lookup
	reduced := a.playlists[id]
	if reduced.TrackCount != 1 || len(reduced.Tracks) != 1 || len(reduced.TrackIDs) != 1 || reduced.TrackIDs[0].ID != 2 {
		t.Fatal("removal did not update the cached song and ID lists")
	}
	replies <- original
	refreshPost(t, posts)
	if a.playlists[id] != reduced {
		t.Fatal("an older lookup restored the removed song")
	}
	a.retryLibraryRefresh()
	if pid := wait(t, requests, "the replacement lookup"); pid != id {
		t.Fatalf("refreshed playlist %d, want %d", pid, id)
	}
	server := copyPlaylist(*reduced)
	server.PlayCount = 10
	replies <- &server
	refreshPost(t, posts)
	if a.playlists[id].PlayCount != 10 || a.pl.QueueRevision() != revision || !a.pl.Playing() || a.pl.Position() != 10*time.Second {
		t.Fatal("the latest playlist did not publish or changed playback")
	}
}

// A playlist the listener saved, not made, offers no removing.
func TestNoRemovingFromSavedPlaylist(t *testing.T) {
	a := editTestApp()
	a.playlists[20] = &netease.Playlist{ID: 20, Name: "飙升榜", Tracks: a.playlists[10].Tracks}
	a.route = route{page: pagePlaylist, id: 20}
	tt := ui.NewTester(a.view, 1200, 800)
	tt.Frame()
	if err := tt.RightClick("宝船"); err != nil {
		t.Fatal(err)
	}
	if err := tt.ChooseMenuItem("从歌单中移除"); err == nil {
		t.Error("offered to remove a song from a saved playlist")
	}
}

// The sidebar's plus asks for a name, and Enter makes the playlist so
// named.
func TestCreatePlaylist(t *testing.T) {
	names := make(chan string, 1)
	createPlaylist = func(name string) (*netease.Playlist, error) {
		names <- name
		return &netease.Playlist{ID: 12, Name: name}, nil
	}
	defer func() { createPlaylist = netease.CreatePlaylist }()

	a := editTestApp()
	tt := ui.NewTester(a.view, 1200, 800)
	tt.Frame()
	if err := tt.Click("新建歌单"); err != nil {
		t.Fatal(err)
	}
	tt.Advance(300 * time.Millisecond)
	tt.Frame()
	tt.Type("  周末  ")
	tt.Key(0, ui.KeyEnter)
	tt.Frame()
	if got := <-names; got != "周末" {
		t.Errorf("made a playlist named %q, want 周末", got)
	}
}

// Choosing 新建歌单… from a song's menu makes the playlist, then adds the
// song to it.
func TestCreatePlaylistForSong(t *testing.T) {
	a := editTestApp()
	tt := ui.NewTester(a.view, 1200, 800)
	tt.Frame()
	if err := tt.RightClick("宝船"); err != nil {
		t.Fatal(err)
	}
	if err := tt.ChooseMenuItem("添加到歌单", "新建歌单…"); err != nil {
		t.Fatalf("%v; the menu shows %q", err, tt.Menu())
	}
	tt.Frame()
	if !a.naming.open || a.naming.add == nil || a.naming.add.ID != 2 {
		t.Fatalf("the dialog is %+v, want it open to add song 2", a.naming)
	}
}

// Renaming a playlist from the sidebar's menu shows its new name at once,
// and asks NetEase to rename it.
func TestRenamePlaylist(t *testing.T) {
	type rename struct {
		id   int64
		name string
	}
	calls := make(chan rename, 1)
	renamePlaylist = func(id int64, name string) error { calls <- rename{id, name}; return nil }
	defer func() { renamePlaylist = netease.RenamePlaylist }()

	a := editTestApp()
	a.route = route{page: pageHome}
	a.homeLoading = true
	tt := ui.NewTester(a.view, 1200, 800)
	tt.Frame()
	if err := tt.RightClick("睡前"); err != nil {
		t.Fatal(err)
	}
	if err := tt.ChooseMenuItem("重命名…"); err != nil {
		t.Fatalf("%v; the menu shows %q", err, tt.Menu())
	}
	tt.Advance(300 * time.Millisecond)
	tt.Frame()
	tt.Key(ui.Cmd, ui.KeyA)
	tt.Type("助眠")
	if err := tt.Click("保存"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	if got := <-calls; got != (rename{11, "助眠"}) {
		t.Errorf("renamed %+v, want playlist 11 to 助眠", got)
	}
	if a.mine[2].Name != "助眠" {
		t.Errorf("the sidebar names it %q", a.mine[2].Name)
	}
	if _, ok := tt.Find("助眠"); !ok {
		t.Error("the new name does not show")
	}
}

// A failed rename from an earlier account epoch must not restore its old
// name over a different account's playlist with the same ID.
func TestRenameFailureFromPreviousAccount(t *testing.T) {
	a := editTestApp()
	posts := testPosts(a)
	reply := make(chan error, 1)
	oldRename := renamePlaylist
	t.Cleanup(func() { renamePlaylist = oldRename })
	renamePlaylist = func(int64, string) error { return <-reply }

	a.naming = nameDialog{open: true, rename: 10, name: "A new"}
	a.submitName()
	a.profile = &netease.Profile{UserID: 2}
	a.homeGen++
	a.resetLibraryFreshness()
	a.mine[1].Name = "B name"
	a.playlists[10].Name = "B name"
	reply <- errors.New("rename failed")
	refreshPost(t, posts)
	if a.mine[1].Name != "B name" || a.playlists[10].Name != "B name" {
		t.Fatalf("previous account's failed rename restored %q and %q", a.mine[1].Name, a.playlists[10].Name)
	}
}

// The liked songs can't be renamed: they offer no menu.
func TestNoRenamingLikedSongs(t *testing.T) {
	a := editTestApp()
	a.route = route{page: pageHome}
	a.homeLoading = true
	tt := ui.NewTester(a.view, 1200, 800)
	tt.Frame()
	if err := tt.RightClick("小明喜欢的音乐"); err != nil {
		t.Fatal(err)
	}
	if err := tt.ChooseMenuItem("重命名…"); err == nil {
		t.Error("offered to rename the liked songs")
	}
}

// Deleting the playlist shown, once confirmed, takes it from the sidebar
// and goes back to the page before, with no way forward to it.
func TestDeletePlaylist(t *testing.T) {
	ids := make(chan int64, 1)
	deletePlaylist = func(id int64) error { ids <- id; return nil }
	defer func() { deletePlaylist = netease.DeletePlaylist }()

	a := editTestApp()
	a.homeLoading = true
	a.route = route{page: pageCharts}
	a.open(route{page: pagePlaylist, id: 10})
	tt := ui.NewTester(a.view, 1200, 800)
	tt.Frame()
	if err := tt.Click("更多"); err != nil {
		t.Fatal(err)
	}
	if err := tt.ChooseMenuItem("删除歌单…"); err != nil {
		t.Fatalf("%v; the menu shows %q", err, tt.Menu())
	}
	tt.Advance(300 * time.Millisecond)
	tt.Frame()
	select {
	case <-ids:
		t.Fatal("deleted before it was confirmed")
	default:
	}
	if err := tt.Click("删除"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	if got := <-ids; got != 10 {
		t.Errorf("deleted playlist %d, want 10", got)
	}
	if a.editable(10) != nil {
		t.Error("the sidebar still lists it")
	}
	if a.route.page != pageCharts || len(a.history) != 0 || len(a.forward) != 0 {
		t.Errorf("on %+v, history %v, forward %v; want the charts with nowhere to go", a.route, a.history, a.forward)
	}
}

// Cancelling the confirmation deletes nothing.
func TestCancelDeletingPlaylist(t *testing.T) {
	deletePlaylist = func(int64) error { t.Error("deleted the playlist"); return nil }
	defer func() { deletePlaylist = netease.DeletePlaylist }()

	a := editTestApp()
	a.route = route{page: pageHome}
	a.homeLoading = true
	tt := ui.NewTester(a.view, 1200, 800)
	tt.Frame()
	if err := tt.RightClick("睡前"); err != nil {
		t.Fatal(err)
	}
	if err := tt.ChooseMenuItem("删除歌单…"); err != nil {
		t.Fatalf("%v; the menu shows %q", err, tt.Menu())
	}
	tt.Advance(300 * time.Millisecond)
	tt.Frame()
	if err := tt.Click("取消"); err != nil {
		t.Fatal(err)
	}
	tt.Advance(300 * time.Millisecond)
	tt.Frame()
	if a.editable(11) == nil {
		t.Error("the playlist left the sidebar")
	}
}

// With NM_FRAMES set, saves the listener's playlist with its sidebar, and
// the dialogs that name and delete playlists, to look at.
func TestPlaylistEditFrames(t *testing.T) {
	dir := os.Getenv("NM_FRAMES")
	if dir == "" {
		t.Skip()
	}
	a := editTestApp()
	tt := ui.NewTester(a.view, 1200, 800)
	save := func(name string) {
		tt.Advance(400 * time.Millisecond)
		tt.Frame()
		f, _ := os.Create(filepath.Join(dir, name+".png"))
		defer f.Close()
		png.Encode(f, tt.Image())
	}
	for _, dark := range []bool{false, true} {
		suffix := "-light"
		if dark {
			suffix = "-dark"
		}
		tt.SetDark(dark)
		save("playlist" + suffix)
		a.newPlaylist(nil)
		save("create" + suffix)
		a.naming.open = false
		a.startRename(a.editable(11))
		save("rename" + suffix)
		a.naming.open = false
		a.confirmDelete(a.editable(11))
		save("delete" + suffix)
		a.deleting.open = false
	}
}
