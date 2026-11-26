/*
验证专辑曲序与碟号元数据，以及导出歌词保留署名和纯文本内容。
*/

package download

import (
	"strings"
	"testing"
	"time"

	"go.senan.xyz/taglib"
	"netease-music-mygo/internal/features/netease"
)

// Disc identifiers are normalized, album completeness gates totals, and
// unknown or gapped disc numbering must never produce impossible ratios.
func TestMetadataUsesAlbumOrderAndKnownTotals(t *testing.T) {
	tr := netease.Track{ID: 9, TrackNumber: 2, DiscNumber: "01", PublishTime: time.Date(2026, 11, 22, 16, 0, 0, 0, time.UTC).UnixMilli()}
	al := &netease.AlbumProfile{Songs: []netease.Track{{ID: 8, TrackNumber: 1, DiscNumber: "1"}, tr, {ID: 10, TrackNumber: 1, DiscNumber: "02"}}, Size: 3}
	tags := metadata(tr, al, "")
	if tags[taglib.TrackNumber][0] != "2/2" || tags[taglib.DiscNumber][0] != "1/2" || tags[taglib.Date][0] != "2026-11-23" {
		t.Fatal(tags)
	}
	al.Size = 4
	tags = metadata(tr, al, "")
	if tags[taglib.TrackNumber][0] != "2" || tags[taglib.DiscNumber][0] != "1" {
		t.Fatal("invented partial totals", tags)
	}
	al.Size = 3
	al.Songs[2].DiscNumber = "3"
	tags = metadata(tr, al, "")
	if tags[taglib.DiscNumber][0] != "1" {
		t.Fatal("invented disc count", tags)
	}
	al.Songs[1].TrackNumber = 12
	tags = metadata(tr, al, "")
	if tags[taglib.TrackNumber][0] != "12" {
		t.Fatal("impossible track ratio", tags)
	}
}

// Export keeps timed credits and aligned translations, including millisecond
// precision, and preserves plain lyrics when no timestamp parser can use them.
func TestRenderLyricsKeepsCreditsAndPlainText(t *testing.T) {
	got := RenderLyrics("[00:00.000]作词：作者\n[00:01.234]Original", "[00:01.234]译文", "")
	for _, want := range []string{"作词：作者", "[00:01.234]Original", "[00:01.234]译文"} {
		if !strings.Contains(got, want) {
			t.Fatalf("lost %q: %s", want, got)
		}
	}
	if got := RenderLyrics("Plain lyrics\nsecond line", "", ""); got != "Plain lyrics\nsecond line" {
		t.Fatal(got)
	}
	if got := RenderLyrics("暂无歌词", "", ""); got != "" {
		t.Fatal("embedded an absence notice as lyrics", got)
	}
	if got := RenderLyrics("", "", ""); got != "" {
		t.Fatal("invented lyrics", got)
	}
}
