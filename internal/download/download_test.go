package download

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"image"
	"image/png"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"go.senan.xyz/taglib"
	"netease-music-mygo/internal/netease"
)

func downloadFixture(t *testing.T, audio []byte) (netease.Track, []byte) {
	t.Helper()
	oldStream, oldAlbum, oldDetails, oldLyrics, oldCover := streamURL, albumDetail, trackDetails, lyricText, coverImage
	t.Cleanup(func() {
		streamURL, albumDetail, trackDetails, lyricText, coverImage = oldStream, oldAlbum, oldDetails, oldLyrics, oldCover
	})
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.ServeContent(w, r, "audio", time.Time{}, bytes.NewReader(audio))
	}))
	t.Cleanup(srv.Close)
	tr := netease.Track{ID: 12, Name: "测试曲", Artists: []netease.Artist{{Name: "甲"}, {Name: "乙"}}, Album: netease.Album{ID: 7, Name: "原专辑", PicURL: "https://art.example/original"}, TrackNumber: 2, DiscNumber: "1"}
	streamURL = func(context.Context, netease.Track, string) (*netease.Stream, error) {
		return &netease.Stream{URL: srv.URL, Size: int64(len(audio)), Type: "mp3", Level: "exhigh"}, nil
	}
	trackDetails = func(context.Context, []int64) ([]netease.Track, error) { return []netease.Track{tr}, nil }
	albumDetail = func(context.Context, int64) (*netease.AlbumProfile, error) {
		return &netease.AlbumProfile{Album: tr.Album, Artists: []netease.Artist{{Name: "专辑歌手"}}, PublishTime: time.Date(2026, 11, 23, 0, 0, 0, 0, time.UTC).UnixMilli(), Company: "厂牌", Songs: []netease.Track{{ID: 11, TrackNumber: 1, DiscNumber: "1"}, tr, {ID: 13, TrackNumber: 1, DiscNumber: "2"}}}, nil
	}
	lyricText = func(context.Context, int64) (string, string, string, error) {
		return "[00:01.123]Original", "[00:01.123]译文", "", nil
	}
	var cover bytes.Buffer
	if err := png.Encode(&cover, image.NewRGBA(image.Rect(0, 0, 8, 8))); err != nil {
		t.Fatal(err)
	}
	coverImage = func(context.Context, string) ([]byte, error) { return cover.Bytes(), nil }
	return tr, cover.Bytes()
}

// Export detects the actual bytes rather than trusting a server's extension,
// embeds all supplied assets, and publishes a matching optional lyric sidecar.
func TestSaveFormatsAndMetadata(t *testing.T) {
	for _, name := range []string{"seek.mp3", "seek.flac", "seek.ogg", "seek.aac", "seek-he.aac", "seek.m4a", "seek-alac.m4a", "seek-he.m4a", "seek.3gp", "seek.3g2", "seek-fragmented.m4a"} {
		t.Run(name, func(t *testing.T) {
			audio, err := os.ReadFile(filepath.Join("..", "player", "testdata", name))
			if err != nil {
				t.Fatal(err)
			}
			tr, cover := downloadFixture(t, audio)
			dir := t.TempDir()
			result, err := Save(context.Background(), tr, Options{Directory: dir, Quality: "hires", Sidecar: true})
			if err != nil {
				t.Fatal(err)
			}
			if result.Quality != "exhigh" || result.Warning != "" {
				t.Fatalf("result: %+v", result)
			}
			tags, err := taglib.ReadTags(result.Path)
			if err != nil {
				t.Fatal(err)
			}
			for k, want := range map[string]string{taglib.Title: tr.Name, taglib.AlbumArtist: "专辑歌手", taglib.TrackNumber: "2/2", taglib.DiscNumber: "1/2", taglib.Date: "2026-11-23", taglib.Label: "厂牌"} {
				if len(tags[k]) != 1 || tags[k][0] != want {
					t.Errorf("%s: %q, want %q", k, tags[k], want)
				}
			}
			pic, _ := taglib.ReadImage(result.Path)
			if !bytes.Equal(pic, cover) {
				t.Fatal("cover changed")
			}
			lrcPath := strings.TrimSuffix(result.Path, filepath.Ext(result.Path)) + ".lrc"
			lrc, err := os.ReadFile(lrcPath)
			if err != nil || strings.TrimSpace(string(lrc)) != tags[taglib.Lyrics][0] {
				t.Fatal("sidecar differs", err)
			}
			if !strings.Contains(string(lrc), "[00:01.123]译文") {
				t.Fatal("lost translation", string(lrc))
			}
			entries, _ := os.ReadDir(dir)
			if len(entries) != 2 {
				t.Fatal("left staging files", entries)
			}
		})
	}
}

