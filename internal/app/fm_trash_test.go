/*
验证私人 FM 不喜欢操作的成功切换及失败反馈。
*/

package app

import (
	"errors"
	"testing"

	"github.com/egoist/mygo/ui"

	"netease-music-mygo/internal/netease"
	"netease-music-mygo/internal/player"
	radiovm "netease-music-mygo/internal/viewmodel/radio"
)

// Rejecting an FM song skips it immediately and reports a failed preference save only to its account.
func TestFMTrashFailureFeedback(t *testing.T) {
	for _, stale := range []bool{false, true} {
		name := "current account"
		if stale {
			name = "replaced account"
		}
		t.Run(name, func(t *testing.T) {
			a := newTestApp()
			posts := testPosts(a)
			a.account.Profile = &netease.Profile{UserID: 1}
			a.settings = defaultSettings()
			player.Cue(a.pl, []netease.Track{{ID: 42, Name: "FM song"}})
			fed := 0
			a.pl.AttachRadio(radiovm.FMRadio(netease.FMDefault), func(func([]netease.Track)) { fed++ })
			old := fmTrash
			t.Cleanup(func() { fmTrash = old })
			requested := make(chan int64, 1)
			result := make(chan error, 1)
			defer close(result)
			fmTrash = func(id int64) error { requested <- id; return <-result }
			tt := ui.NewTester(a.views.radio.Card, 1000, 600)
			tt.Frame()
			if err := tt.Click("不喜欢"); err != nil {
				t.Fatal(err)
			}
			if fed != 1 {
				t.Fatal("the next song was not requested immediately")
			}
			if got := wait(t, requested, "the FM preference request"); got != 42 {
				t.Fatalf("rejected song %d, want 42", got)
			}
			if stale {
				a.account.Generation++
			}
			result <- errors.New("preference refused")
			refreshPost(t, posts)
			if (a.toast != "") == stale {
				t.Fatalf("feedback %q, stale account %v", a.toast, stale)
			}
		})
	}
}

// A successful FM preference request keeps the immediate skip and produces no failure feedback.
func TestFMTrashSuccess(t *testing.T) {
	a := newTestApp()
	posts := testPosts(a)
	player.Cue(a.pl, []netease.Track{{ID: 42}})
	fed := 0
	a.pl.AttachRadio(radiovm.FMRadio(netease.FMDefault), func(func([]netease.Track)) { fed++ })
	old := fmTrash
	t.Cleanup(func() { fmTrash = old })
	done := make(chan int64, 1)
	fmTrash = func(id int64) error { done <- id; return nil }
	a.trashFM()
	if got := wait(t, done, "the saved FM preference"); got != 42 || fed != 1 {
		t.Fatalf("saved song %d and requested next song %d times", got, fed)
	}
	if len(posts) != 0 || a.toast != "" {
		t.Fatal("successful preference save reported an error")
	}
}
