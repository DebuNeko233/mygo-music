/*
验证写入多语言标签、封面和歌词后，各音频格式的解码 PCM 保持一致。
*/

package player

import (
	"bytes"
	"image"
	"image/png"
	"os"
	"path/filepath"
	"slices"
	"testing"

	"go.senan.xyz/taglib"
	audiotag "netease-music-mygo/internal/features/download/tagging"
)

// Embedded multilingual credits, cover and timed lyrics must round-trip in
// every supported container without changing even one decoded PCM sample.
func TestTaggedFormatsPreservePCM(t *testing.T) {
	var cover bytes.Buffer
	png.Encode(&cover, image.NewRGBA(image.Rect(0, 0, 12, 12)))
	fixtures := append(supportedAudioFixtures(t), formatFixture{"fragmented", "m4a", testAudio(t, "seek-fragmented.m4a")})
	for _, fixture := range fixtures {
		t.Run(fixture.name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "song."+fixture.kind)
			data := fixture.data
			if fixture.kind == "aac" {
				// Existing tagged uploads can carry v1 tails; the decoder must
				// accept them before the exporter normalizes the new tags.
				tail := make([]byte, 128)
				copy(tail, "TAG")
				data = append(append([]byte(nil), data...), tail...)
			}
			if err := os.WriteFile(path, data, 0600); err != nil {
				t.Fatal(err)
			}
			_, before, err := openLocal(path)
			if err != nil {
				t.Fatal(err)
			}
			want := readAudioPCM(t, before)
			before.Close()
			tags := map[string][]string{
				taglib.Title: {"测试曲 — 嵐"}, taglib.Artist: {"歌手甲", "歌手乙"}, taglib.AlbumArtist: {"专辑歌手"},
				taglib.Album: {"测试专辑"}, taglib.TrackNumber: {"2/12"}, taglib.DiscNumber: {"1/2"},
				taglib.Date: {"2026-11-23"}, taglib.Genre: {"Pop"}, taglib.ISRC: {"TEST26000001"}, taglib.Composer: {"作者"},
				taglib.Lyrics: {"[00:01.234]Original\n[00:01.234]译文"}, taglib.Comment: {"原有备注"},
			}
			if err := audiotag.Write(path, tags, cover.Bytes(), false); err != nil {
				t.Fatal(err)
			}
			got, err := taglib.ReadTags(path)
			if err != nil {
				t.Fatal(err)
			}
			for key, values := range tags {
				if !slices.Equal(got[key], values) {
					t.Errorf("%s = %q, want %q", key, got[key], values)
				}
			}
			pic, err := taglib.ReadImage(path)
			if err != nil || !bytes.Equal(pic, cover.Bytes()) {
				t.Fatal("cover changed", err)
			}
			_, after, err := openLocal(path)
			if err != nil {
				t.Fatal(err)
			}
			pcm := readAudioPCM(t, after)
			after.Close()
			if len(want) == 0 || !slices.Equal(want, pcm) {
				t.Fatalf("PCM changed: %d / %d samples", len(want), len(pcm))
			}
			// Private uploads keep their own credits and assets, and fill missing fields.
			if err := audiotag.Write(path, map[string][]string{taglib.Title: {"错误匹配"}, taglib.Comment: {"覆盖"}, taglib.Label: {"厂牌"}}, nil, true); err != nil {
				t.Fatal(err)
			}
			got, _ = taglib.ReadTags(path)
			if got[taglib.Title][0] != tags[taglib.Title][0] || got[taglib.Comment][0] != "原有备注" || got[taglib.Label][0] != "厂牌" {
				t.Fatal("private tags overwritten", got)
			}
		})
	}
}
