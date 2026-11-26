/*
验证提交前取消或丢弃提交回调时能够回滚并释放暂停的缓存服务。
*/

package settings

import (
	"os"
	"path/filepath"
	"testing"

	diskcache "netease-music-mygo/internal/cache/disk"
	cachemove "netease-music-mygo/internal/cache/move"
	"netease-music-mygo/internal/player"
	"netease-music-mygo/internal/viewmodel/internal/testutil"
)

// Cancelling before the commit callback runs rolls back without publishing a new cache or settings.
func TestCacheSettingsCancelBeforeCommit(t *testing.T) {
	root := t.TempDir()
	source, target := filepath.Join(root, "old"), filepath.Join(root, "new")
	if err := os.MkdirAll(source, 0700); err != nil {
		t.Fatal(err)
	}
	posts := make(chan func(), 16)
	p := player.New(func(func()) {})
	p.Cache = player.NewCache(filepath.Join(source, "songs"), 1024)
	original := p.Cache
	m := new(CacheSettings)
	committed := false
	s := CacheServices{Post: func(f func()) { posts <- f }, Player: p, Commit: func(string) error { committed = true; return nil }, NewCache: func() *player.Cache { t.Error("new cache after cancellation"); return nil }, Measure: func() {}, Changed: func() {}, Quit: func() { t.Error("unexpected quit") }}
	if err := m.Change(CacheChange{Source: cachemove.Layout{Root: source, Songs: filepath.Join(source, "songs"), Browse: filepath.Join(source, "browse-v1"), Artwork: filepath.Join(source, cachemove.ArtworkDir)}, Target: target, Directory: target, Journal: filepath.Join(root, "journal.json")}, s); err != nil {
		t.Fatal(err)
	}
	m.Cancel()
	for m.Moving {
		testutil.Wait(t, posts, "cache cancellation")()
	}
	if committed || p.Cache != original || m.Cancelable() || m.Cancelling {
		t.Fatal("cancelled transaction was published")
	}
}

// A discarded commit must unblock rollback and pending store cleanup without running the UI callback.
func TestCacheSettingsDiscardedCommitReleasesStores(t *testing.T) {
	root := t.TempDir()
	source, target := filepath.Join(root, "old"), filepath.Join(root, "new")
	if err := os.MkdirAll(source, 0700); err != nil {
		t.Fatal(err)
	}
	posts := make(chan func(), 16)
	commits := make(chan [2]func(), 1)
	p := player.New(func(func()) {})
	t.Cleanup(p.Close)
	p.Cache = player.NewCache(filepath.Join(source, "songs"), 1024)
	original := p.Cache
	browse := diskcache.New(filepath.Join(source, "browse-v1"), 1024)
	t.Cleanup(browse.Close)
	m := new(CacheSettings)
	committed := false
	s := CacheServices{
		Post: func(fn func()) { posts <- fn }, PostResource: func(fn, discard func()) { commits <- [2]func(){fn, discard} },
		Player: p, Browse: browse,
		Commit:   func(string) error { committed = true; return nil },
		NewCache: func() *player.Cache { t.Error("published discarded commit"); return nil },
		Measure:  func() {}, Changed: func() {}, Quit: func() { t.Error("unexpected quit") },
	}
	plan := CacheChange{Source: cachemove.Layout{Root: source, Songs: filepath.Join(source, "songs"), Browse: filepath.Join(source, "browse-v1"), Artwork: filepath.Join(source, cachemove.ArtworkDir)}, Target: target, Directory: target, Journal: filepath.Join(root, "journal.json")}
	if err := m.Change(plan, s); err != nil {
		t.Fatal(err)
	}
	commit := testutil.Wait(t, commits, "cache commit")
	// Clear waits for the suspended store to resume; dispatch is now closed.
	cleaned := make(chan struct{})
	browse.Clear("", func() { close(cleaned) })
	m.Cancel()
	commit[1]()
	for m.Moving {
		testutil.Wait(t, posts, "cache rollback")()
	}
	testutil.Wait(t, cleaned, "resumed store cleanup")
	if committed || p.Cache != original || m.Cancelable() || m.Cancelling {
		t.Fatal("discarded commit was published or left resources suspended")
	}
	if _, err := os.Stat(target); !os.IsNotExist(err) {
		t.Fatalf("discarded destination survived rollback: %v", err)
	}
}