// A colliding audio or sidecar must remain byte-for-byte intact, and repeated
// downloads get a suffix instead of overwriting or leaving an orphaned lyric.
func TestSaveNeverOverwrites(t *testing.T) {
	audio, _ := os.ReadFile("../player/testdata/seek.flac")
	tr, _ := downloadFixture(t, audio)
	dir := t.TempDir()
	base := filename(tr.ArtistNames(), tr.Name, NameArtistTitle)
	old := filepath.Join(dir, base+".flac")
	oldLRC := filepath.Join(dir, base+" (2).lrc")
	for _, p := range []string{old, oldLRC} {
		if err := os.WriteFile(p, []byte("keep"), 0600); err != nil {
			t.Fatal(err)
		}
	}
	result, err := Save(context.Background(), tr, Options{Directory: dir, Quality: "exhigh", Sidecar: true})
	if err != nil {
		t.Fatal(err)
	}
	if filepath.Base(result.Path) != base+" (3).flac" {
		t.Fatal(result.Path)
	}
	for _, p := range []string{old, oldLRC} {
		b, _ := os.ReadFile(p)
		if string(b) != "keep" {
			t.Fatal("overwritten", p)
		}
	}
	if _, err := os.Stat(filepath.Join(dir, base+".lrc")); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("orphan lyric", err)
	}
}

// Failures at permission, transfer, metadata, artwork and cancellation leave
// no apparently completed audio or private staging data in the destination.
func TestSaveFailuresAreNotPublished(t *testing.T) {
	for _, failure := range []string{"permission", "truncated", "html", "album", "lyrics", "cover", "invalid cover", "cancel"} {
		t.Run(failure, func(t *testing.T) {
			audio, _ := os.ReadFile("../player/testdata/seek.mp3")
			if failure == "html" {
				audio = []byte("<html>expired</html>")
			}
			tr, _ := downloadFixture(t, audio)
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			sentinel := errors.New("failed")
			switch failure {
			case "permission":
				streamURL = func(context.Context, netease.Track, string) (*netease.Stream, error) {
					return nil, netease.ErrUnavailable
				}
			case "truncated":
				orig := streamURL
				streamURL = func(c context.Context, tr netease.Track, q string) (*netease.Stream, error) {
					st, e := orig(c, tr, q)
					st.Size++
					return st, e
				}
			case "album":
				albumDetail = func(context.Context, int64) (*netease.AlbumProfile, error) { return nil, sentinel }
			case "lyrics":
				lyricText = func(context.Context, int64) (string, string, string, error) { return "", "", "", sentinel }
			case "cover":
				coverImage = func(context.Context, string) ([]byte, error) { return nil, sentinel }
			case "invalid cover":
				coverImage = func(context.Context, string) ([]byte, error) { return []byte("not an image"), nil }
			case "cancel":
				orig := coverImage
				coverImage = func(c context.Context, s string) ([]byte, error) { b, e := orig(c, s); cancel(); return b, e }
			}
			dir := t.TempDir()
			res, err := Save(ctx, tr, Options{Directory: dir, Quality: "exhigh", Sidecar: true})
			if err == nil || res.Path != "" {
				t.Fatal("published failure", res, err)
			}
			entries, _ := os.ReadDir(dir)
			if len(entries) != 0 {
				t.Fatal("left files", entries)
			}
		})
	}
}

