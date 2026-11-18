package app

import (
	"strings"
	"testing"

	"github.com/egoist/mygo/ui"

	"netease-music-mygo/internal/netease"
)

// The long introduction on either detail page opens in a dialog that
// scrolls through the original paragraphs and can be dismissed.
func TestDetailDescriptionCanReadAndDismissFullText(t *testing.T) {
	text := strings.Repeat("这是一段介绍，包含发行背景和作品信息。\n\n", 30) + "最后一段。"
	for _, tc := range []struct {
		name string
		page pageKind
	}{
		{"artist", pageArtist},
		{"album", pageAlbum},
	} {
		t.Run(tc.name, func(t *testing.T) {
			a := newTestApp()
			a.settings = defaultSettings()
			a.route = route{page: tc.page, id: 77}
			a.artists.set(77, &artistData{profile: &netease.ArtistProfile{Artist: netease.Artist{ID: 77, Name: "歌手名称"}, BriefDesc: text}})
			a.albums.set(77, &albumData{album: &netease.AlbumProfile{Album: netease.Album{ID: 77, Name: "专辑名称"}, Description: text}})
			view := ui.NewTester(a.view, 1180, 780)
			if err := view.Click("更多"); err != nil {
				t.Fatal(err)
			}
			if !a.intro.open || a.intro.text != text || a.intro.scroll.MaxY <= 0 {
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
			if err := view.Click("更多"); err != nil {
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
	a.albums.set(77, &albumData{album: &netease.AlbumProfile{
		Album: netease.Album{ID: 77, Name: "专辑名称"}, Description: strings.Repeat("这是一段专辑介绍。", 10),
	}})
	view := ui.NewTester(a.view, 1420, 780)
	if _, ok := view.Find("更多"); ok {
		t.Fatal("the full preview already fits at the wider size")
	}
	view.SetSize(880, 780)
	if _, ok := view.Find("更多"); !ok {
		t.Fatal("the narrower preview is cut short without a way to read the rest")
	}
	view.SetSize(1420, 780)
	if _, ok := view.Find("更多"); ok {
		t.Fatal("the button remained after the full preview fit again")
	}
}
