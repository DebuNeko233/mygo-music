package netease

import (
	"fmt"
	"net/http"
	"slices"
	"strconv"
	"strings"
	"testing"
)

// UserPlaylists returns all of them, not the first page of them: asked for
// 200 with no more asked for, a listener with more than 200 playlists had the
// rest missing from the library.
func TestUserPlaylistsAsksForThemAll(t *testing.T) {
	const total = 437
	var offsets []string
	answer(t, func(req *http.Request) string {
		q := req.URL.Query()
		offset, _ := strconv.Atoi(q.Get("offset"))
		limit, _ := strconv.Atoi(q.Get("limit"))
		offsets = append(offsets, q.Get("offset"))
		var ps []string
		for i := offset; i < min(offset+limit, total); i++ {
			ps = append(ps, fmt.Sprintf(`{"id":%d,"name":"p%d","subscribed":%t}`, i, i, i%10 == 0))
		}
		return `{"code":200,"playlist":[` + strings.Join(ps, ",") + `]}`
	})

	made, saved, err := UserPlaylists(1)
	if err != nil {
		t.Fatal(err)
	}
	if want := []string{"0", "200", "400"}; !slices.Equal(offsets, want) {
		t.Errorf("asked for offsets %v, wanted %v", offsets, want)
	}
	if len(made)+len(saved) != total {
		t.Fatalf("got %d playlists, wanted %d", len(made)+len(saved), total)
	}
	// Every tenth was saved, the liked songs among them, which came first.
	if len(saved) != 44 || len(made) != total-44 {
		t.Errorf("%d saved and %d made, wanted 44 and %d", len(saved), len(made), total-44)
	}
	if last := made[len(made)-1]; last.ID != total-1 {
		t.Errorf("last playlist %d, wanted %d", last.ID, total-1)
	}
}
