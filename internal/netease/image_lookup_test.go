package netease

import (
	"bytes"
	"errors"
	"net/http"
	"sync"
	"testing"
	"time"

	"netease-music-mygo/internal/diskcache"
)

type heldImageStore struct {
	*diskcache.Store
	key              string
	entered, release chan struct{}
	once             sync.Once
}

func (s *heldImageStore) Read(scope, key string) ([]byte, time.Time) {
	if scope == "requests" && key == s.key {
		s.once.Do(func() { close(s.entered) })
		<-s.release
	}
	return s.Store.Read(scope, key)
}

// A stalled disk lookup must not hold the flight lock and delay unrelated covers.
func TestImageLookupDoesNotBlockOtherFlights(t *testing.T) {
	body := coverBytes(t)
	server, store := imageFixture(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "max-age=3600")
		w.Write(body)
	})
	req, _ := http.NewRequest("GET", server.URL+"/held", nil)
	req.Header.Set("User-Agent", UserAgent)
	held := &heldImageStore{Store: store, key: imageIdentity(req), entered: make(chan struct{}), release: make(chan struct{})}
	images.Load().store = held
	var release sync.Once
	unblock := func() { release.Do(func() { close(held.release) }) }
	defer unblock()
	first := make(chan error, 1)
	go func() { _, err := Fetch(req.URL.String()); first <- err }()
	<-held.entered
	second := make(chan error, 1)
	go func() {
		b, err := Fetch(server.URL + "/other")
		if err == nil && !bytes.Equal(b, body) {
			err = errors.New("wrong cover")
		}
		second <- err
	}()
	finished := false
	select {
	case err := <-second:
		finished = true
		if err != nil {
			t.Error(err)
		}
	case <-time.After(2 * time.Second):
		t.Error("another cover waited for the blocked disk lookup")
	}
	unblock()
	if err := <-first; err != nil {
		t.Fatal(err)
	}
	// Drain a timed-out second request before restoring global dependencies.
	if !finished {
		if err := <-second; err != nil {
			t.Error(err)
		}
	}
}
