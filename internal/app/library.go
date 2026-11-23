package app

import (
	"netease-music-mygo/internal/library"
	"netease-music-mygo/internal/netease"
)

func (a *app) editable(id int64) *netease.Playlist           { return a.library.Editable(id) }
func (a *app) isSaved(pl *netease.Playlist) bool             { return a.library.IsSaved(pl) }
func (a *app) isLiked(tr *netease.Track) bool                { return a.library.IsLiked(tr) }
func (a *app) albumSaved(id int64) bool                      { return a.library.AlbumSaved(id) }
func (a *app) likedPlaylist() int64                          { return a.library.LikedPlaylist() }
func copyPlaylist(pl netease.Playlist) netease.Playlist      { return library.CopyPlaylist(pl) }
func copyPlaylists(pl []netease.Playlist) []netease.Playlist { return library.CopyPlaylists(pl) }