// Private uploads preserve embedded assets without fetching unrelated public
// metadata, while source-confirmed missing assets remain visible as warnings.
func TestSaveCloudPreservesPrivateTags(t *testing.T) {
	audio, _ := os.ReadFile("../player/testdata/seek.m4a")
	p := filepath.Join(t.TempDir(), "private.m4a")
	os.WriteFile(p, audio, 0600)
	if err := taglib.WriteTags(p, map[string][]string{taglib.Title: {"私有标题"}, taglib.Artist: {"私有歌手"}, taglib.Lyrics: {"[00:02.000]Private"}}, 0); err != nil {
		t.Fatal(err)
	}
	var picture bytes.Buffer
	png.Encode(&picture, image.NewRGBA(image.Rect(0, 0, 8, 8)))
	if err := taglib.WriteImage(p, picture.Bytes()); err != nil {
		t.Fatal(err)
	}
	audio, _ = os.ReadFile(p)
	tr, _ := downloadFixture(t, audio)
	coverImage = func(context.Context, string) ([]byte, error) {
		t.Error("fetched cover despite private artwork")
		return nil, fmt.Errorf("unrelated")
	}
	tr.Cloud = true
	trackDetails = func(context.Context, []int64) ([]netease.Track, error) {
		t.Error("fetched catalogue for upload")
		return nil, fmt.Errorf("unrelated")
	}
	lyricText = func(context.Context, int64) (string, string, string, error) {
		t.Error("fetched lyrics despite private tags")
		return "", "", "", fmt.Errorf("unrelated")
	}
	res, err := Save(context.Background(), tr, Options{Directory: t.TempDir(), Quality: "exhigh", Sidecar: true})
	if err != nil {
		t.Fatal(err)
	}
	tags, _ := taglib.ReadTags(res.Path)
	if tags[taglib.Title][0] != "私有标题" || tags[taglib.Artist][0] != "私有歌手" || tags[taglib.Lyrics][0] != "[00:02.000]Private" {
		t.Fatal(tags)
	}
}

// Sparse detail responses must not discard the valid artwork address already
// carried by the song list for the same album.
func TestSaveKeepsListedCoverWhenDetailsOmitIt(t *testing.T) {
	audio, err := os.ReadFile("../player/testdata/seek.flac")
	if err != nil {
		t.Fatal(err)
	}
	tr, cover := downloadFixture(t, audio)
	details, getAlbum := trackDetails, albumDetail
	trackDetails = func(ctx context.Context, ids []int64) ([]netease.Track, error) {
		tracks, err := details(ctx, ids)
		tracks[0].Album.PicURL = ""
		return tracks, err
	}
	albumDetail = func(ctx context.Context, id int64) (*netease.AlbumProfile, error) {
		album, err := getAlbum(ctx, id)
		album.PicURL = ""
		return album, err
	}
	result, err := Save(context.Background(), tr, Options{Directory: t.TempDir(), Quality: "exhigh"})
	if err != nil {
		t.Fatal(err)
	}
	got, err := taglib.ReadImage(result.Path)
	if err != nil || !bytes.Equal(got, cover) || result.Warning != "" {
		t.Fatal("listed artwork lost", result, err)
	}
}

// An empty lyric response preserves the audio's own timed lyrics in both its
// embedded tags and the requested sidecar, without a false missing-lyrics warning.
func TestSaveUsesExistingLyricsForSidecar(t *testing.T) {
	audio, err := os.ReadFile("../player/testdata/seek.flac")
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), "original.flac")
	if err := os.WriteFile(path, audio, 0600); err != nil {
		t.Fatal(err)
	}
	const original = "[00:02.123]Existing lyric"
	if err := taglib.WriteTags(path, map[string][]string{taglib.Lyrics: {original}}, 0); err != nil {
		t.Fatal(err)
	}
	audio, err = os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	tr, _ := downloadFixture(t, audio)
	lyricText = func(context.Context, int64) (string, string, string, error) { return "", "", "", nil }
	result, err := Save(context.Background(), tr, Options{Directory: t.TempDir(), Quality: "exhigh", Sidecar: true})
	if err != nil {
		t.Fatal(err)
	}
	got, err := os.ReadFile(strings.TrimSuffix(result.Path, ".flac") + ".lrc")
	if err != nil || strings.TrimSpace(string(got)) != original || result.Warning != "" {
		t.Fatal("existing lyrics not exported", result, err)
	}
}

