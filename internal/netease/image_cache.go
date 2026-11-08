package netease

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	_ "golang.org/x/image/webp"
	"image"
	_ "image/gif"
	_ "image/jpeg"
	_ "image/png"
	"io"
	"net"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"netease-music-mygo/internal/diskcache"
)

type imageRecord struct {
	Content, ETag, Modified string
	Expires                 time.Time
	Revalidate              bool
	NoCache                 bool
	FreshFor                time.Duration
	Width, Height           int
}
type imageFlight struct {
	done  chan struct{}
	body  []byte
	err   error
	epoch uint64
}
type imageCache struct {
	familyMu sync.Mutex
	sem      chan struct{}
	store    *diskcache.Store
	changed  func(string)
	mu       sync.Mutex
	flights  map[string]*imageFlight
}

var images atomic.Pointer[imageCache]

// UseImageCache installs the shared byte cache for UI and system media covers.
// Requests retain their complete URL and cookie identity; neither is persisted.
func UseImageCache(store *diskcache.Store, changed func(string)) {
	if store == nil {
		images.Store(nil)
		return
	}
	images.Store(&imageCache{store: store, changed: changed, flights: map[string]*imageFlight{}, sem: make(chan struct{}, 8)})
}
func imageIdentity(req *http.Request) string {
	key := req.URL.String() + "\n" + req.Header.Get("User-Agent")
	if httpClient.Jar != nil {
		for _, cookie := range httpClient.Jar.Cookies(req.URL) {
			key += "\n" + cookie.String()
		}
	}
	return key
}

func (c *imageCache) fetch(req *http.Request) ([]byte, error) {
	epoch := c.store.Epoch()
	key := imageIdentity(req)
	c.mu.Lock() // Cache lookup and flight registration form one atomic decision.
	var record imageRecord
	raw, _ := c.store.Read("requests", key)
	var body []byte
	if json.Unmarshal(raw, &record) == nil {
		body, _ = c.store.Read("content", record.Content)
	}
	public := httpClient.Jar == nil || len(httpClient.Jar.Cookies(req.URL)) == 0
	if body == nil && public {
		if family, n, ok := imageFamily(req); ok {
			data, _ := c.store.Read("families", family)
			var larger imageRecord
			if json.Unmarshal(data, &larger) == nil && larger.Width >= n && larger.Height >= n {
				body, _ = c.store.Read("content", larger.Content)
				record = larger
				// Validators of a 600px response do not validate an 80px request.
				record.ETag, record.Modified = "", ""
			}
		}
	}
	if body != nil && !record.NoCache && time.Now().Before(record.Expires) {
		c.mu.Unlock()
		return body, nil
	}
	// Only public responses which permit stale reuse get the immediate fallback.

	if body != nil && !record.Revalidate && public {
		if c.flights[key] == nil {
			f := &imageFlight{done: make(chan struct{}), epoch: epoch}
			c.flights[key] = f
			go c.refresh(req, key, record, body, f)
		}
		c.mu.Unlock()
		return body, nil
	}
	f := c.flights[key]
	if f == nil {
		f = &imageFlight{done: make(chan struct{}), epoch: epoch}
		c.flights[key] = f
		go c.refresh(req, key, record, body, f)
	}
	c.mu.Unlock()
	<-f.done
	return f.body, f.err
}

