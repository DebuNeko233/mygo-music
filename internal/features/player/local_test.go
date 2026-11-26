/*
验证本地临时播放与原队列衔接、会话恢复及音频跳转和资源所有权。
*/

package player

import (
	"bytes"
	"io"
	"os"
	"path/filepath"
	"slices"
	"testing"
	"time"

	"netease-music-mygo/internal/features/netease"
)

// A one-off bypasses repeat-one and preserves the shuffled order and saved position.
func TestLocalTemporaryContinuesOriginalQueue(t *testing.T) {
	path := filepath.Join("testdata", "seek.flac")
	posts, p := newLoadPlayer(t, nil)
	p.LocalPath = func(int64) string { return path }
	tracks := []netease.Track{{LocalID: 1}, {LocalID: 2}, {LocalID: 3}}
	order := []int{2, 0, 1}
	p.Restore(Session{Queue: slices.Clone(tracks), Order: slices.Clone(order), Shuffle: true, Repeat: RepeatOne, At: 1, Position: 1500})
	audio, err := PrepareLocalAudio(path)
	if err != nil {
		t.Fatal(err)
	}
	p.PlayTemporary(netease.Track{LocalID: 4, Duration: audio.Duration.Milliseconds()}, audio)
	pump(t, posts, "temporary audio", func() bool { return p.Ready() })
	s, ok := p.Session()
	if !ok || s.At != 1 || s.Position != 1500 || !slices.Equal(s.Order, order) || len(s.Queue) != 3 {
		t.Fatalf("saved the temporary track or lost the original queue: %+v", s)
	}
	p.ended()
	pump(t, posts, "original next song", func() bool { return p.Ready() && p.Current().LocalID == 2 })
	if got, at := p.PlayOrder(); !slices.Equal(got, order) || at != 2 || p.Repeat() != RepeatOne || !p.Playing() {
		t.Fatalf("continuation changed the original order: %v at %d", got, at)
	}
	for i, song := range p.Queue() {
		if song.LocalID != tracks[i].LocalID || song.Temporary {
			t.Fatalf("temporary playback changed the queue: %+v", p.Queue())
		}
	}
}

// Finishing a temporary file with no next song pauses the original track where it was.
func TestLocalTemporaryAtQueueEnd(t *testing.T) {
	path := filepath.Join("testdata", "seek.mp3")
	posts, p := newLoadPlayer(t, nil)
	p.LocalPath = func(int64) string { return path }
	p.Restore(Session{Queue: []netease.Track{{LocalID: 1}}, Order: []int{0}, Position: 1000})
	audio, err := PrepareLocalAudio(path)
	if err != nil {
		t.Fatal(err)
	}
	p.PlayTemporary(netease.Track{LocalID: 2, Duration: audio.Duration.Milliseconds()}, audio)
	pump(t, posts, "temporary audio", func() bool { return p.Ready() })
	p.ended()
	pump(t, posts, "previous track paused", func() bool { return p.Ready() })
	if p.Playing() || len(p.Queue()) != 1 || p.Current().LocalID != 1 || p.Position() < 950*time.Millisecond {
		t.Fatalf("returned to %+v at %v, playing %v", p.Current(), p.Position(), p.Playing())
	}
}

// Opening a queued file plays its existing entry and then returns to the original next song.
func TestOpenQueuedLocalFileReusesEntry(t *testing.T) {
	path := filepath.Join("testdata", "seek.flac")
	posts, p := newLoadPlayer(t, nil)
	p.LocalPath = func(int64) string { return path }
	track := netease.Track{LocalID: 3}
	p.Restore(Session{Queue: []netease.Track{{LocalID: 1}, {LocalID: 2}, track}, Order: []int{0, 1, 2}})
	audio, err := PrepareLocalAudio(path)
	if err != nil {
		t.Fatal(err)
	}
	p.PlayTemporary(track, audio)
	pump(t, posts, "queued file", func() bool { return p.Ready() })
	if !slices.Equal(p.order, []int{0, 2, 1}) || p.at != 1 || p.Current().LocalID != 3 || p.hasTemporary() {
		t.Fatalf("opened file did not reuse its queue entry: %v at %d, queue %+v", p.order, p.at, p.queue)
	}
	p.Next()
	pump(t, posts, "original next song", func() bool { return p.Ready() && p.Current().LocalID == 2 })
}

