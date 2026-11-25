/*
按下载权限获取歌曲资源地址，为私人上传歌曲使用网盘资源接口。
*/

package netease

import (
	"context"
	"slices"
)

// DownloadURL uses download rights, which can differ from playback rights.
// Private uploads instead use their existing cloud playback endpoint.
func DownloadURL(ctx context.Context, track Track, level string) (*Stream, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if track.IsLocal() || track.ID <= 0 || !slices.Contains(Levels, level) {
		return nil, ErrUnavailable
	}
	if track.Cloud {
		return SongURLContext(ctx, track.ID, level)
	}

	const path = "/api/song/enhance/download/url/v1"
	var res struct {
		apiError
		Data songURLData `json:"data"`
	}
	if err := eapiPostContext(ctx, path, map[string]any{"id": track.ID, "level": level, "immerseType": "c51"}, &res); err != nil {
		return nil, err
	}
	if res.Code != 200 {
		return nil, res.codeError(path)
	}
	if res.Data.Code != 0 && res.Data.Code != 200 {
		return nil, ErrUnavailable
	}
	// Missing download quality stays unknown instead of borrowing the request.
	return res.Data.stream("")
}
