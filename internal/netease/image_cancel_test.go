/*
验证取消一个图片消费者不会中断其他消费者共享的下载。
*/

package netease

import (
	"bytes"
	"context"
	"errors"
	"net/http"
	"testing"
	"time"
)

// Canceling a download waiting for shared artwork returns promptly without
// aborting the same cover transfer that another UI consumer still needs.
func TestImageCacheConsumerCancellation(t *testing.T) {
	entered, release := make(chan struct{}), make(chan struct{})
	body := coverBytes(t)
	server, _ := imageFixture(t, func(w http.ResponseWriter, r *http.Request) {
		close(entered)
		<-release
		w.Header().Set("Cache-Control", "max-age=3600")
		w.Write(body)
	})
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	first := make(chan error, 1)
	go func() { _, err := FetchContext(ctx, server.URL+"/cover"); first <- err }()
	select {
	case <-entered:
	case <-time.After(5 * time.Second):
		close(release)
		t.Fatal("artwork did not start")
	}
	cancel()
	select {
	case err := <-first:
		if !errors.Is(err, context.Canceled) {
			close(release)
			t.Fatal(err)
		}
	case <-time.After(5 * time.Second):
		close(release)
		t.Fatal("canceled consumer blocked")
	}
	close(release)
	got, err := Fetch(server.URL + "/cover")
	if err != nil || !bytes.Equal(got, body) {
		t.Fatal("shared artwork was aborted", err)
	}
}
