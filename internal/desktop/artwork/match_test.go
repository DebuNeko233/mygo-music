/*
验证封面匹配接受相近录音，并拒绝歌手不符或版本存在歧义的候选。
*/

package artwork

import (
	"testing"

	"netease-music-mygo/internal/netease"
)

// A close recording is eligible, while conflicting editions or different
// artists don't silently supply the cover.
func TestLocalArtworkMatchesRecording(t *testing.T) {
	song := netease.Track{Name: "Ｓｏｎｇ", Duration: 180000, Artists: []netease.Artist{{Name: "Band"}}}
	studio := netease.Track{Name: "Song", Duration: 180600, Artists: song.Artists, Album: netease.Album{Name: "Studio", PicURL: "studio"}}
	live := studio
	live.Duration, live.Album = 224000, netease.Album{Name: "Live", PicURL: "live"}
	cover := studio
	cover.Artists, cover.Album.PicURL = []netease.Artist{{Name: "Other"}}, "cover"
	if got := chooseTrack(&song, []netease.Track{live, cover, studio}); got != "studio" {
		t.Fatalf("matched %q, want the studio version", got)
	}
	live.Duration = 180100
	if got := chooseTrack(&song, []netease.Track{live, studio}); got != "" {
		t.Fatalf("picked %q for two versions with nearly identical times", got)
	}
	song.Album.Name = "Studio"
	if got := chooseTrack(&song, []netease.Track{live, studio}); got != "studio" {
		t.Fatalf("matched %q with an exact album tag", got)
	}
}
