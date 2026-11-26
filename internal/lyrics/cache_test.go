/*
验证离线歌词读取、缓存缺失或损坏处理，以及异步刷新发布。
*/

package lyrics

import (
	"encoding/json"
	"errors"
	"sync"
	"testing"
	"time"

	diskcache "netease-music-mygo/internal/cache/disk"
)

// Cached lyrics are available without any successful request to the lyrics service.
func TestOfflineLyricsFromDisk(t *testing.T) {
	store := diskcache.New(t.TempDir(), 1<<20)
	source := Cache{Store: store, Scope: "7", Fetch: func(int64) (string, string, string, error) {
		t.Error("requested cached lyrics")
		return "", "", "", errors.New("offline")
	}}
	store.Put("7", "lyrics/1", diskcache.Encode(Text{Original: "[00:01]Cached", Translation: "translation", Words: "words"}), 0)
	l, tr, w, err := source.Load(1, nil)
	if err != nil || l != "[00:01]Cached" || tr != "translation" || w != "words" {
		t.Fatal("did not restore all lyric formats")
	}
}

// Cache misses fetch all source formats once; failed and malformed reads can
// never become a successful empty cached response.
func TestLyricsCacheMissAndFailure(t *testing.T) {
	store := diskcache.New(t.TempDir(), 1<<20)
	defer store.Close()
	calls := 0
	offline := errors.New("offline")
	source := Cache{Store: store, Scope: "7", Fetch: func(id int64) (string, string, string, error) {
		calls++
		if id == 2 {
			return "partial", "", "", offline
		}
		return "[00:01]Fetched", "translation", "words", nil
	}}
	store.Put("7", "lyrics/1", []byte("bad JSON"), 0)
	for range 2 {
		l, tr, w, err := source.Load(1, nil)
		if err != nil || l != "[00:01]Fetched" || tr != "translation" || w != "words" {
			t.Fatalf("incomplete source: %q %q %q %v", l, tr, w, err)
		}
	}
	if calls != 1 {
		t.Fatalf("cache hit fetched again: %d", calls)
	}
	l, tr, w, err := source.Load(2, nil)
	if !errors.Is(err, offline) || l != "" || tr != "" || w != "" {
		t.Fatal("failed fetch published partial source")
	}
	if b, _ := store.Read("7", "lyrics/2"); b != nil {
		t.Fatal("failed fetch was cached")
	}
}

// Stale lyrics return immediately while the replacement waits for owner-thread
// publication, retaining the request epoch and all source formats on disk.
func TestLyricsCacheRefreshDispatch(t *testing.T) {
	store := diskcache.New(t.TempDir(), 1<<20)
	defer store.Close()
	store.Put("7", "lyrics/1", diskcache.Encode(Text{Original: "[00:01]Old"}), 0)
	_, at := store.Read("7", "lyrics/1")
	release := make(chan struct{})
	unblock := sync.OnceFunc(func() { close(release) })
	defer unblock()
	type update struct {
		epoch uint64
		apply func()
	}
	posts := make(chan update, 1)
	source := Cache{Store: store, Scope: "7", Now: func() time.Time { return at.Add(8 * 24 * time.Hour) },
		Fetch: func(int64) (string, string, string, error) {
			<-release
			return "[00:01]New", "[00:01]Translated", "", nil
		},
		Publish: func(epoch uint64, fn func()) { posts <- update{epoch, fn} },
	}
	published := false
	l, _, _, err := source.Load(1, func(lines []Line, display Display) {
		published = true
		if len(lines) != 1 || lines[0].Text != "New" {
			t.Fatalf("refresh lost parsed lyrics: %+v", lines)
		}
	})
	if err != nil || l != "[00:01]Old" || published {
		t.Fatal("stale read waited or published before dispatch")
	}
	unblock()
	select {
	case post := <-posts:
		if published || post.epoch != store.Epoch() {
			t.Fatal("worker bypassed owner publication or changed epoch")
		}
		post.apply()
	case <-time.After(5 * time.Second):
		t.Fatal("missing refresh publication")
	}
	if !published {
		t.Fatal("accepted update did not publish")
	}
	var kept Text
	b, _ := store.Read("7", "lyrics/1")
	if json.Unmarshal(b, &kept) != nil || kept.Original != "[00:01]New" || kept.Translation != "[00:01]Translated" {
		t.Fatal("refresh was not persisted")
	}
}
