package viewmodel

import (
	"os"
	"path/filepath"
	"testing"

	"netease-music-mygo/internal/cachemove"
	"netease-music-mygo/internal/player"
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
		wait(t, posts, "cache cancellation")()
	}
	if committed || p.Cache != original || m.Cancelable() || m.Cancelling {
		t.Fatal("cancelled transaction was published")
	}
}
