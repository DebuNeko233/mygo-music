/*
管理自选背景图片的加载与预热，并根据外观设置生成界面配色。
*/

package app

import (
	"slices"

	"github.com/egoist/mygo/ui"

	artcache "netease-music-mygo/internal/ui/view/artwork"
	"netease-music-mygo/internal/ui/view/components"
)

// accentChosen is the accent the listener chose, empty for the one its
// appearance gives. Set from the settings, on the main thread, before the
// pages are built.
var accentChosen string

// addPictures adds those of files a page can stand on to its pictures,
// once each, and keeps the settings.
func (a *app) addPictures(files []string, paths *[]string) {
	added := false
	for _, path := range files {
		if !artcache.IsPicture(path) || slices.Contains(*paths, path) {
			continue
		}
		*paths = append(*paths, path)
		added = true
	}
	if added {
		a.changed()
	}
}

// Prewarm only the two selected surfaces, independently of configured count.
func (a *app) preloadPictures() {
	a.homeBackdrop.Visible = true
	a.homePicture()
	a.playingPicture()
}

func (a *app) homePicture() *artcache.Picture {
	return a.picture(&a.homeBackdrop, a.settings.BackdropHome)
}
func (a *app) playingPicture() *artcache.Picture {
	return a.picture(&a.playingBackdrop, a.settings.BackdropPlaying)
}

func (a *app) picture(slot *artcache.PictureSlot, paths []string) *artcache.Picture {
	if a.pics == nil {
		a.pics = artcache.NewPictureCache(artcache.PictureCacheOptions{Post: func(fn func()) { a.dispatch(fn, false) }, PostResource: a.postResource})
	}
	var key, next int64
	hasNext := false
	if a.pl != nil {
		if tr := a.pl.Current(); tr != nil {
			key = tr.Key()
		}
		if tr, _ := a.pl.AdjacentTracks(); tr != nil {
			next = tr.Key()
			hasNext = next != key
		}
	}
	return a.pics.Select(slot, paths, key, next, hasNext)
}

func (a *app) forgetPictures() {
	if a.pics != nil {
		a.homePicture()
		a.playingPicture()
		a.pics.Retain(slices.Concat(a.settings.BackdropHome, a.settings.BackdropPlaying))
	}
}

func paletteFor(t *ui.Theme) components.Palette {
	return components.PaletteFor(t, components.AccentColor(accentChosen, t.Dark))
}

func (s *settings) uiTheme(base *ui.Theme) ui.Theme {
	theme := *base
	if s.UIFont != "" {
		theme.Font = s.UIFont
	}
	return theme
}
