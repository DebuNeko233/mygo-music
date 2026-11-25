/*
管理自选背景图片的加载与预热，并根据外观设置生成界面配色。
*/

package app

import (
	"slices"

	"github.com/egoist/mygo/ui"

	artcache "netease-music-mygo/internal/desktop/artwork"
	"netease-music-mygo/internal/desktop/view/components"
)

// accentChosen is the accent the listener chose, empty for the one its
// appearance gives. Set from the settings, on the main thread, before the
// pages are built.
var accentChosen string

// Each request owns an entry so a discarded read cannot overwrite a later
// request for the same path.
type backdropEntry struct {
	pic *artcache.Picture // nil while loading or if decoding failed
}

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

// preloadPictures starts reading the chosen backdrops before their first
// frame. The home and playing paths overlap at most once in the read queue.
func (a *app) preloadPictures() {
	for _, path := range slices.Concat(a.settings.BackdropHome, a.settings.BackdropPlaying) {
		a.loadPicture(path)
	}
}

func (a *app) loadPicture(path string) {
	if a.pics[path] != nil {
		return
	}
	if a.pics == nil {
		a.pics = make(map[string]*backdropEntry)
	}
	entry := &backdropEntry{}
	a.pics[path] = entry
	go a.readPicture(path, entry)
}

// pictures returns the pictures of the paths that could be read, skipping
// those that could not, and starts reading the rest.
func (a *app) pictures(paths []string) []*artcache.Picture {
	out := make([]*artcache.Picture, 0, len(paths))
	for _, path := range paths {
		if entry := a.pics[path]; entry == nil {
			a.loadPicture(path)
		} else if entry.pic != nil {
			out = append(out, entry.pic)
		}
	}
	return out
}

// Both pages can build in the same frame. Keep their configured pictures
// together so neither page forces the other to decode its pictures again.
func (a *app) forgetPictures() {
	for path := range a.pics {
		if !a.pictureChosen(path) {
			delete(a.pics, path)
		}
	}
}

func (a *app) pictureChosen(path string) bool {
	return slices.Contains(a.settings.BackdropHome, path) || slices.Contains(a.settings.BackdropPlaying, path)
}

// readPicture reads and decodes the picture at path, off the main thread,
// and keeps it, or that it cannot be read.
func (a *app) readPicture(path string, entry *backdropEntry) {
	pic := artcache.DecodePicture(path)
	a.postIfChanged(func() bool {
		if a.pics[path] != entry {
			return false
		}
		entry.pic = pic
		return pic != nil
	})
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
