package app

import (
	"strings"
	"testing"
	"time"

	"github.com/egoist/mygo/ui"

	"netease-music-mygo/internal/netease"
	"netease-music-mygo/internal/viewmodel"
)

// A long introduction on any detail page opens in a dialog that
// scrolls through the original paragraphs and can be dismissed.
func TestDetailDescriptionCanReadAndDismissFullText(t *testing.T) {
	text := strings.Repeat("这是一段介绍，包含发行背景和作品信息。\n\n", 30) + "最后一段。"
	for _, tc := range []struct {
		name  string
		page  pageKind
		title string
	}{
		{"artist", pageArtist, "歌手名称 · 简介"},
		{"album", pageAlbum, "专辑名称 · 简介"},
		{"playlist", pagePlaylist, "歌单名称 · 简介"},
		{"podcast", pagePodcast, "播客名称 · 简介"},
		{"user", pageUser, "用户名称 · 个人签名"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			a := newTestApp()
			a.settings = defaultSettings()
			a.route = route{page: tc.page, id: 77}
			a.artists.Pages.Set(77, &viewmodel.ArtistPage{Profile: &netease.ArtistProfile{Artist: netease.Artist{ID: 77, Name: "歌手名称"}, BriefDesc: text}})
			a.albums.Pages.Set(77, &viewmodel.AlbumPage{Album: &netease.AlbumProfile{Album: netease.Album{ID: 77, Name: "专辑名称"}, Description: text}})
			a.library.Playlists[77] = &netease.Playlist{ID: 77, Name: "歌单名称", Description: text}
			a.podcasts.Set(77, &podcastData{podcast: &netease.Podcast{ID: 77, Name: "播客名称", Description: text}})
			a.users.Pages.Set(77, &viewmodel.UserPage{User: &netease.User{UserID: 77, Nickname: "用户名称", Signature: text}})
			view := ui.NewTester(a.view, 1180, 780)
			more := "更多：" + tc.title
			if err := view.Click(more); err != nil {
				t.Fatal(err)
			}
			if !a.intro.open || a.intro.title != tc.title || a.intro.text != text || a.intro.scroll.MaxY <= 0 {
				t.Fatal("the complete introduction did not open in a scrolling dialog")
			}
			closeButton, ok := view.Find("关闭")
			if !ok {
				t.Fatal("the introduction dialog has no close button")
			}
			view.Scroll(closeButton.X-120, closeButton.Y+120, 0, 10000)
			if a.intro.scroll.Y != a.intro.scroll.MaxY {
				t.Fatal("scrolling did not reach the end of the introduction")
			}
			view.Key(0, ui.KeyEscape)
			if a.intro.open {
				t.Fatal("Escape left the introduction open")
			}
			if err := view.Click(more); err != nil {
				t.Fatal(err)
			}
			if a.intro.scroll.Y != 0 {
				t.Fatal("reopening the introduction did not return to its start")
			}
			if err := view.Click("关闭"); err != nil {
				t.Fatal(err)
			}
			if a.intro.open {
				t.Fatal("the close button left the introduction open")
			}
		})
	}
}

// Resizing a detail page exposes the reading button only while the preview is cut short.
func TestDetailDescriptionMoreFollowsAvailableWidth(t *testing.T) {
	a := newTestApp()
	a.settings = defaultSettings()
	a.route = route{page: pageAlbum, id: 77}
	a.albums.Pages.Set(77, &viewmodel.AlbumPage{Album: &netease.AlbumProfile{
		Album: netease.Album{ID: 77, Name: "专辑名称"}, Description: strings.Repeat("这是一段专辑介绍。", 10),
	}})
	view := ui.NewTester(a.view, 1420, 780)
	const more = "更多：专辑名称 · 简介"
	if _, ok := view.Find(more); ok {
		t.Fatal("the full preview already fits at the wider size")
	}
	view.SetSize(880, 780)
	if _, ok := view.Find(more); !ok {
		t.Fatal("the narrower preview is cut short without a way to read the rest")
	}
	view.SetSize(1420, 780)
	if _, ok := view.Find(more); ok {
		t.Fatal("the button remained after the full preview fit again")
	}
}

