package netease

import (
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

// Disconnect cancels a request still waiting for headers; reconnect creates
// a new request context rather than reviving the cancelled request.
func TestOfflineCancelsRequestAndAllowsReconnect(t *testing.T) {
	SetOffline(false)
	t.Cleanup(func() { SetOffline(false) })
	entered := make(chan struct{})
	client := &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
		close(entered)
		<-r.Context().Done()
		return nil, r.Context().Err()
	})}
	done := make(chan error, 1)
	go func() {
		req, _ := http.NewRequest("GET", "https://example.test", nil)
		_, err := DoRequest(client, req)
		done <- err
	}()
	<-entered
	SetOffline(true)
	select {
	case err := <-done:
		if !errors.Is(err, ErrOffline) {
			t.Fatalf("wrong cancellation: %v", err)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("disconnected request kept buffering")
	}
	req, _ := http.NewRequest("GET", "https://example.test", nil)
	if _, err := DoRequest(client, req); !errors.Is(err, ErrOffline) {
		t.Fatal("new offline request reached transport")
	}
	SetOffline(false)
	client.Transport = roundTripFunc(func(*http.Request) (*http.Response, error) {
		return &http.Response{StatusCode: 200, Header: make(http.Header), Body: io.NopCloser(strings.NewReader("online"))}, nil
	})
	res, err := DoRequest(client, req)
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	b, err := io.ReadAll(res.Body)
	if err != nil || string(b) != "online" {
		t.Fatal("new connection reused cancelled context")
	}
}

// Receiving headers does not end cancellation coverage: a stalled body read
// is unblocked when the system reports disconnection.
func TestOfflineCancelsResponseBody(t *testing.T) {
	SetOffline(false)
	t.Cleanup(func() { SetOffline(false) })
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(200)
		w.(http.Flusher).Flush()
		<-r.Context().Done()
	}))
	defer server.Close()
	req, _ := http.NewRequest("GET", server.URL, nil)
	res, err := DoRequest(server.Client(), req)
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	done := make(chan error, 1)
	go func() { _, err := io.ReadAll(res.Body); done <- err }()
	SetOffline(true)
	select {
	case err := <-done:
		if !errors.Is(err, ErrOffline) {
			t.Fatalf("wrong body error: %v", err)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("body kept buffering")
	}
}

// A fresh image cache hit remains readable with the network gate closed.
func TestOfflineImageStillUsesDisk(t *testing.T) {
	SetOffline(false)
	t.Cleanup(func() { SetOffline(false) })
	body := coverBytes(t)
	calls := 0
	server, _ := imageFixture(t, func(w http.ResponseWriter, r *http.Request) { calls++; w.Write(body) })
	if _, err := Fetch(server.URL); err != nil {
		t.Fatal(err)
	}
	SetOffline(true)
	if _, err := Fetch(server.URL); err != nil {
		t.Fatal("offline gate blocked local image")
	}
	if calls != 1 {
		t.Fatal("offline image hit the network")
	}
}
