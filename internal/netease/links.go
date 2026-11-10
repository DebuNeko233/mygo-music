package netease

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"time"
)

var (
	ErrInvalidLink     = errors.New("invalid NetEase link")
	ErrUnsupportedLink = errors.New("unsupported NetEase link")
	musicURLs          = regexp.MustCompile(`(?i)(?:https?://|orpheus://)[^\s<>"'，。；！？、（）【】《》「」『』]+`)
)

// MusicLink is a resource to open. Short links keep their URL until resolved;
// a playlist or album opens its page, while a song or program plays.
type MusicLink struct {
	Kind string
	ID   int64
	URL  string
}

// ParseMusicLink accepts a URL or the text NetEase copies when sharing.
// The bool distinguishes a bad NetEase link from an ordinary search query.
func ParseMusicLink(text string) (MusicLink, bool, error) {
	text = strings.TrimSpace(text)
	if len(text) > 16*1024 {
		lower := strings.ToLower(text)
		known := strings.Contains(lower, "orpheus://") || strings.Contains(lower, "music.163.com/") || strings.Contains(lower, "163cn.tv/")
		return MusicLink{}, known, ErrInvalidLink
	}
	if strings.HasPrefix(strings.ToLower(text), "music.163.com/") || strings.HasPrefix(strings.ToLower(text), "163cn.tv/") {
		text = "https://" + text
	}
	for _, raw := range musicURLs.FindAllString(text, -1) {
		raw = strings.TrimRight(raw, ").,;!?]}")
		if strings.HasPrefix(strings.ToLower(raw), "orpheus://") {
			link, err := parseOrpheus(raw[len("orpheus://"):])
			return link, true, err
		}
		u, err := url.Parse(raw)
		if err != nil {
			continue
		}
		if !musicHost(u.Hostname()) {
			continue
		}
		if !musicWebURL(u) {
			return MusicLink{}, true, ErrInvalidLink
		}
		if strings.EqualFold(u.Hostname(), "163cn.tv") {
			return MusicLink{Kind: "short", URL: u.String()}, true, nil
		}
		if strings.HasPrefix(u.Fragment, "/") {
			u, err = url.Parse(u.Fragment)
			if err != nil {
				return MusicLink{}, true, ErrInvalidLink
			}
		}
		path := strings.TrimPrefix(strings.Trim(u.Path, "/"), "m/")
		link, err := resourceLink(path, u.Query())
		return link, true, err
	}
	if strings.HasPrefix(strings.ToLower(text), "orpheus://") {
		return MusicLink{}, true, ErrInvalidLink
	}
	return MusicLink{}, false, nil
}

func parseOrpheus(raw string) (MusicLink, error) {
	// The desktop website sends Base64(JSON), not just song/ID paths.
	payload, err := url.PathUnescape(strings.TrimRight(raw, "/"))
	if err != nil {
		return MusicLink{}, ErrInvalidLink
	}
	for _, enc := range []*base64.Encoding{base64.StdEncoding, base64.RawStdEncoding, base64.URLEncoding, base64.RawURLEncoding} {
		data, err := enc.DecodeString(payload)
		if err != nil || len(data) == 0 || data[0] != '{' {
			continue
		}
		var msg struct {
			Type string          `json:"type"`
			ID   json.RawMessage `json:"id"`
			Cmd  string          `json:"cmd"`
		}
		if json.Unmarshal(data, &msg) != nil {
			return MusicLink{}, ErrInvalidLink
		}
		if msg.Cmd != "" && msg.Cmd != "play" && msg.Cmd != "open" {
			return MusicLink{}, ErrUnsupportedLink
		}
		id := string(msg.ID)
		if strings.HasPrefix(id, `"`) && json.Unmarshal(msg.ID, &id) != nil {
			return MusicLink{}, ErrInvalidLink
		}
		return resourceLink(msg.Type, url.Values{"id": {id}})
	}
	u, err := url.Parse("orpheus://" + raw)
	if err != nil || u.User != nil || u.Port() != "" {
		return MusicLink{}, ErrInvalidLink
	}
	if cmd := u.Query().Get("cmd"); cmd != "" && cmd != "play" && cmd != "open" {
		return MusicLink{}, ErrUnsupportedLink
	}
	return resourceLink(u.Host+"/"+strings.Trim(u.Path, "/"), u.Query())
}

