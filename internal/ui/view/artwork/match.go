/*
从文件名和歌曲标签构造查询，按歌手、标题及长度匹配可信的在线封面来源。
*/

package artwork

import (
	"strings"
	"unicode"

	"golang.org/x/text/unicode/norm"

	"netease-music-mygo/internal/features/netease"
)

func FilenameTags(track netease.Track) netease.Track {
	if track.Album.Name == "" && len(track.Artists) == 0 {
		if artist, title, ok := strings.Cut(track.Name, " - "); ok {
			track.Name = title
			track.Artists = []netease.Artist{{Name: artist}}
		}
	}
	return track
}

func FindTrack(track netease.Track, search func(string, int) ([]netease.Track, error)) (string, error) {
	if !canMatchArtwork(&track) {
		return "", nil
	}
	query := trackQuery(&track)
	if track.ArtistNames() == "" {
		query = strings.TrimSpace(query + " " + track.Album.Name)
	}
	candidates, err := search(query, 20)
	if err != nil {
		return "", err
	}
	return chooseTrack(&track, candidates), nil
}

func trackQuery(t *netease.Track) string {
	return strings.TrimSpace(t.Name + " " + t.ArtistNames())
}

func artworkTag(s string) string {
	return strings.Map(func(r rune) rune {
		if unicode.IsLetter(r) || unicode.IsNumber(r) {
			return unicode.ToLower(r)
		}
		return -1
	}, norm.NFKC.String(s))
}

func canMatchArtwork(track *netease.Track) bool {
	return track.Duration > 0 && artworkTag(track.Name) != "" &&
		(artworkTag(track.ArtistNames()) != "" || artworkTag(track.Album.Name) != "")
}

// Require matching tags and a close duration. If two nearly equally close
// versions have different art, keep the placeholder.
func chooseTrack(local *netease.Track, candidates []netease.Track) string {
	if !canMatchArtwork(local) {
		return ""
	}
	title, artist, album := artworkTag(local.Name), artworkTag(local.ArtistNames()), artworkTag(local.Album.Name)
	const tolerance = 5000
	best, other := int64(tolerance+1000), int64(tolerance+1000)
	url := ""
	for _, t := range candidates {
		if netease.CoverURL(t.Album.PicURL) == "" || t.Duration <= 0 || artworkTag(t.Name) != title ||
			artist != "" && artworkTag(t.ArtistNames()) != artist || album != "" && artworkTag(t.Album.Name) != album {
			continue
		}
		diff := max(t.Duration-local.Duration, local.Duration-t.Duration)
		if diff > tolerance {
			continue
		}
		if t.Album.PicURL == url {
			best = min(best, diff)
		} else if diff < best {
			best, other, url = diff, best, t.Album.PicURL
		} else {
			other = min(other, diff)
		}
	}
	if other-best < 1000 {
		return ""
	}
	return url
}
