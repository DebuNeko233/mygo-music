package netease

import (
	"fmt"
	"time"
)

// What the listener played lately, on any device signed in as them: NetEase
// keeps their last 300 songs, with the playlists, albums, podcasts and
// episodes they played along with them.

// Play is a song the listener played, on one of their devices.
type Play struct {
	Track Track
	At    time.Time
	// Device is the system NetEase names the device by: iphone, android,
	// osx, pc, web…
	Device string
}

// Played is a playlist, an album, a podcast or an episode the listener
// played, and where and when, as Play is for songs.
type Played[T any] struct {
	Item   T
	At     time.Time
	Device string
}

// Voice is an episode the listener played, with the podcast it is on.
type Voice struct {
	Program Program
	Podcast Podcast
}

// RecentPlays returns the songs the listener played last, on any device
// signed in as them, the latest first.
func RecentPlays(limit int) ([]Play, error) {
	played, err := playedItems[Track]("/api/play-record/song/list", limit)
	if err != nil {
		return nil, err
	}
	songs := make([]Track, len(played))
	for i := range played {
		songs[i] = played[i].Item
	}
	// They come without privileges, as the daily songs do.
	detailPrivileges(songs)
	plays := make([]Play, len(played))
	for i := range played {
		plays[i] = Play{Track: songs[i], At: played[i].At, Device: played[i].Device}
	}
	return plays, nil
}

// RecentPlaylists returns the playlists the listener played last, the
// latest first.
func RecentPlaylists(limit int) ([]Played[Playlist], error) {
	return playedItems[Playlist]("/api/play-record/playlist/list", limit)
}

// RecentAlbums returns the albums the listener played last, the latest
// first.
func RecentAlbums(limit int) ([]Played[AlbumProfile], error) {
	return playedItems[AlbumProfile]("/api/play-record/album/list", limit)
}

// RecentPodcasts returns the podcasts the listener played last, the latest
// first.
func RecentPodcasts(limit int) ([]Played[Podcast], error) {
	return playedItems[Podcast]("/api/play-record/djradio/list", limit)
}

// RecentVoices returns the episodes the listener played last, the latest
// first: the songs' length of audio NetEase files as voices, under the
// podcasts they are on.
func RecentVoices(limit int) ([]Played[Voice], error) {
	played, err := playedItems[voiceRecord]("/api/play-record/voice/list", limit)
	if err != nil {
		return nil, err
	}
	voices := make([]Played[Voice], len(played))
	for i, p := range played {
		voices[i] = Played[Voice]{
			Item:   Voice{Program: p.Item.Program.Program, Podcast: p.Item.Program.Radio},
			At:     p.At,
			Device: p.Device,
		}
	}
	return voices, nil
}

// voiceRecord is how NetEase files an episode under a voice: the episode,
// with the podcast it is on among its fields.
type voiceRecord struct {
	Program voiceProgram `json:"pubDJProgramData"`
}

type voiceProgram struct {
	Program
	Radio Podcast `json:"radio"`
}

// playedItems asks NetEase of the play records path, limit of them at most,
// the latest first.
func playedItems[T any](path string, limit int) ([]Played[T], error) {
	var res struct {
		Code int `json:"code"`
		Data struct {
			List []struct {
				Data     T      `json:"data"`
				PlayTime int64  `json:"playTime"` // milliseconds
				OS       string `json:"os"`
				Terminal struct {
					OS string `json:"os"`
				} `json:"multiTerminalInfo"`
			} `json:"list"`
		} `json:"data"`
	}
	if err := eapiPost(path, map[string]any{"limit": limit}, &res); err != nil {
		return nil, err
	}
	if res.Code != 200 {
		return nil, fmt.Errorf("netease: %s: code %d", path, res.Code)
	}
	played := make([]Played[T], len(res.Data.List))
	for i, l := range res.Data.List {
		device := l.OS
		if device == "" {
			device = l.Terminal.OS
		}
		played[i] = Played[T]{Item: l.Data, At: time.UnixMilli(l.PlayTime), Device: device}
	}
	return played, nil
}