func (c *imageCache) refresh(req *http.Request, key string, old imageRecord, previous []byte, f *imageFlight) {
	c.sem <- struct{}{}
	defer func() { <-c.sem }()
	epoch := f.epoch
	defer func() { c.mu.Lock(); delete(c.flights, key); close(f.done); c.mu.Unlock() }()
	if previous != nil {
		if old.ETag != "" {
			req.Header.Set("If-None-Match", old.ETag)
		}
		if old.Modified != "" {
			req.Header.Set("If-Modified-Since", old.Modified)
		}
	}
	response, err := DoRequest(httpClient, req)
	if err != nil {
		f.err = err
		return
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK && response.StatusCode != http.StatusNotModified {
		f.err = coverHTTPError{response.StatusCode, response.Status}
		switch response.StatusCode {
		case 401, 403, 404, 410:
			if c.store.Epoch() != epoch {
				return
			}
			c.store.Forget("requests", key)
			if family, _, ok := imageFamily(req); ok {
				c.store.Forget("families", family)
			}
			if c.changed != nil {
				c.changed(req.URL.String())
			}
		}
		return
	}
	body := previous
	width, height := old.Width, old.Height
	if response.StatusCode == http.StatusOK {
		body, err = io.ReadAll(io.LimitReader(response.Body, 16<<20+1))
		if err == nil && len(body) > 16<<20 {
			err = errors.New("cover exceeds 16 MiB")
		}
		if err == nil {
			var config image.Config
			config, _, err = image.DecodeConfig(bytes.NewReader(body))
			width, height = config.Width, config.Height
			if err == nil && (config.Width > 16384 || config.Height > 16384 || int64(config.Width)*int64(config.Height) > 16<<20) {
				err = errors.New("cover dimensions exceed limit")
			}
		}
		if err != nil {
			f.err = err
			return
		}
	}
	if body == nil {
		f.err = errors.New("304 without a cached cover")
		return
	}
	sum := sha256.Sum256(body)
	record := imageRecord{Width: width, Height: height, Content: hex.EncodeToString(sum[:]), ETag: response.Header.Get("ETag"), Modified: response.Header.Get("Last-Modified")}
	if response.StatusCode == http.StatusNotModified {
		if record.ETag == "" {
			record.ETag = old.ETag
		}
		if record.Modified == "" {
			record.Modified = old.Modified
		}
	}
	allowed := imagePolicy(response.Header, &record) && len(response.Cookies()) == 0
	if response.StatusCode == http.StatusNotModified && response.Header.Get("Cache-Control") == "" {
		record.Revalidate = old.Revalidate
		record.NoCache = old.NoCache
		record.FreshFor = old.FreshFor
		record.Expires = time.Now().Add(old.FreshFor)
	}
	if allowed {
		c.store.Put("content", record.Content, body, epoch)
		data, _ := json.Marshal(record)
		c.store.Put("requests", key, data, epoch)
		if family, _, ok := imageFamily(req); ok && (httpClient.Jar == nil || len(httpClient.Jar.Cookies(req.URL)) == 0) {
			c.familyMu.Lock()
			b, _ := c.store.Read("families", family)
			var largest imageRecord
			if json.Unmarshal(b, &largest) != nil || width >= largest.Width && height >= largest.Height {
				c.store.Put("families", family, data, epoch)
			}
			c.familyMu.Unlock()
		}
	} else if c.store.Epoch() == epoch {
		c.store.Forget("requests", key)
		if family, _, ok := imageFamily(req); ok {
			c.store.Forget("families", family)
		}
		if previous != nil && c.changed != nil {
			c.changed(req.URL.String())
		}
	}
	f.body = body
	if previous != nil && !bytes.Equal(previous, body) && c.changed != nil && c.store.Epoch() == epoch {
		c.changed(req.URL.String())
	}
}

// Cache freshness is distinct from offline reuse. no-store/private/Vary:*
// never reach disk; no-cache/must-revalidate require a successful validation.
func imagePolicy(header http.Header, record *imageRecord) bool {
	now := time.Now()
	record.Expires = now.Add(24 * time.Hour)
	allowed := true
	for _, v := range strings.Split(strings.Join(header.Values("Vary"), ","), ",") {
		if strings.TrimSpace(v) == "*" {
			allowed = false
		}
	}
	if expiry, err := http.ParseTime(header.Get("Expires")); err == nil {
		record.Expires = expiry
	}
	age, _ := strconv.Atoi(header.Get("Age"))
	base := now
	if date, err := http.ParseTime(header.Get("Date")); err == nil {
		base = date
	}
	for _, part := range strings.Split(strings.ToLower(strings.Join(header.Values("Cache-Control"), ",")), ",") {
		directive, value, _ := strings.Cut(strings.TrimSpace(part), "=")
		switch directive {
		case "no-store", "private":
			allowed = false
		case "no-cache":
			record.NoCache = true
			record.Revalidate = true
			record.Expires = now
		case "must-revalidate":
			record.Revalidate = true
		case "max-age":
			if seconds, err := strconv.Atoi(strings.Trim(value, "\"")); err == nil && seconds >= 0 {
				record.Expires = base.Add(time.Duration(seconds-age) * time.Second)
			}
		}
	}
	record.FreshFor = max(time.Duration(0), record.Expires.Sub(now))
	return allowed
}

// Only NetEase's square-size parameter on its public image CDN
// forms a reuse family. Other queries (including versions/signatures) stay
// exact URL requests, and cached dimensions must be sufficient for the view.
func imageFamily(req *http.Request) (string, int, bool) {
	host := strings.ToLower(req.URL.Hostname())
	if !strings.HasSuffix(host, ".music.126.net") {
		return "", 0, false
	}
	query := req.URL.Query()
	if len(query) != 1 || len(query["param"]) != 1 {
		return "", 0, false
	}
	x, y, ok := strings.Cut(query.Get("param"), "y")
	if !ok || x != y {
		return "", 0, false
	}
	n, err := strconv.Atoi(x)
	if err != nil || n <= 0 || n > 16384 || strconv.Itoa(n) != x {
		return "", 0, false
	}
	base := *req.URL
	base.RawQuery = ""
	return base.String(), n, true
}

type coverHTTPError struct {
	code   int
	status string
}

func (e coverHTTPError) Error() string { return e.status }

// RetryableCoverError separates missing/bad images from a lost connection.
func RetryableCoverError(err error) bool {
	var status coverHTTPError
	if errors.As(err, &status) {
		return status.code >= 500 || status.code == 408 || status.code == 429
	}
	var network net.Error
	return errors.Is(err, ErrOffline) || errors.As(err, &network) || errors.Is(err, io.ErrUnexpectedEOF) || errors.Is(err, io.EOF)
}
