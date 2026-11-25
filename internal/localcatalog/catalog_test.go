/*
验证本地扫描的身份与元数据复用、格式支持及歌词持久化兼容性。
*/

package localcatalog

import (
	"encoding/binary"
	"os"
	"path/filepath"
	"slices"
	"testing"

	"netease-music-mygo/internal/lyrics"
	"netease-music-mygo/internal/netease"
	"netease-music-mygo/internal/userdata"
)

// An unchanged file is not reopened or assigned another ID; overlapping scans and relinks keep stable identities.
func TestScanKeepsUnchangedMetadataAndIdentity(t *testing.T) {
	dir := t.TempDir()
	raw, err := os.ReadFile("../player/testdata/seek.mp3")
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, "song.mp3")
	if err := os.WriteFile(path, raw, 0600); err != nil {
		t.Fatal(err)
	}
	c := Catalog{Folders: []string{dir, dir}}
	if changed, _ := Scan(&c, map[string]struct{}{}); !changed || len(c.Songs) != 1 || c.Songs[0].Track.LocalID != 1 {
		t.Fatal("overlapping folders duplicated the file or its identity")
	}
	original := c.Songs[0]
	st, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	// Equal size/stamp is the cache key: deliberately undecodable bytes prove the file wasn't decoded again.
	if err := os.WriteFile(path, make([]byte, len(raw)), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.Chtimes(path, st.ModTime(), st.ModTime()); err != nil {
		t.Fatal(err)
	}
	if changed, _ := Scan(&c, map[string]struct{}{}); changed || c.Songs[0].Track.Name != original.Track.Name || c.Songs[0].Stamp != original.Stamp {
		t.Fatal("unchanged metadata was unnecessarily rescanned")
	}
	replacement := filepath.Join(t.TempDir(), "relocated.mp3")
	if err := os.WriteFile(replacement, raw, 0600); err != nil {
		t.Fatal(err)
	}
	replacement, err = filepath.EvalSymlinks(replacement)
	if err != nil {
		t.Fatal(err)
	}
	if changed, _ := Relink(&c, map[string]struct{}{}, 1, replacement); !changed || c.Songs[0].Track.LocalID != 1 || c.Songs[0].Scanned {
		t.Fatal("relink lost identity or kept ownership of an unrelated folder")
	}
	RemoveFolder(&c, dir)
	if len(c.Songs) != 1 || c.Songs[0].Path != replacement {
		t.Fatal("removing folders lost an independently relinked song")
	}
	RemoveSong(&c, 1)
	c.Folders = []string{filepath.Dir(replacement)}
	Scan(&c, map[string]struct{}{})
	if len(c.Songs) != 1 || c.Songs[0].Track.LocalID != 2 {
		t.Fatal("a removed song ID was reused")
	}
}

// Import and temporary playback accept the same supported decoder formats, including WAV and MP4 variants.
func TestInspectAndPrepareSupportedAudio(t *testing.T) {
	names := []string{"seek.mp3", "seek.flac", "seek.ogg", "seek.m4a", "seek-alac.m4a", "seek.aac", "seek-he.aac", "seek-he.m4a", "seek-fragmented.m4a", "seek.3gp", "seek.3g2"}
	paths := make([]string, 0, len(names)+1)
	for _, name := range names {
		paths = append(paths, filepath.Join("../player/testdata", name))
	}
	wav := make([]byte, 44+4410*4)
	copy(wav, "RIFF")
	binary.LittleEndian.PutUint32(wav[4:], uint32(len(wav)-8))
	copy(wav[8:], "WAVEfmt ")
	binary.LittleEndian.PutUint32(wav[16:], 16)
	binary.LittleEndian.PutUint16(wav[20:], 1)
	binary.LittleEndian.PutUint16(wav[22:], 2)
	binary.LittleEndian.PutUint32(wav[24:], 44100)
	binary.LittleEndian.PutUint32(wav[28:], 44100*4)
	binary.LittleEndian.PutUint16(wav[32:], 4)
	binary.LittleEndian.PutUint16(wav[34:], 16)
	copy(wav[36:], "data")
	binary.LittleEndian.PutUint32(wav[40:], uint32(len(wav)-44))
	wavPath := filepath.Join(t.TempDir(), "sample.WAV")
	if err := os.WriteFile(wavPath, wav, 0600); err != nil {
		t.Fatal(err)
	}
	paths = append(paths, wavPath)
	for _, path := range paths {
		t.Run(filepath.Base(path), func(t *testing.T) {
			song, ok := Inspect(path)
			if !ok || song.Track.Duration <= 0 {
				t.Fatal("supported file did not import")
			}
			prepared, audio := PrepareTemporary(path)
			if audio == nil {
				t.Fatal("imported file could not use the same player decoder")
			}
			defer audio.Close()
			if prepared.Track.Duration != song.Track.Duration || audio.Duration <= 0 {
				t.Fatal("import and playback disagreed about duration")
			}
		})
	}
}