// More in a search result reads that item's full description without opening its page.
func TestDescriptionMoreInSearchResults(t *testing.T) {
	const short = "简短。"
	long := strings.Repeat("这段介绍很长。", 25) + "\n\n最后一段。"
	for _, tc := range []struct {
		kind        netease.SearchType
		description string
	}{
		{netease.SearchPodcasts, "简介"},
		{netease.SearchUsers, "个人签名"},
	} {
		t.Run(tc.description, func(t *testing.T) {
			a := newTestApp()
			a.settings = defaultSettings()
			r := route{page: pageSearch, query: "测试", kind: tc.kind}
			a.route = r
			d := &searchData{page: viewmodel.Page{Loaded: true}}
			if tc.kind == netease.SearchPodcasts {
				d.res.Podcasts = []netease.Podcast{{ID: 1, Name: "甲", Description: short}, {ID: 2, Name: "乙", Description: long}}
			} else {
				d.res.Users = []netease.User{{UserID: 1, Nickname: "甲", Signature: short}, {UserID: 2, Nickname: "乙", Signature: long}}
			}
			a.searches.Set(r, d)
			view := ui.NewTester(a.view, 1180, 780)
			if _, ok := view.Find("更多：甲 · " + tc.description); ok {
				t.Fatal("the short description already fits")
			}
			if err := view.Click("更多：乙 · " + tc.description); err != nil {
				t.Fatal(err)
			}
			if a.route != r || !a.intro.open || a.intro.text != long {
				t.Fatal("More changed the page or opened the wrong description")
			}
		})
	}
}

// A podcast in a grid uses its own column width to decide if its preview fits.
func TestDescriptionMoreInPodcastGrid(t *testing.T) {
	a := newTestApp()
	a.settings = defaultSettings()
	a.account.Profile = &netease.Profile{UserID: 1}
	a.route, a.recent.Selected = route{page: pageRecent}, viewmodel.RecentPodcasts
	for i := range a.recent.Tabs {
		a.recent.Tabs[i].At = a.listsNow()
	}
	a.recent.Tabs[viewmodel.RecentPodcasts].Shown = true
	desc := strings.Repeat("播客介绍", 12)
	a.recent.Podcasts = []netease.Played[netease.Podcast]{{Item: netease.Podcast{ID: 2, Name: "播客名称", Description: desc}}}
	view := ui.NewTester(a.view, 1180, 780)
	if err := view.Click("更多：播客名称 · 简介"); err != nil {
		t.Fatal(err)
	}
	if !a.intro.open || a.intro.text != desc || a.route.page != pageRecent {
		t.Fatal("More did not open the description over the podcast grid")
	}
}

// A long episode introduction also opens the reader from Now Playing.
func TestDescriptionMoreInNowPlaying(t *testing.T) {
	a := newTestApp()
	a.settings = defaultSettings()
	desc := strings.Repeat("这是本期节目的详细介绍。\n\n", 30) + "最后一段。"
	tr := netease.Track{ID: 1, ProgramID: 2, Name: "节目名称", Description: desc}
	a.pl.Cue([]netease.Track{tr})
	a.comments.track = tr.ID
	a.npPane, a.nowPlaying = paneProgram, true
	view := ui.NewTester(a.view, 1180, 780)
	view.Advance(time.Second)
	if err := view.Click("更多：节目名称 · 简介"); err != nil {
		t.Fatal(err)
	}
	if !a.nowPlaying || !a.intro.open || a.intro.text != desc {
		t.Fatal("the full episode introduction did not open over Now Playing")
	}
	view.Key(0, ui.KeyEscape)
	if a.intro.open || !a.nowPlaying {
		t.Fatal("Escape should close the description and keep Now Playing open")
	}
}
