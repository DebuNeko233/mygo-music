/*
按需构建歌曲、节目和队列菜单，并将菜单项连接到注入命令。
*/

package view

import (
	"github.com/egoist/mygo/ui"

	"netease-music-mygo/internal/features/navigation"
	"netease-music-mygo/internal/features/netease"
	"netease-music-mygo/internal/features/player"
	"netease-music-mygo/internal/ui/view/components"
)

type SongMenu struct {
	Player        func() *player.Player
	Copy          func(string) error
	LocalItems    func(*netease.Track, bool) func(*ui.Menu)
	Download      func([]netease.Track)
	OpenArtist    func(int64)
	OpenPage      func(navigation.Route)
	PlaylistItems func(*ui.Menu, *netease.Track, int64)
}

// songMenu is three dots that open the menu of tr.
func (a *SongMenu) Button(c *ui.Context, tr *netease.Track, size float32, color ui.Color) ui.Element {
	b := components.IconButton(c, components.IconMore, size, color, "更多")
	b.Menu(a.Items(tr, 0))
	return b
}

// songItems builds the menu of what can be done with tr, behind its dots
// or a right click on its row: copying its title, its title with its
// artists, or the link to its page, opening its artists' or album's, and,
// signed in, adding it to a playlist or taking it out of pid, the playlist
// it is listed in, if set.
func (a *SongMenu) Items(tr *netease.Track, pid int64) func(m *ui.Menu) {
	if tr.IsLocal() {
		return a.LocalItems(tr, false)
	}
	return func(m *ui.Menu) {
		if tr.ProgramID != 0 {
			a.ProgramItems(m, tr)
			return
		}
		if tr.Cloud {
			a.QueueItems(m, *tr, true, true)
			m.Separator()
		}
		if m.Item("下载").Chosen() {
			a.Download([]netease.Track{*tr})
		}
		if m.Item("复制歌名").Chosen() {
			a.Copy(tr.Name)
		}
		if m.Item("复制歌名和歌手").Chosen() {
			a.Copy(tr.Name + " - " + tr.ArtistNames())
		}
		// Private uploads have no public song link or catalogue playlist entry.
		if tr.Cloud {
			return
		}
		m.Separator()
		if m.Item("复制链接").Chosen() {
			a.Copy(tr.URL())
		}
		// Its artists' and album's pages, those NetEase has.
		linked := tr.Album.ID != 0
		for _, ar := range tr.Artists {
			linked = linked || ar.ID != 0
		}
		if linked {
			m.Separator()
		}
		for _, ar := range tr.Artists {
			if ar.ID != 0 && m.Item("查看歌手："+ar.Name).Chosen() {
				a.OpenArtist(ar.ID)
			}
		}
		if tr.Album.ID != 0 && m.Item("查看专辑："+tr.Album.Name).Chosen() {
			a.OpenPage(navigation.Route{Page: navigation.Album, ID: tr.Album.ID})
		}
		a.PlaylistItems(m, tr, pid)
	}
}

// ProgramItems builds the menu of an episode: copying its title or the
// link to its page, or opening its podcast's.
func (a *SongMenu) ProgramItems(m *ui.Menu, tr *netease.Track) {
	if m.Item("复制节目名").Chosen() {
		a.Copy(tr.Name)
	}
	if m.Item("复制链接").Chosen() {
		a.Copy(tr.URL())
	}
	m.Separator()
	if m.Item("查看播客").Chosen() {
		a.OpenPage(navigation.Route{Page: navigation.Podcast, ID: tr.PodcastID})
	}
}

// QueueItems uses the player's play order for both local and uploaded songs.
// It reports whether it added anything, for callers separating later actions.
func (a *SongMenu) QueueItems(m *ui.Menu, track netease.Track, available, allowRemove bool) bool {
	order, at := a.Player().PlayOrder()
	position := a.Player().QueuePosition(track.Key())
	scheduled := position >= 0 && position >= at
	canRemove := allowRemove && position >= 0 && position != at
	if available {
		if m.Item("下一首播放").Disabled(scheduled && position <= at+1).Chosen() {
			a.Player().Enqueue(track, true)
		}
	}
	if canRemove {
		if m.Item("移除队列").Chosen() {
			a.Player().RemoveQueueEntry(order[position])
		}
	} else if available {
		if m.Item("加入队列").Disabled(scheduled).Chosen() {
			a.Player().Enqueue(track, false)
		}
	}
	return available || canRemove
}
