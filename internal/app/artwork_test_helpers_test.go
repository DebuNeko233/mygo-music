/*
提供封面加载测试使用的图片请求与缓存保留辅助方法。
*/

package app

import (
	"errors"
	"image"

	"github.com/egoist/mygo/ui"

	artcache "netease-music-mygo/internal/desktop/artwork"
	"netease-music-mygo/internal/netease"
)

func fetchImage(url string) (image.Image, error) {
	b, err := netease.Fetch(url)
	if err != nil {
		return nil, err
	}
	return artcache.DecodeImage(b)
}

const artJobs = 8

func holdArtwork(c *artcache.Cache, key string) {
	c.RequestCover(key, false, true, func(string) (*ui.Bitmap, image.Image, int, error) { return nil, nil, 0, errors.New("pending fixture") })
}