// Cloud uploads without artist metadata use the song title alone; an absent
// artist must not leave a separator prefix on either the audio or its LRC.
func TestSaveCloudFilenameOmitsMissingArtistSeparator(t *testing.T) {
	for _, test := range []struct {
		name    string
		artists []netease.Artist
		want    string
	}{
		{"missing", nil, "私藏歌曲"},
		{"empty", []netease.Artist{{Name: ""}}, "私藏歌曲"},
		{"present", []netease.Artist{{Name: "歌手甲"}, {Name: "歌手乙"}}, "歌手甲, 歌手乙 - 私藏歌曲"},
	} {
		t.Run(test.name, func(t *testing.T) {
			audio, err := os.ReadFile("../player/testdata/seek.flac")
			if err != nil {
				t.Fatal(err)
			}
			tr, _ := downloadFixture(t, audio)
			tr.Cloud, tr.Name, tr.Artists = true, "私藏歌曲", test.artists
			result, err := Save(context.Background(), tr, Options{Directory: t.TempDir(), Quality: "exhigh", Sidecar: true})
			if err != nil {
				t.Fatal(err)
			}
			if filepath.Base(result.Path) != test.want+".flac" {
				t.Fatalf("filename = %q, want %q", filepath.Base(result.Path), test.want+".flac")
			}
			lrc, err := os.ReadFile(filepath.Join(filepath.Dir(result.Path), test.want+".lrc"))
			if err != nil || !strings.Contains(string(lrc), "Original") {
				t.Fatal("sidecar did not follow audio filename", err)
			}
		})
	}
}

// Uploaded song titles can include old or repeated audio extensions. Publish
// exactly one actual-container extension and keep unrelated dotted titles.
func TestSaveCloudFilenameKeepsOneActualExtension(t *testing.T) {
	for _, test := range []struct{ title, want string }{
		{"私藏歌曲.flac", "私藏歌曲.flac"},
		{"私藏歌曲.FLAC", "私藏歌曲.flac"},
		{"私藏歌曲.mp3.flac", "私藏歌曲.flac"},
		{"私藏歌曲.3gp", "私藏歌曲.flac"},
		{"私藏歌曲.flac. ", "私藏歌曲.flac"},
		{"私藏歌曲.Live", "私藏歌曲.Live.flac"},
	} {
		t.Run(test.title, func(t *testing.T) {
			audio, err := os.ReadFile("../player/testdata/seek.flac")
			if err != nil {
				t.Fatal(err)
			}
			tr, _ := downloadFixture(t, audio)
			tr.Cloud, tr.Name, tr.Artists = true, test.title, nil
			result, err := Save(context.Background(), tr, Options{Directory: t.TempDir(), Quality: "exhigh", Sidecar: true})
			if err != nil {
				t.Fatal(err)
			}
			if filepath.Base(result.Path) != test.want {
				t.Fatalf("filename = %q, want %q", filepath.Base(result.Path), test.want)
			}
			if _, err := os.Stat(strings.TrimSuffix(result.Path, ".flac") + ".lrc"); err != nil {
				t.Fatal("sidecar did not follow normalized filename", err)
			}
		})
	}
}

// The selected naming format applies to audio and sidecar together, still
// using the real container extension and omitting absent artist separators.
func TestSaveFilenameFormats(t *testing.T) {
	for _, test := range []struct{ format, artist, want string }{
		{NameArtistTitle, "歌手", "歌手 - 歌曲"},
		{NameTitleArtist, "歌手", "歌曲 - 歌手"},
		{NameTitle, "歌手", "歌曲"},
		{NameArtistTitle, "", "歌曲"},
		{NameTitleArtist, "", "歌曲"},
		{NameTitle, "", "歌曲"},
		{"unknown", "歌手", "歌手 - 歌曲"},
	} {
		t.Run(test.format+test.artist, func(t *testing.T) {
			audio, err := os.ReadFile("../player/testdata/seek.flac")
			if err != nil {
				t.Fatal(err)
			}
			tr, _ := downloadFixture(t, audio)
			tr.Cloud, tr.Name = true, "歌曲.MP3"
			tr.Artists = nil
			if test.artist != "" {
				tr.Artists = []netease.Artist{{Name: test.artist}}
			}
			result, err := Save(context.Background(), tr, Options{Directory: t.TempDir(), Quality: "exhigh", NameFormat: test.format, Sidecar: true})
			if err != nil {
				t.Fatal(err)
			}
			if filepath.Base(result.Path) != test.want+".flac" {
				t.Fatalf("filename = %q, want %q", filepath.Base(result.Path), test.want+".flac")
			}
			if _, err := os.Stat(filepath.Join(filepath.Dir(result.Path), test.want+".lrc")); err != nil {
				t.Fatal("sidecar did not use selected format", err)
			}
		})
	}
}
