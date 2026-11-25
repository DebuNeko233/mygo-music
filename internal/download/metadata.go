/*
收集歌曲标签与封面资源，并将对齐后的歌词转换为导出文本。
*/

package download

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	"go.senan.xyz/taglib"
	"netease-music-mygo/internal/lyrics"
	"netease-music-mygo/internal/netease"
)

// songMetadata holds the metadata and assets for one private staging copy.
type songMetadata struct {
	track           netease.Track
	tags            map[string][]string
	cover           []byte
	lyrics, warning string
}

func loadMetadata(ctx context.Context, path string, tr netease.Track) (info songMetadata, err error) {
	var album *netease.AlbumProfile
	if !tr.Cloud {
		details, e := trackDetails(ctx, []int64{tr.ID})
		if e != nil {
			return info, fmt.Errorf("获取歌曲元数据：%w", e)
		}
		if len(details) != 1 || details[0].ID != tr.ID {
			return info, errors.New("歌曲元数据不匹配")
		}
		listedAlbum := tr.Album
		tr = details[0]
		if tr.Album.ID == listedAlbum.ID && netease.CoverURL(tr.Album.PicURL) == "" {
			tr.Album.PicURL = listedAlbum.PicURL
		}
	}
	if !tr.Cloud && tr.Album.ID > 0 {
		album, err = albumDetail(ctx, tr.Album.ID)
		if err != nil {
			return info, fmt.Errorf("获取专辑元数据：%w", err)
		}
		if album == nil || album.ID != tr.Album.ID {
			return info, errors.New("专辑元数据不匹配")
		}
	}
	existing, e := taglib.ReadTags(path)
	if e != nil {
		return info, fmt.Errorf("读取音频标签：%w", e)
	}
	oldCover, e := taglib.ReadImage(path)
	if e != nil {
		return info, fmt.Errorf("读取原有封面：%w", e)
	}
	lrc := strings.Join(existing[taglib.Lyrics], "\n")
	if !tr.Cloud || lrc == "" {
		original, translation, words, e := lyricText(ctx, tr.ID)
		if e != nil {
			return info, fmt.Errorf("获取歌词：%w", e)
		}
		if fetched := RenderLyrics(original, translation, words); fetched != "" {
			lrc = fetched
		}
	}

	pic := netease.CoverURL(tr.Album.PicURL)
	if album != nil && netease.CoverURL(album.PicURL) != "" {
		pic = album.PicURL
	}
	var cover []byte
	if pic != "" && (!tr.Cloud || len(oldCover) == 0) {
		cover, err = coverImage(ctx, pic)
		if err != nil {
			return info, fmt.Errorf("获取专辑封面：%w", err)
		}
		if len(cover) == 0 {
			return info, errors.New("专辑封面为空")
		}
	}
	info.track, info.tags, info.cover, info.lyrics = tr, metadata(tr, album, lrc), cover, lrc
	var warnings []string
	if lrc == "" {
		warnings = append(warnings, "网易云未提供歌词")
	}
	if len(cover) == 0 && len(oldCover) == 0 {
		warnings = append(warnings, "网易云未提供封面")
	}
	info.warning = strings.Join(warnings, "；")
	return info, nil
}

func metadata(tr netease.Track, album *netease.AlbumProfile, lrc string) map[string][]string {
	tags := map[string][]string{taglib.Title: {tr.Name}, taglib.Album: {tr.Album.Name}, taglib.Lyrics: {lrc}}
	for _, ar := range tr.Artists {
		if ar.Name != "" {
			tags[taglib.Artist] = append(tags[taglib.Artist], ar.Name)
		}
	}
	// A catalogue match on a private upload does not establish its release
	// date, ISRC or disc order. Keep the file's own values for these.
	if tr.Cloud {
		return tags
	}
	if tr.ISRC != "" {
		tags[taglib.ISRC] = []string{tr.ISRC}
	}
	if tr.PublishTime > 0 {
		tags[taglib.Date] = []string{time.UnixMilli(tr.PublishTime).In(time.FixedZone("CST", 8*60*60)).Format("2006-01-02")}
	}
	if tr.TrackNumber > 0 {
		tags[taglib.TrackNumber] = []string{strconv.Itoa(tr.TrackNumber)}
	}
	if n, err := strconv.Atoi(tr.DiscNumber); err == nil && n > 0 {
		tags[taglib.DiscNumber] = []string{strconv.Itoa(n)}
	}
	if album == nil {
		return tags
	}
	if album.Name != "" {
		tags[taglib.Album] = []string{album.Name}
	}
	for _, ar := range album.Artists {
		if ar.Name != "" {
			tags[taglib.AlbumArtist] = append(tags[taglib.AlbumArtist], ar.Name)
		}
	}
	if album.PublishTime > 0 {
		tags[taglib.Date] = []string{time.UnixMilli(album.PublishTime).In(time.FixedZone("CST", 8*60*60)).Format("2006-01-02")}
	}
	if album.Company != "" {
		tags[taglib.Label] = []string{album.Company}
	}
	// Totals come from the album's complete track list. Normalize numeric
	// disc IDs ("01" and "1"); never invent totals for a partial/gapped list.
	counts := make(map[int]int)
	maxDisc := 0
	var found *netease.Track
	complete := album.Size <= 0 || album.Size == len(album.Songs)
	for i := range album.Songs {
		song := &album.Songs[i]
		disc, _ := strconv.Atoi(song.DiscNumber)
		if disc > 0 {
			counts[disc]++
			maxDisc = max(maxDisc, disc)
		} else {
			counts[0]++
		}
		if song.ID == tr.ID {
			found = song
		}
	}
	if found != nil {
		disc, _ := strconv.Atoi(found.DiscNumber)
		if found.TrackNumber > 0 {
			number := strconv.Itoa(found.TrackNumber)
			if complete && found.TrackNumber <= counts[disc] {
				number = fmt.Sprintf("%d/%d", found.TrackNumber, counts[disc])
			}
			tags[taglib.TrackNumber] = []string{number}
		}
		if disc > 0 {
			number := strconv.Itoa(disc)
			if complete && counts[0] == 0 && maxDisc == len(counts) {
				number = fmt.Sprintf("%d/%d", disc, maxDisc)
			}
			tags[taglib.DiscNumber] = []string{number}
		}
	}
	return tags
}

// RenderLyrics uses the same timestamp/translation alignment as the player.
// Untimed lyrics are still useful and must not disappear on export.
func RenderLyrics(original, translation, words string) string {
	_, display := lyrics.BuildDisplay(original, translation, words)
	if !display.HasSinging() && display.EmptyText() == lyrics.UnavailableText {
		return ""
	}
	var out strings.Builder
	for _, line := range display.Lines {
		ms := max(0, line.At.Milliseconds())
		stamp := fmt.Sprintf("[%02d:%02d.%03d]", ms/60000, ms/1000%60, ms%1000)
		fmt.Fprintf(&out, "%s%s\n", stamp, line.Text)
		if line.Translation != "" && line.Translation != line.Text {
			fmt.Fprintf(&out, "%s%s\n", stamp, line.Translation)
		}
	}
	if out.Len() == 0 {
		return strings.TrimSpace(original)
	}
	return strings.TrimSpace(out.String())
}