func resourceLink(path string, q url.Values) (MusicLink, error) {
	parts := strings.Split(strings.Trim(path, "/"), "/")
	kind := strings.ToLower(parts[0])
	if kind == "user" && len(parts) > 1 && parts[1] == "home" {
		parts = append(parts[:1], parts[2:]...)
	}
	if kind == "dj" || kind == "radio" {
		kind = "djradio"
	}
	switch kind {
	case "song", "playlist", "album", "artist", "user", "djradio", "program":
	default:
		return MusicLink{}, ErrUnsupportedLink
	}
	id := q.Get("id")
	if len(q["id"]) > 1 {
		return MusicLink{}, ErrInvalidLink
	}
	if len(parts) == 2 {
		if id != "" && id != parts[1] {
			return MusicLink{}, ErrInvalidLink
		}
		id = parts[1]
	} else if len(parts) > 2 {
		return MusicLink{}, ErrInvalidLink
	}
	if id == "" {
		return MusicLink{}, ErrInvalidLink
	}
	for _, ch := range id {
		if ch < '0' || ch > '9' {
			return MusicLink{}, ErrInvalidLink
		}
	}
	n, err := strconv.ParseInt(id, 10, 64)
	if err != nil || n <= 0 {
		return MusicLink{}, ErrInvalidLink
	}
	return MusicLink{Kind: kind, ID: n}, nil
}

func musicHost(host string) bool {
	switch strings.ToLower(host) {
	case "music.163.com", "www.music.163.com", "y.music.163.com", "163cn.tv":
		return true
	}
	return false
}

func musicWebURL(u *url.URL) bool {
	return musicHost(u.Hostname()) && u.User == nil && (u.Scheme == "https" || u.Scheme == "http") &&
		(u.Port() == "" || u.Scheme == "https" && u.Port() == "443" || u.Scheme == "http" && u.Port() == "80")
}

// ResolveMusicLink follows only NetEase redirects, without fetching the
// final resource page or sending the listener's session cookies.
func ResolveMusicLink(ctx context.Context, link MusicLink) (MusicLink, error) {
	ctx, cancel := context.WithTimeout(ctx, 8*time.Second)
	defer cancel()
	client := &http.Client{Timeout: 8 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	for range 5 {
		if link.Kind != "short" {
			return link, nil
		}
		u, err := url.Parse(link.URL)
		if err != nil || !musicWebURL(u) {
			return MusicLink{}, ErrInvalidLink
		}
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, u.String(), nil)
		if err != nil {
			return MusicLink{}, err
		}
		res, err := DoRequest(client, req)
		if err != nil {
			return MusicLink{}, err
		}
		res.Body.Close()
		if res.StatusCode < 300 || res.StatusCode >= 400 {
			return MusicLink{}, ErrInvalidLink
		}
		next, err := res.Location()
		if err != nil || !musicWebURL(next) {
			return MusicLink{}, ErrInvalidLink
		}
		var ok bool
		link, ok, err = ParseMusicLink(next.String())
		if err != nil {
			return MusicLink{}, err
		}
		if !ok {
			return MusicLink{}, ErrInvalidLink
		}
	}
	if link.Kind != "short" {
		return link, nil
	}
	return MusicLink{}, ErrInvalidLink
}

// MusicLinkTrack loads metadata before touching the queue, so a missing
// or unavailable resource leaves the current music playing.
func MusicLinkTrack(ctx context.Context, link MusicLink) (Track, error) {
	if link.ID <= 0 {
		return Track{}, ErrInvalidLink
	}
	switch link.Kind {
	case "song":
		tracks, err := songDetails(ctx, []int64{link.ID})
		if err != nil {
			return Track{}, err
		}
		for _, tr := range tracks {
			if tr.ID == link.ID {
				return tr, nil
			}
		}
	case "program":
		var res struct {
			Program *struct {
				Program
				Radio *Podcast `json:"radio"`
			} `json:"program"`
		}
		if err := getContext(ctx, "/api/dj/program/detail", url.Values{"id": {strconv.FormatInt(link.ID, 10)}}, &res); err != nil {
			return Track{}, err
		}
		if p := res.Program; p != nil && p.ID == link.ID && p.Radio != nil && p.Radio.ID > 0 && p.MainSong.ID > 0 {
			return p.Track(p.Radio), nil
		}
	}
	return Track{}, fmt.Errorf("netease: resource %s/%d not found", link.Kind, link.ID)
}
