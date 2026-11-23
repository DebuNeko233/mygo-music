package navigation

import "netease-music-mygo/internal/netease"

type Page int

const (
	Home Page = iota
	Daily
	Recent
	Charts
	Playlist
	Search
	FM
	Artist
	Album
	User
	Podcast
	Podcasts
	Local
	Cloud
)

type Route struct {
	Page  Page
	ID    int64
	Query string
	Kind  netease.SearchType
}
