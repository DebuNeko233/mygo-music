package app

import (
	"path/filepath"

	"github.com/egoist/mygo"

	"netease-music-mygo/internal/audioformat"
	"netease-music-mygo/internal/localcatalog"
	"netease-music-mygo/internal/netease"
	"netease-music-mygo/internal/userdata"
	"netease-music-mygo/internal/viewmodel"
)

var localExtensions = audioformat.Extensions()

func (a *app) initLocalMusic() {
	path := ""
	if dir, err := mygo.App.Path(mygo.PathUserData); err == nil {
		path = filepath.Join(dir, "local-music.json")
	}
	a.local.Load(path)
	a.pl.LocalPath = func(id int64) string {
		if t := a.pl.Current(); t != nil && t.Temporary && t.LocalID == id {
			return a.local.Temporary.Path
		}
		return a.local.Song(id).Path
	}
	a.pl.LocalMissing = a.local.MarkMissing
	a.pl.LocalLyrics = a.local.LyricsLoader
}

func (a *app) localServices() viewmodel.LocalServices {
	return viewmodel.LocalServices{
		Post: a.post, Check: localcatalog.MissingFiles, UpdateTracks: a.pl.UpdateLocalTracks,
		Save: func(path string, catalog localcatalog.Catalog) bool { return userdata.WriteJSON(path, catalog) },
		Changed: func() {
			a.redrawLocalFolders()
			if a.win != nil {
				a.win.Invalidate()
			}
		},
	}
}

func (a *app) playbackLocalSong(track *netease.Track) localcatalog.Song {
	if track.Temporary {
		return a.local.Temporary
	}
	return a.local.Song(track.LocalID)
}
func (a *app) checkLocalFiles()               { a.local.CheckFiles(a.localServices()) }
func (a *app) addLocalFolders(paths []string) { a.local.AddFolders(paths, a.localServices()) }
func (a *app) refreshLocalSongs() {
	if !a.localView.picking {
		a.local.Refresh(a.localServices())
	}
}
func (a *app) removeLocalFolder(path string)         { a.local.RemoveFolder(path, a.localServices()) }
func (a *app) relinkLocalSong(id int64, path string) { a.local.Relink(id, path, a.localServices()) }
func (a *app) removeLocalSong(id int64)              { a.local.RemoveSong(id, a.localServices()) }