// Existing catalog/lyric JSON loads unchanged, and malformed catalogs cannot overwrite the user's library.
func TestCatalogAndLyricPersistenceCompatibility(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "local-music.json")
	original := []byte(`{"lastId":9,"songs":[{"track":{"name":"recording","localId":4},"path":"song.flac","stamp":12,"size":34,"scanned":true}],"folders":["Music"]}`)
	if err := os.WriteFile(path, original, 0600); err != nil {
		t.Fatal(err)
	}
	catalog, err := Load(path)
	if err != nil || catalog.LastID != 9 || len(catalog.Songs) != 1 || catalog.Songs[0].Track.LocalID != 4 || !catalog.Songs[0].Scanned {
		t.Fatal("legacy catalog did not restore", err)
	}
	var state State
	state.Replace(catalog)
	if state.Song(4).Path != "song.flac" || len(state.Tracks) != 1 {
		t.Fatal("restored catalog lost its derived index")
	}
	matchPath := LyricFile(path, 4)
	if err := os.MkdirAll(filepath.Dir(matchPath), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(matchPath, []byte(`{"songId":7,"lyrics":{"Original":"original","Translation":"translated","Words":"word timing"}}`), 0600); err != nil {
		t.Fatal(err)
	}
	if l, tr, w := LoadLyrics(matchPath); l != "original" || tr != "translated" || w != "word timing" {
		t.Fatal("legacy lyric payload lost fields")
	}
	if !userdata.WriteJSON(path, catalog) {
		t.Fatal("could not persist restored catalog")
	}
	if after, err := Load(path); err != nil || !slices.Equal(after.Folders, catalog.Folders) || after.LastID != 9 {
		t.Fatal("catalog round-trip changed its schema", err)
	}
	if err := os.WriteFile(path, []byte("broken"), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := Load(path); err == nil {
		t.Fatal("malformed JSON was accepted")
	}
	if b, err := os.ReadFile(path); err != nil || string(b) != "broken" {
		t.Fatal("load overwrote the malformed catalog")
	}
}

// Startup cleanup only removes retired IDs from its snapshot; newly allocated IDs and live matches survive.
func TestPruneLyricsPreservesNewAndLiveFiles(t *testing.T) {
	path := filepath.Join(t.TempDir(), "local-music.json")
	var cache LyricCache
	for _, id := range []int64{1, 2, 10} {
		if err := cache.Save(LyricFile(path, id), id, LyricMatch{SongID: 7, Lyrics: lyrics.Text{Original: "saved"}}); err != nil {
			t.Fatal(err)
		}
	}
	PruneLyrics(path, 9, map[int64]int{2: 0})
	if _, err := os.Stat(LyricFile(path, 1)); !os.IsNotExist(err) {
		t.Fatal("retired file survived startup cleanup")
	}
	for _, id := range []int64{2, 10} {
		if _, err := os.Stat(LyricFile(path, id)); err != nil {
			t.Fatal("startup snapshot deleted live/new lyrics", err)
		}
	}
	// Availability updates affect search revisions without rebuilding the song identity.
	var state State
	state.Replace(Catalog{Songs: []Song{{Track: netease.Track{LocalID: 2}}}})
	rev := state.Revision
	state.MarkMissing(2)
	state.MarkMissing(2)
	if !state.Missing[2] || state.Revision != rev+1 {
		t.Fatal("availability updates missed or repeatedly invalidated the search cache")
	}
}
