/*
管理在线搜索联想的加载与关闭，并整理供界面显示的建议列表。
*/

package search

import (
	"fmt"
	"time"

	"netease-music-mygo/internal/features/navigation"
	"netease-music-mygo/internal/features/netease"
)

const suggestDelay = 220 * time.Millisecond

type Suggestion struct {
	Key, Name, Detail, Kind string
	Route                   navigation.Route
}
type Suggestions struct {
	Query  string
	Items  []Suggestion
	tracks trackSuggestState
	timer  *time.Timer
	gen    uint64
}

func (m *Search) NeedsSuggestions(q string) bool { return q != m.Suggestions.Query }
func (m *Search) Suggest(q string, s SearchServices) {
	st := &m.Suggestions
	if q == st.Query {
		return
	}
	m.DismissSuggestions()
	st.Query = q
	generation, fetch := st.gen, s.Suggest
	st.timer = time.AfterFunc(suggestDelay, func() {
		res, _ := fetch(q)
		s.Post(func() {
			if q != st.Query || generation != st.gen {
				return
			}
			st.timer = nil
			st.Items = SuggestionList(res)
		})
	})
}
func (m *Search) DismissSuggestions() {
	st := &m.Suggestions
	if st.timer != nil {
		st.timer.Stop()
	}
	*st = Suggestions{gen: st.gen + 1}
}

// suggestionList is the first ten suggestions, the kinds in the order
// NetEase ranks them.
func SuggestionList(res netease.Suggestions) []Suggestion {
	byKind := map[string][]Suggestion{}
	for _, t := range res.Songs {
		byKind["songs"] = append(byKind["songs"], Suggestion{fmt.Sprint("song:", t.ID), t.Name, t.ArtistNames(), "单曲",
			navigation.Route{Page: navigation.Search, Query: t.Name, Kind: netease.SearchSongs}})
	}
	for _, ar := range res.Artists {
		byKind["artists"] = append(byKind["artists"], Suggestion{fmt.Sprint("artist:", ar.ID), ar.Name, "", "歌手",
			navigation.Route{Page: navigation.Artist, ID: ar.ID}})
	}
	for _, al := range res.Albums {
		byKind["albums"] = append(byKind["albums"], Suggestion{fmt.Sprint("album:", al.ID), al.Name, al.ArtistNames(), "专辑",
			navigation.Route{Page: navigation.Album, ID: al.ID}})
	}
	for _, pl := range res.Playlists {
		byKind["playlists"] = append(byKind["playlists"], Suggestion{fmt.Sprint("playlist:", pl.ID), pl.Name, pl.Creator.Nickname, "歌单",
			navigation.Route{Page: navigation.Playlist, ID: pl.ID}})
	}
	var out []Suggestion
	for _, kind := range append(res.Order, "songs", "artists", "albums", "playlists") {
		out = append(out, byKind[kind]...)
		delete(byKind, kind) // listed once, if also in the order
	}
	return out[:min(len(out), 10)]
}
