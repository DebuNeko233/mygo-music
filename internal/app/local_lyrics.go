package app

import (
	"cmp"
	"encoding/json"
	"errors"
	"math"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"sync"

	"github.com/egoist/mygo/ui"

	"netease-music-mygo/internal/lyrics"
	"netease-music-mygo/internal/netease"
)

var searchLocalLyrics = netease.Search

type localLyricMatch struct {
	SongID int64      `json:"songId"`
	Lyrics keptLyrics `json:"lyrics"`
}

type localLyricDialog struct {
	open, searching, loading bool
	track                    *netease.Track
	query, status            string
	results                  []netease.Track
	selected                 int64
	request                  uint64
	scroll                   ui.ScrollState
}

func (a *app) localLyricPath(id int64) string {
	if a.local.path == "" {
		return ""
	}
	return localLyricFile(a.local.path, id)
}

func localLyricDir(catalogPath string) string {
	return filepath.Join(filepath.Dir(catalogPath), "local-lyrics")
}

func localLyricFile(catalogPath string, id int64) string {
	return filepath.Join(localLyricDir(catalogPath), strconv.FormatInt(id, 10)+".json")
}

type localLyricCache struct {
	mu      sync.Mutex
	deleted map[int64]struct{}
}

func (c *localLyricCache) save(path string, localID int64, match localLyricMatch) error {
	if path == "" {
		return os.ErrInvalid
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if _, removed := c.deleted[localID]; removed {
		return os.ErrNotExist
	}
	return writeUserData(path, cacheBytes(match))
}

// Only a committed catalog edit may remove the matched lyrics. A late
// network result cannot recreate their file after the track is removed.
func (c *localLyricCache) removeDeleted(path string, before, after []localSong) {
	if len(after) >= len(before) {
		return
	}
	kept := make(map[int64]bool, len(after))
	for _, song := range after {
		kept[song.Track.LocalID] = true
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.deleted == nil {
		c.deleted = make(map[int64]struct{})
	}
	for _, song := range before {
		if id := song.Track.LocalID; !kept[id] {
			c.deleted[id] = struct{}{}
			_ = os.Remove(localLyricFile(path, id))
		}
	}
}

// Clean files left behind by older versions. IDs allocated after this
// startup snapshot are above lastID and still belong to the live catalog.
func pruneLocalLyricCache(path string, lastID int64, byID map[int64]int) {
	dir := localLyricDir(path)
	entries, err := os.ReadDir(dir)
	if err != nil {
		return
	}
	for _, entry := range entries {
		stem, ok := strings.CutSuffix(entry.Name(), ".json")
		if !ok || !entry.Type().IsRegular() {
			continue
		}
		id, err := strconv.ParseInt(stem, 10, 64)
		if err != nil || id <= 0 || id > lastID || stem != strconv.FormatInt(id, 10) {
			continue
		}
		if _, kept := byID[id]; !kept {
			_ = os.Remove(filepath.Join(dir, entry.Name()))
		}
	}
}

// The player captures this loader before doing any file I/O on its worker.
func (a *app) localLyricsLoader(track netease.Track) func() (string, string, string) {
	if track.Temporary {
		kept := a.local.temporaryLyrics
		return func() (string, string, string) { return kept.Original, kept.Translation, kept.Words }
	}
	path := a.localLyricPath(track.LocalID)
	if path == "" {
		return nil
	}
	return func() (string, string, string) {
		var match localLyricMatch
		if b, err := os.ReadFile(path); err == nil && json.Unmarshal(b, &match) == nil && match.SongID > 0 {
			return match.Lyrics.Original, match.Lyrics.Translation, match.Lyrics.Words
		}
		return "", "", ""
	}
}

func (a *app) openLocalLyricMatch() {
	track := a.pl.Current()
	display, loaded := a.pl.LyricsDisplay()
	if track == nil || !track.IsLocal() || !loaded || display.HasSinging() || a.pl.MetadataLoading() {
		return
	}
	d := &localLyricDialog{open: true, track: track, query: localTrackQuery(track)}
	a.local.lyricDialog = d
	a.findLocalLyricMatches(d)
}

func (a *app) matchingLocalLyrics(d *localLyricDialog) bool {
	return a.local.lyricDialog == d && d.open && a.pl.Current() == d.track
}

func (a *app) findLocalLyricMatches(d *localLyricDialog) {
	query := strings.TrimSpace(d.query)
	if !a.matchingLocalLyrics(d) || query == "" || d.loading {
		return
	}
	d.request++
	d.results, d.selected, d.status, d.scroll = nil, 0, "", ui.ScrollState{}
	d.searching = true
	request, duration := d.request, d.track.Duration
	search := searchLocalLyrics
	go func() {
		results, err := search(query, 20)
		results = lyricCandidates(results, duration)
		a.post(func() {
			if !a.matchingLocalLyrics(d) || d.request != request {
				return
			}
			d.searching, d.results = false, results
			switch {
			case errors.Is(err, netease.ErrOffline):
				d.status = "联网后可搜索歌词"
			case err != nil:
				d.status = "搜索失败，请重试"
			case len(results) == 0:
				d.status = "没有找到匹配的歌曲，可以换个关键词"
			}
		})
	}()
}

func lyricCandidates(results []netease.Track, duration int64) []netease.Track {
	seen := make(map[int64]bool, len(results))
	unique := make([]netease.Track, 0, len(results))
	for _, track := range results {
		if track.ID > 0 && !seen[track.ID] {
			seen[track.ID] = true
			unique = append(unique, track)
		}
	}
	distance := func(t netease.Track) int64 {
		if t.Duration <= 0 || duration <= 0 {
			return math.MaxInt64
		}
		return max(t.Duration-duration, duration-t.Duration)
	}
	slices.SortStableFunc(unique, func(x, y netease.Track) int { return cmp.Compare(distance(x), distance(y)) })
	return unique
}

func (a *app) confirmLocalLyricMatch(d *localLyricDialog) {
	if !a.matchingLocalLyrics(d) || d.selected <= 0 || d.searching || d.loading {
		return
	}
	if !d.track.Temporary && a.local.song(d.track.LocalID).Path == "" {
		d.status = "该歌曲已从曲库移除"
		return
	}
	d.loading, d.status = true, ""
	id, localID, temporary := d.selected, d.track.LocalID, d.track.Temporary
	path, fetch := a.localLyricPath(localID), lyricsRequest
	go func() {
		original, translation, words, err := fetch(id)
		kept := keptLyrics{original, translation, words}
		_, display := lyrics.BuildDisplay(original, translation, words)
		status := ""
		switch {
		case errors.Is(err, netease.ErrOffline):
			status = "联网后可加载歌词"
		case err != nil:
			status = "歌词加载失败，请重试"
		case !display.HasSinging():
			status = "这个版本没有可用的歌词，请选择其他版本"
		case !temporary:
			err := a.local.lyricCache.save(path, localID, localLyricMatch{SongID: id, Lyrics: kept})
			if errors.Is(err, os.ErrNotExist) {
				status = "该歌曲已从曲库移除"
			} else if err != nil {
				status = "无法保存歌词，请重试"
			}
		}
		a.post(func() {
			if !a.matchingLocalLyrics(d) {
				return
			}
			d.loading, d.status = false, status
			if status != "" {
				return
			}
			if temporary {
				a.local.temporaryLyrics = kept
			}
			d.open = false
			a.pl.ReloadLocalLyrics()
		})
	}()
}

func (a *app) localLyricMatchButton(c *ui.Context) {
	if a.network.offline {
		ui.Text(c, "联网后可匹配歌词").FontSize(12).TextColor(white.Alpha(0.5))
		return
	}
	pillButton(c.Key("match-local-lyrics"), nil, "从网易云匹配歌词", white.Alpha(.16), white).
		Disabled(a.pl.MetadataLoading()).OnClick(a.openLocalLyricMatch)
}

func (a *app) localLyricMatchDialog(c *ui.Context) {
	d := a.local.lyricDialog
	if d == nil {
		return
	}
	if a.pl.Current() != d.track {
		d.open = false
	}
	p := a.pal
	ui.DialogBase(c.Key("local-lyric-match"), &d.open, func(backdrop, panel ui.Element) {
		w, h := c.Size()
		backdrop.Background(ui.RGBA(0, 0, 0, .35))
		panel.Width(min(560, w-48)).Padding(22).Radius(18).Gap(12).
			Background(p.bg).Border(1, p.hairline).Shadow(0, 24, 60, -12, ui.RGBA(0, 0, 0, .4))
		ui.Row(c).AlignItems(ui.Center).Gap(12).Children(func() {
			ui.Text(c, "从网易云匹配歌词").Grow(1).FontSize(17).FontWeight(700)
			iconButton(c.Key("close"), iconX, 18, p.muted, "关闭").OnClick(func() { d.open = false })
		})
		ui.Text(c, strings.Join([]string{d.track.Name, clock(d.track.Length())}, " · ")).
			TextColor(p.muted).FontSize(13).SingleLine()
		ui.Row(c).Gap(8).Children(func() {
			input := ui.TextInput(c.Key("query"), &d.query).Grow(1).MinWidth(0).
				Label("歌名和歌手").Placeholder("歌名和歌手").Disabled(d.loading).AutoFocus()
			if input.Submitted() {
				a.findLocalLyricMatches(d)
			}
			ui.Button(c.Key("search"), "搜索").Disabled(d.loading || strings.TrimSpace(d.query) == "").
				OnClick(func() { a.findLocalLyricMatches(d) })
		})
		if d.searching {
			ui.Text(c, "正在搜索…").TextColor(p.muted).FontSize(13)
		}
		if len(d.results) != 0 {
			ui.Scroll(c.Key("versions")).TrackScroll(&d.scroll).MaxHeight(max(100, min(322, h-245))).Gap(4).
				Children(func() {
					for _, track := range d.results {
						details := make([]string, 0, 3)
						if names := track.ArtistNames(); names != "" {
							details = append(details, names)
						}
						if track.Album.Name != "" {
							details = append(details, track.Album.Name)
						}
						if track.Duration > 0 {
							details = append(details, clock(track.Length()))
						}
						detail := strings.Join(details, " · ")
						row := ui.ButtonBase(c.Key(track.ID)).Label(strings.TrimSpace(track.Name+" "+detail)).
							Disabled(d.loading).Padding(8, 10).Radius(7)
						if d.selected == track.ID {
							row.Background(p.accent.Alpha(.16))
						} else if row.Hovered() {
							row.Background(p.hover)
						}
						row.Children(func() {
							ui.Column(c).Grow(1).MinWidth(0).Gap(3).Children(func() {
								ui.Text(c, track.Name).SingleLine().FontSize(14).FontWeight(600)
								if detail != "" {
									ui.Text(c, detail).SingleLine().FontSize(12).TextColor(p.muted)
								}
							})
						})
						row.OnClick(func() { d.selected = track.ID; d.status = "" })
					}
				})
		}
		if d.status != "" {
			ui.Text(c, d.status).TextColor(p.muted).FontSize(13)
		}
		ui.Row(c).Justify(ui.End).Children(func() {
			label := "使用此版本"
			if d.loading {
				label = "正在加载歌词…"
			}
			ui.PrimaryButton(c.Key("confirm"), label).Disabled(d.searching || d.loading || d.selected == 0).
				OnClick(func() { a.confirmLocalLyricMatch(d) })
		})
	})
}
