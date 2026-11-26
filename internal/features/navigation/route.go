/*
定义应用页面种类和导航目标的数据结构。
*/

package navigation

import "netease-music-mygo/internal/features/netease"

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
