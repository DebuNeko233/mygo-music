/*
验证播客订阅同步、延迟播放结果及首页请求的账号隔离。
*/

package app

import (
	"slices"
	"testing"

	"github.com/egoist/mygo/ui"

	"netease-music-mygo/internal/features/netease"
	"netease-music-mygo/internal/features/player"
	"netease-music-mygo/internal/ui/viewmodel"
	podcastvm "netease-music-mygo/internal/ui/viewmodel/podcast"
)

// Unsubscribing in the detail immediately updates the already loaded personal page without a second subscription cache.
func TestPodcastMVVMSubscriptionUpdatesPersonalPage(t *testing.T) {
	a := ownPageApp()
	posts := testPosts(a)
	pod := a.podcasts.Home.Subs[0]
	pod.Subed = true
	pod.SubCount = 10
	d := &podcastvm.PodcastPage{Podcast: &pod, Page: viewmodel.Page{Loaded: true}}
	a.podcasts.Pages.Set(pod.ID, d)
	a.nav.Current = route{Page: pagePodcast, ID: pod.ID}
	old := subscribePodcast
	t.Cleanup(func() { subscribePodcast = old })
	subscribePodcast = func(int64, bool) error { return nil }
	personal := false
	tt := ui.NewTester(func(c *ui.Context) {
		a.pal = paletteFor(c.Theme())
		if personal {
			a.views.user.Build(c)
		} else {
			a.views.podcast.Build(c)
		}
	}, 1200, 1200)
	tt.Frame()
	if err := tt.Click("已订阅"); err != nil {
		t.Fatal(err)
	}
	if slices.ContainsFunc(a.podcasts.Home.Subs, func(p netease.Podcast) bool { return p.ID == pod.ID }) {
		t.Fatal("detail kept a separate subscription list")
	}
	personal = true
	a.nav.Current = route{Page: pageUser, ID: 9}
	tt.Frame()
	if tt.HasText(pod.Name) {
		t.Fatal("personal page still displayed the removed subscription")
	}
	refreshPost(t, posts)
	if a.podcasts.IsSubscribed(&pod) || d.Subscribing {
		t.Fatal("subscription completion restored removed data")
	}
}

// A podcast card whose data arrives late cannot replace a queue the listener explicitly selected meanwhile.
func TestPodcastMVVMLatePlayKeepsPlayerQueue(t *testing.T) {
	a := newTestApp()
	posts := testPosts(a)
	oldDetail, oldPrograms := podcastDetail, podcastPrograms
	t.Cleanup(func() { podcastDetail, podcastPrograms = oldDetail, oldPrograms })
	podcastDetail = func(id int64) (*netease.Podcast, error) { return &netease.Podcast{ID: id}, nil }
	podcastPrograms = func(int64, int, int) ([]netease.Program, bool, error) {
		p := netease.Program{ID: 10}
		p.MainSong.ID = 100
		return []netease.Program{p}, false, nil
	}
	a.playPodcast(7)
	first, second := wait(t, posts, "metadata or episodes"), wait(t, posts, "other playback input")
	player.Cue(a.pl, []netease.Track{{ID: 99, Name: "new choice"}})
	revision := a.pl.QueueRevision()
	first()
	second()
	if a.pl.QueueRevision() != revision || len(a.pl.Queue()) != 1 || a.pl.Current() == nil || a.pl.Current().ID != 99 {
		t.Fatal("late podcast load replaced the new Player queue")
	}
}

// Re-entering the same account rejects every old home response before its loading flags or private data can change.
func TestPodcastMVVMHomeRepliesRespectAccountGeneration(t *testing.T) {
	a := newTestApp()
	posts := testPosts(a)
	a.account.Profile = &netease.Profile{UserID: 7}
	oldSubs, oldPicks, oldHot := subscribedPodcasts, recommendedPodcasts, hotPodcasts
	t.Cleanup(func() { subscribedPodcasts, recommendedPodcasts, hotPodcasts = oldSubs, oldPicks, oldHot })
	subscribedPodcasts = func() ([]netease.Podcast, error) { return []netease.Podcast{{ID: 1}}, nil }
	recommendedPodcasts = func() ([]netease.Podcast, error) { return []netease.Podcast{{ID: 2}}, nil }
	hotPodcasts = func(int, int) ([]netease.Podcast, bool, error) { return []netease.Podcast{{ID: 3}}, false, nil }
	a.loadPodcastTab()
	first, second := wait(t, posts, "home reply"), wait(t, posts, "subscriptions reply")
	a.account.Generation += 2
	first()
	second()
	if !a.podcasts.Home.Loading || !a.podcasts.Home.SubscriptionLoading || len(a.podcasts.Home.Subs) != 0 || len(a.podcasts.Home.Hot) != 0 {
		t.Fatal("old login's reply reached the new session")
	}
}
