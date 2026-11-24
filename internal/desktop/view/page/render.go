package page

import (
	"github.com/egoist/mygo/ui"

	"netease-music-mygo/internal/desktop/view/components"
	"netease-music-mygo/internal/navigation"
	"netease-music-mygo/internal/netease"
	"netease-music-mygo/internal/viewmodel"
)

// Renderers composes existing shared artwork, virtual rows and skeletons.
// Callbacks belong to the app lifetime and never retain a build-scoped element or Context.
type Renderers struct {
	Image              func(string, bool) components.Image
	Cover              func(*ui.Context, string, int, float32, float32) ui.Element
	CoverFill          func(*ui.Context, string, int, float32)
	Link               func(*ui.Context, string, ui.Color, ui.Color, navigation.Route)
	HeaderDescription  func(*ui.Context, string, string, float32)
	DescriptionSummary func(*ui.Context, string, string, float32, int) ui.Element
	TrackRows          func(*ui.Context, []netease.Track, bool, func(*ui.Context), ...func(*ui.Context)) bool
	TrackRowsMeta      func(*ui.Context, []netease.Track, bool, func(int) string, func(*ui.Context), ...func(*ui.Context)) bool
	Skeleton           func(*ui.Context, bool, func(*ui.Context))
	HeaderSkeleton     func(*ui.Context)
	CardsSkeleton      func(*ui.Context, int, float32, bool)
	PlaylistCard       func(*ui.Context, *netease.Playlist)
	AlbumGrid          func(*ui.Context, []netease.AlbumProfile)
	PodcastGrid        func(*ui.Context, int, func(int) *netease.Podcast)
	ArtistCard         func(*ui.Context, *netease.ArtistProfile)
	UserRow            func(*ui.Context, *netease.User)
	PodcastRow         func(*ui.Context, *netease.Podcast, float32)
	CoverCard          func(*ui.Context, string, string, float64, string, func()) bool
}

// pageFoot ends a list loaded a page at a time, asking next for the next
// page once it is within a screen of coming into view: where the last
// failed, it says so, with a button to try again.
func (a *Page) Foot(c *ui.Context, pg *viewmodel.Page, next func()) {
	foot := ui.Row(c.Key("foot")).Height(56).Gap(12).Center()
	foot.Children(func() {
		switch {
		case pg.Loading:
			ui.Text(c, "正在加载…").FontSize(12.5).TextColor(a.Palette.Muted)
		case pg.Error != "":
			ui.Text(c, pg.Error).FontSize(12.5).TextColor(a.Palette.Muted)
			if components.IconButton(c.Key("retry"), components.IconReload, 18, a.Palette.Muted, "重新加载内容").Tooltip("重新加载内容").Clicked() {
				next()
			}
		}
	})
	_, h := c.Size()
	if b := foot.Bounds(); pg.More && !pg.Loading && pg.Error == "" && b.W > 0 && b.Y < 2*h {
		next()
	}
}
