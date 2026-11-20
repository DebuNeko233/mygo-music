package app

import (
	"image"

	"netease-music-mygo/internal/netease"
)

func fetchImage(url string) (image.Image, error) {
	b, err := netease.Fetch(url)
	if err != nil {
		return nil, err
	}
	return decodeImage(b)
}