// Restoring and navigating a mixed queue offline reads local lyrics and cached cloud audio.
func TestLocalMixedQueueRestoresOffline(t *testing.T) {
	b := song(t)
	path := filepath.Join(t.TempDir(), "local.mp3")
	if err := os.WriteFile(path, b, 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path[:len(path)-4]+".lrc", []byte("[00:01.00]本地歌词"), 0600); err != nil {
		t.Fatal(err)
	}
	cache := NewCache(t.TempDir(), 1<<30)
	part := filepath.Join(cache.dir, "cloud.part")
	if err := os.WriteFile(part, b, 0600); err != nil {
		t.Fatal(err)
	}
	if _, ok := cache.keep(1, "exhigh", &netease.Stream{Type: "mp3", Level: "exhigh"}, part, int64(len(b))); !ok {
		t.Fatal("could not cache the cloud fixture")
	}
	netease.SetOffline(true)
	t.Cleanup(func() { netease.SetOffline(false) })
	posts, p := newLoadPlayer(t, cache)
	p.LocalPath = func(int64) string { return path }
	p.FetchLyrics = func(int64) (string, string, string, error) { return "", "", "", nil }
	local := netease.Track{LocalID: 1, Name: "Local", Duration: 5000}
	cloud := netease.Track{ID: 1, Name: "Cloud", Duration: 5000}
	p.Restore(Session{Queue: []netease.Track{cloud, local}, Order: []int{0, 1}, At: 1, Position: 1500})
	pump(t, posts, "local restore", func() bool { return p.Ready() && !p.MetadataLoading() })
	if p.Playing() || p.Current().Key() != local.Key() || p.Position().Milliseconds() < 1450 ||
		len(p.lyrics) != 1 || p.lyrics[0].Text != "本地歌词" {
		t.Fatalf("restored %+v at %v, lyrics %+v", p.Current(), p.Position(), p.lyrics)
	}
	p.Enqueue(cloud, true)
	if order, at := p.PlayOrder(); len(order) != 3 || p.Queue()[order[at+1]].ID != cloud.ID {
		t.Fatal("did not insert the song after the current entry")
	}
	p.Next()
	pump(t, posts, "cached cloud song", func() bool { return p.Ready() && p.Current().ID == cloud.ID })
	p.Jump(1)
	pump(t, posts, "local song playing again", func() bool { return p.Ready() && p.Current().IsLocal() })
}

// Restoring an old queue removes duplicates on both sides of the current song and preserves its checkpoint.
func TestRestoreDeduplicatesLocalTracks(t *testing.T) {
	p := &Player{}
	p.SuspendCache()
	p.Restore(Session{
		Queue: []netease.Track{
			{LocalID: 7, Name: "later duplicate"},
			{LocalID: 7, Name: "played before requeue"},
			{LocalID: 8, Name: "current"},
			{ID: 7, Name: "cloud"},
			{LocalID: 7, Name: "first upcoming"},
			{LocalID: 8, Name: "duplicate of current"},
			{LocalID: 7, Name: "third upcoming"},
			{LocalID: 8, Name: "played copy of current"},
			{LocalID: 9, Name: "most recent history"},
			{LocalID: 9, Name: "older history"},
		},
		Order: []int{9, 1, 7, 8, 2, 4, 3, 0, 5, 6}, At: 4, Shuffle: true, Position: 1500,
	})
	order, at := p.PlayOrder()
	var names []string
	for _, index := range order {
		names = append(names, p.Queue()[index].Name)
	}
	if !slices.Equal(names, []string{"most recent history", "current", "first upcoming", "cloud"}) ||
		len(p.Queue()) != 4 || at != 1 || p.Current().Name != "current" || !p.Shuffle() ||
		p.Playing() || p.Position() != 1500*time.Millisecond {
		t.Fatalf("restored queue %v, at %d, position %v, current %+v", names, at, p.Position(), p.Current())
	}
}

// A local FLAC decodes and seeks with the same sample alignment as a complete stream.
func TestLocalFLACSeek(t *testing.T) {
	b, err := os.ReadFile("testdata/seek.flac")
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), "local.flac")
	if err := os.WriteFile(path, b, 0600); err != nil {
		t.Fatal(err)
	}
	st, s, err := openLocal(path)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	if st.Type != "flac" {
		t.Fatalf("opened as %s", st.Type)
	}
	ref := decodeAll(t, b)
	const pos = 123457
	if err := s.SetPosition(pos); err != nil {
		t.Fatal(err)
	}
	buf := make([]float32, 4096)
	n, err := s.Read(buf)
	if n == 0 || err != nil || maxDiff(buf[:n], ref[pos*2:]) != 0 {
		t.Fatalf("local FLAC seek did not match the source: %d, %v", n, err)
	}
	if err := s.SetPosition(s.Len()); err != nil {
		t.Fatal(err)
	}
	if n, err := s.Read(buf); n != 0 || err != io.EOF {
		t.Fatalf("local FLAC at the end: %d, %v", n, err)
	}
}

// MP3 seeking uses indexed sample positions, and cache cleanup leaves user files alone.
func TestLocalMP3SeekAndSourceOwnership(t *testing.T) {
	b := song(t)
	path := filepath.Join(t.TempDir(), "sample.mp3")
	if err := os.WriteFile(path, b, 0600); err != nil {
		t.Fatal(err)
	}
	_, s, err := openLocal(path)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	ref := decodeAll(t, b)
	if s.Len()*2 != int64(len(ref)) {
		t.Fatalf("indexed %d samples, decoded %d", s.Len()*2, len(ref))
	}
	buf := make([]float32, 4096)
	for _, pos := range []int64{134567, 23451, 0} {
		if err := s.SetPosition(pos); err != nil {
			t.Fatal(err)
		}
		n, err := s.Read(buf)
		if n == 0 || err != nil || maxDiff(buf[:n], ref[pos*2:]) > 1e-4 {
			t.Fatalf("seek to %d did not decode the expected samples: %d, %v", pos, n, err)
		}
	}
	dl, _ := s.source()
	dl.discard()
	s.Close()
	if after, err := os.ReadFile(path); err != nil || !bytes.Equal(after, b) {
		t.Fatal("cache cleanup changed the local source file", err)
	}
}
