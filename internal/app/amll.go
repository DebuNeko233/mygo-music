package app

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"time"

	"netease-music-mygo/internal/diskcache"
	"netease-music-mygo/internal/lyrics"
	"netease-music-mygo/internal/netease"
)

var errAMLLMissing = errors.New("AMLL lyrics not found")
var errAMLLUnavailable = errors.New("AMLL mirrors unavailable")

type lyricOption struct{ id, label string }

var lyricSources = []lyricOption{
	{"auto", "自动"},
	{"amll", "AMLL"},
	{"netease", "网易云"},
}

var lyricMirrors = []lyricOption{
	{"", "作者镜像"},
	{"bikonoo", "Bikonoo"},
	{"raw", "GitHub Raw"},
}

type amllMirror struct {
	url     string
	timeout time.Duration
}

var amllMirrors = map[string]amllMirror{
	"author":  {"https://amll-ttml-db.stevexmh.net/ncm/%d", 1800 * time.Millisecond},
	"bikonoo": {"https://amlldb.bikonoo.com/ncm-lyrics/%d.ttml", 3200 * time.Millisecond},
	"raw":     {"https://raw.githubusercontent.com/amll-dev/amll-ttml-db/refs/heads/main/ncm-lyrics/%d.ttml", 3 * time.Second},
}

var amllClient = &http.Client{
	CheckRedirect: func(req *http.Request, via []*http.Request) error {
		if len(via) >= 4 || req.URL.Scheme != "https" {
			return http.ErrUseLastResponse
		}
		switch req.URL.Host {
		case "raw.githubusercontent.com":
			if len(via) > 0 && via[0].URL.Host == req.URL.Host {
				return nil
			}
			return http.ErrUseLastResponse
		case "amll-ttml-db.stevexmh.net", "amlldb.bikonoo.com", "api.beta.amllhub.bikonoo.com":
			return nil
		default:
			return http.ErrUseLastResponse
		}
	},
}

func fetchAMLLMirror(ctx context.Context, mirror amllMirror, id int64) ([]byte, lyrics.Display, error) {
	ctx, cancel := context.WithTimeout(ctx, mirror.timeout)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, fmt.Sprintf(mirror.url, id), nil)
	if err != nil {
		return nil, lyrics.Display{}, err
	}
	res, err := netease.DoRequest(amllClient, req)
	if err != nil {
		return nil, lyrics.Display{}, err
	}
	defer res.Body.Close()
	switch res.StatusCode {
	case http.StatusNotFound:
		return nil, lyrics.Display{}, errAMLLMissing
	case http.StatusOK:
		data, err := io.ReadAll(io.LimitReader(res.Body, lyrics.MaxTTMLBytes+1))
		if err != nil {
			return nil, lyrics.Display{}, err
		}
		display, err := lyrics.ParseTTML(data, id)
		return data, display, err
	default:
		return nil, lyrics.Display{}, errAMLLUnavailable
	}
}

func fetchAMLL(ctx context.Context, node string, id int64) ([]byte, lyrics.Display, error) {
	if id <= 0 {
		return nil, lyrics.Display{}, errAMLLMissing
	}
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	order := []string{"author", "bikonoo"}
	switch node {
	case "bikonoo":
		order = []string{"bikonoo", "author"}
	case "raw":
		order = []string{"raw"}
	}
	allMissing := true
	for _, backend := range order {
		data, display, err := fetchAMLLMirror(ctx, amllMirrors[backend], id)
		if err == nil {
			return data, display, nil
		}
		if ctx.Err() != nil {
			return nil, lyrics.Display{}, ctx.Err()
		}
		if errors.Is(err, netease.ErrOffline) {
			return nil, lyrics.Display{}, err
		}
		allMissing = allMissing && errors.Is(err, errAMLLMissing)
	}
	if allMissing {
		return nil, lyrics.Display{}, errAMLLMissing
	}
	return nil, lyrics.Display{}, errAMLLUnavailable
}

var amllRequest = fetchAMLL

func loadAMLL(ctx context.Context, store *diskcache.Store, node string, id int64, refresh func(lyrics.Display)) (lyrics.Display, error) {
	const scope = "amll"
	epoch := store.Epoch()
	key := fmt.Sprintf("ttml/%d", id)
	missKey := fmt.Sprintf("missing/%s/%d", node, id)
	fetch := amllRequest
	update := func() (lyrics.Display, error) {
		data, display, err := fetch(ctx, node, id)
		if ctx.Err() != nil {
			return lyrics.Display{}, ctx.Err()
		}
		if err == nil {
			store.Put(scope, key, data, epoch)
		} else if errors.Is(err, errAMLLMissing) {
			store.Put(scope, missKey, []byte{1}, epoch)
		}
		return display, err
	}
	_, missingAt := store.Read(scope, missKey)
	recentMiss := time.Since(missingAt) < 30*time.Minute
	if data, at := store.Read(scope, key); data != nil {
		if display, err := lyrics.ParseTTML(data, id); err == nil {
			if time.Since(at) >= 7*24*time.Hour && !recentMiss && refresh != nil && !netease.Offline() {
				go func() {
					if newer, err := update(); err == nil {
						refresh(newer)
					}
				}()
			}
			return display, nil
		}
	}
	if recentMiss {
		return lyrics.Display{}, errAMLLMissing
	}
	return update()
}
