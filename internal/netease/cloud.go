/*
分页读取用户上传的网盘歌曲，保留上传身份并支持请求取消。
*/

package netease

import (
	"context"
	"path"
	"strings"
)

// CloudPage is one page of the signed-in listener's uploaded songs. Offset
// counts server entries, including entries omitted for having no usable ID.
type CloudPage struct {
	Tracks     []Track
	Count      int
	HasMore    bool
	NextOffset int
}

type cloudSong struct {
	SongID     int64  `json:"songId"`
	SongName   string `json:"songName"`
	Artist     string `json:"artist"`
	Album      string `json:"album"`
	FileName   string `json:"fileName"`
	SimpleSong Track  `json:"simpleSong"`
}

// track keeps the cloud ID: simpleSong can describe a different catalogue
// match. A private upload need not have catalogue artists or album pages.
func (s cloudSong) track() Track {
	t := s.SimpleSong
	t.ID = s.SongID
	t.Cloud = true
	t.Album.PicURL = CoverURL(t.Album.PicURL)
	if strings.TrimSpace(s.SongName) != "" {
		t.Name = s.SongName
	}
	if strings.TrimSpace(t.Name) == "" {
		t.Name = strings.TrimSuffix(path.Base(s.FileName), path.Ext(s.FileName))
	}
	if t.Name == "" || t.Name == "." {
		t.Name = "未命名歌曲"
	}
	var artists []Artist
	for _, ar := range t.Artists {
		if strings.TrimSpace(ar.Name) != "" {
			if ar.ID < 0 {
				ar.ID = 0
			}
			artists = append(artists, ar)
		}
	}
	if len(artists) == 0 && strings.TrimSpace(s.Artist) != "" {
		artists = []Artist{{Name: s.Artist}}
	}
	t.Artists = artists
	if t.Album.ID < 0 {
		t.Album.ID = 0
	}
	if strings.TrimSpace(t.Album.Name) == "" {
		t.Album.Name = s.Album
	}
	return t
}

// CloudSongs lists uploaded songs with the current login. Playback uses the
// existing SongURL endpoint, which checks access to each uploaded file.
func CloudSongs(limit, offset int) (*CloudPage, error) {
	return CloudSongsContext(context.Background(), limit, offset)
}

// CloudSongsContext lets a library scan stop an in-flight page request.
func CloudSongsContext(ctx context.Context, limit, offset int) (*CloudPage, error) {
	limit = max(1, min(limit, 100))
	offset = max(0, offset)
	var res struct {
		Data    []cloudSong `json:"data"`
		Count   int         `json:"count"`
		HasMore bool        `json:"hasMore"`
	}
	if err := weapiPostContext(ctx, "/weapi/v1/cloud/get", map[string]any{"limit": limit, "offset": offset}, &res); err != nil {
		return nil, err
	}
	p := &CloudPage{Count: res.Count, HasMore: res.HasMore && len(res.Data) > 0, NextOffset: offset + len(res.Data)}
	for _, song := range res.Data {
		if t := song.track(); t.ID > 0 {
			p.Tracks = append(p.Tracks, t)
		}
	}
	return p, nil
}
