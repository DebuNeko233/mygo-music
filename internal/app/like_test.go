/*
验证歌曲与节目喜欢状态的界面联动和快照持久化，并提供相关视觉截图。
*/

package app

import (
	"errors"
	"fmt"
	"image"
	"image/color"
	"image/draw"
	"image/png"
	"os"
	"path/filepath"
	"slices"
	"testing"
	"time"

	"github.com/egoist/mygo/ui"

	artcache "netease-music-mygo/internal/desktop/artwork"
	playbackview "netease-music-mygo/internal/desktop/view/playback"
	"netease-music-mygo/internal/diskcache"
	"netease-music-mygo/internal/library"
	"netease-music-mygo/internal/lyrics"
	"netease-music-mygo/internal/netease"
	"netease-music-mygo/internal/player"
)

// Clicking the player heart immediately changes the like and sends it to NetEase.
func TestLikeFromPlayerBar(t *testing.T) {
	calls := make(chan bool, 4)
	likeSong = func(id int64, like bool) error { calls <- like; return nil }
	defer func() { likeSong = netease.Like }()

	a := newTestApp()
	posts := testPosts(a)
	a.settings = defaultSettings()
	a.account.Profile = &netease.Profile{UserID: 1, Nickname: "小明"}
	tr := netease.Track{ID: 7, Name: "宝船", Artists: []netease.Artist{{Name: "DOUDOU"}}, Duration: 200000}
	player.Cue(a.pl, []netease.Track{tr})
	player.SetLyrics(a.pl, nil)
	tt := ui.NewTester(a.view, 1200, 800)
	tt.Advance(time.Second)
	tt.Frame()

	if err := tt.Click("喜欢 宝船"); err != nil {
		t.Fatal(err)
	}
	if !a.library.Liked[7] {
		t.Fatal("the heart did not fill at once")
	}
	if like := <-calls; !like {
		t.Error("asked NetEase to unlike")
	}
	refreshPost(t, posts)
	tt.Frame()
	if err := tt.Click("取消喜欢 宝船"); err != nil {
		t.Fatal(err)
	}
	if a.library.Liked[7] {
		t.Error("still liked after unliking")
	}
	if like := <-calls; like {
		t.Error("asked NetEase to like again")
	}
	refreshPost(t, posts)
}

// A confirmed unlike removes the row before revalidation, while a rejected
// unlike keeps the row and restores its heart.
func TestUnlikeInLikedPlaylistKeepsScrollAndQueue(t *testing.T) {
	for _, rejected := range []bool{false, true} {
		t.Run(fmt.Sprintf("rejected=%t", rejected), func(t *testing.T) {
			a, posts, _ := refreshFixture(t)
			a.account.Profile = &netease.Profile{UserID: 1}
			a.browseCache = diskcache.New(t.TempDir(), 64<<20)
			t.Cleanup(a.browseCache.Close)
			const pid, song = 5, 50
			pl := &netease.Playlist{ID: pid, Name: "我喜欢的音乐", SpecialType: 5, TrackCount: 100}
			for i := int64(1); i <= 100; i++ {
				pl.Tracks = append(pl.Tracks, netease.Track{ID: i, Name: fmt.Sprintf("歌曲 %03d", i), Duration: 200000})
				pl.TrackIDs = append(pl.TrackIDs, netease.TrackID{ID: i})
			}
			a.nav.Current = route{Page: pagePlaylist, ID: pid}
			a.library.Playlists[pid] = pl
			if !rejected {
				a.library.Mine = []netease.Playlist{{ID: pid, Name: pl.Name, SpecialType: 5}}
			}
			a.library.Liked = map[int64]bool{song: true}
			player.Cue(a.pl, pl.Tracks)
			player.Hold(a.pl, 43*time.Second)
			queueRevision := a.pl.QueueRevision()
			requests := make(chan int64, 1)
			reply := make(chan error, 1)
			oldLike := likeSong
			t.Cleanup(func() { likeSong = oldLike })
			likeSong = func(id int64, like bool) error {
				if like {
					return errors.New("expected unlike")
				}
				requests <- id
				return <-reply
			}
			defer close(reply)
			detailRequests := make(chan int64, 1)
			detailReply := make(chan *netease.Playlist, 1)
			defer close(detailReply)
			playlistDetail = func(pid int64) (*netease.Playlist, error) {
				detailRequests <- pid
				return <-detailReply, nil
			}

			tt := ui.NewTester(a.view, 1200, 800)
			tt.Frame()
			a.tracks.List.ScrollTo(song, ui.Center)
			tt.Frame()
			first, _ := a.tracks.List.Visible()
			y := a.shell.PageScroll.Y
			if err := tt.Click("取消喜欢 歌曲 050"); err != nil {
				t.Fatal(err)
			}
			if got := wait(t, requests, "the unlike"); got != song {
				t.Fatalf("unliked song %d, want %d", got, song)
			}
			if a.library.Liked[song] || a.library.Playlists[pid] != pl || a.library.Loading[pid] {
				t.Fatal("unlike should update the heart while leaving the list visible")
			}
			var err error
			if rejected {
				err = errors.New("unlike rejected")
			}
			reply <- err
			refreshPost(t, posts)
			tt.Frame()
			if top, _ := a.tracks.List.Visible(); top != first || a.shell.PageScroll.Y != y {
				t.Fatal("unlike moved the list")
			}
			if a.library.Playlists[pid] == nil || a.library.Loading[pid] {
				t.Fatal("unlike blanked the liked playlist")
			}
			if a.pl.QueueRevision() != queueRevision || a.pl.Queue()[song-1].ID != song ||
				!a.pl.Playing() || a.pl.Position() != 43*time.Second {
				t.Fatal("unlike changed the active playback queue")
			}
			if rejected {
				if !a.library.Liked[song] || a.library.Playlists[pid] != pl || a.playlistRequest(pid).Busy {
					t.Fatal("rejected unlike lost the original row or its heart")
				}
				return
			}
			if requested := wait(t, detailRequests, "the liked playlist refresh"); requested != pid {
				t.Fatalf("refreshed playlist %d, want %d", requested, pid)
			}
			next := a.library.Playlists[pid]
			if a.library.Liked[song] || next.TrackCount != 99 || len(next.Tracks) != 99 || len(next.TrackIDs) != 99 ||
				next.Tracks[song-1].ID != song+1 || next.TrackIDs[song-1].ID != song+1 {
				t.Fatal("the confirmed unlike did not remove the song from the cached list")
			}
			remote := library.CopyPlaylist(*next)
			remote.PlayCount = 1
			detailReply <- &remote
			refreshPost(t, posts)
			tt.Frame()
			if a.library.Playlists[pid].PlayCount != 1 || a.shell.PageScroll.Y != y {
				t.Fatal("the background refresh did not apply in place")
			}
			req := a.cacheRequest("playlist/5")
			a.waitForPlaylistSnapshot(req)
			var kept netease.Playlist
			if at := req.Read(&kept); at.IsZero() || len(kept.Tracks) != 99 || kept.TrackIDs[song-1].ID != song+1 {
				t.Fatal("the offline snapshot did not record the unlike")
			}
		})
	}
}

// A heart pressed outside the favorites page still refreshes that playlist
// once the sidebar has supplied its ID, even when a prior lookup was pending.
func TestLikeRefreshesOffscreenPlaylist(t *testing.T) {
	a, posts, _ := refreshFixture(t)
	a.account.Profile = &netease.Profile{UserID: 1}
	a.nav.Current = route{Page: pageHome}
	const pid, song = 5, 7
	tr := netease.Track{ID: song, Name: "宝船"}
	player.Cue(a.pl, []netease.Track{tr})
	mineReply := make(chan struct{}, 1)
	defer close(mineReply)
	userPlaylists = func(int64) ([]netease.Playlist, []netease.Playlist, error) {
		<-mineReply
		return []netease.Playlist{{ID: pid, SpecialType: 5}}, nil, nil
	}
	oldLike := likeSong
	t.Cleanup(func() { likeSong = oldLike })
	likeSong = func(int64, bool) error { return nil }
	requested := make(chan int64, 1)
	playlistDetail = func(id int64) (*netease.Playlist, error) {
		requested <- id
		return &netease.Playlist{ID: id, SpecialType: 5, Tracks: []netease.Track{tr}}, nil
	}
	a.loadMine(1)
	a.toggleLike(&tr)
	refreshPost(t, posts)
	mineReply <- struct{}{}
	refreshPost(t, posts)
	a.retryLibraryRefresh()
	mineReply <- struct{}{}
	refreshPost(t, posts)
	if id := wait(t, requested, "the offscreen favorites refresh"); id != pid {
		t.Fatalf("refreshed playlist %d, want %d", id, pid)
	}
	refreshPost(t, posts)
	if pl := a.library.Playlists[pid]; pl == nil || len(pl.Tracks) != 1 || pl.Tracks[0].ID != song || a.nav.Current.Page != pageHome {
		t.Fatal("offscreen favorites did not update in place")
	}
}

// The heart of an episode playing likes the episode, not its audio, once
// NetEase was asked, once, whether the listener likes it already.
func TestLikeEpisode(t *testing.T) {
	asked := make(chan []int64, 4)
	programsLiked = func(ids []int64) (map[int64]bool, error) { asked <- ids; return nil, nil }
	type call struct {
		id   int64
		like bool
	}
	calls := make(chan call, 4)
	likeProgram = func(id int64, like bool) error { calls <- call{id, like}; return nil }
	likeSong = func(int64, bool) error { t.Error("liked the episode's audio as a song"); return nil }
	defer func() {
		programsLiked, likeProgram, likeSong = netease.ProgramsLiked, netease.LikeProgram, netease.Like
	}()

	a := newTestApp()
	posts := testPosts(a)
	a.settings = defaultSettings()
	a.account.Profile = &netease.Profile{UserID: 1}
	player.Cue(a.pl, []netease.Track{{ID: 13, Name: "第一期", ProgramID: 3, PodcastID: 5, Duration: 600000}})
	tt := ui.NewTester(a.view, 1200, 800)
	tt.Advance(time.Second)
	tt.Frame()
	if ids := wait(t, asked, "whether the episode is liked"); !slices.Equal(ids, []int64{3}) {
		t.Fatalf("asked of %v", ids)
	}
	refreshPost(t, posts)
	if err := tt.Click("喜欢 第一期"); err != nil {
		t.Fatal(err)
	}
	if !a.library.LikedPrograms[3] {
		t.Fatal("the heart did not fill at once")
	}
	if c := wait(t, calls, "the like"); c != (call{3, true}) {
		t.Errorf("asked NetEase for %+v", c)
	}
	refreshPost(t, posts)
	tt.Frame()
	if err := tt.Click("取消喜欢 第一期"); err != nil {
		t.Fatal(err)
	}
	if c := wait(t, calls, "the unlike"); c != (call{3, false}) || a.library.LikedPrograms[3] {
		t.Errorf("asked NetEase for %+v, liked %v", c, a.library.LikedPrograms[3])
	}
	refreshPost(t, posts)
	if len(asked) > 0 {
		t.Error("asked again whether the episode is liked")
	}
}

// A heart clicked while NetEase still answers whether the listener likes
// the episode keeps what the click made it; an answer for a listener
// signed out since is dropped.
func TestEpisodeLikeClickBeatsAnswer(t *testing.T) {
	isolateUserData(t)
	fakeOfflineHome(t)
	asked := make(chan []int64, 2)
	answers := make(chan map[int64]bool, 1)
	programsLiked = func(ids []int64) (map[int64]bool, error) {
		asked <- ids
		return <-answers, nil
	}
	likeProgram = func(int64, bool) error { return nil }
	defer func() { programsLiked, likeProgram = netease.ProgramsLiked, netease.LikeProgram }()
	a := newTestApp()
	posts := testPosts(a)
	a.account.Profile = &netease.Profile{UserID: 1}
	a.askProgramLikes([]int64{3, 4})
	wait(t, asked, "the first answer request")
	a.toggleLike(&netease.Track{ID: 13, ProgramID: 3})
	refreshPost(t, posts)
	answers <- map[int64]bool{4: true}
	refreshPost(t, posts)
	if !a.library.LikedPrograms[3] || !a.library.LikedPrograms[4] {
		t.Fatalf("liked %v", a.library.LikedPrograms)
	}
	a.askProgramLikes([]int64{5})
	wait(t, asked, "the old account request")
	a.signedIn(nil)
	a.account.Profile = &netease.Profile{UserID: 2}
	answers <- map[int64]bool{5: true}
	// Two guest home requests and the old account's answer all finish here.
	for range 3 {
		refreshPost(t, posts)
	}
	if a.library.LikedPrograms[5] {
		t.Error("took the last listener's answer")
	}
}

// On a podcast's page, a liked episode's heart shows, and another's as the
// pointer is on it, which likes that one.
func TestLikeEpisodeRow(t *testing.T) {
	programsLiked = func([]int64) (map[int64]bool, error) { return nil, nil }
	liked := make(chan int64, 4)
	likeProgram = func(id int64, like bool) error { liked <- id; return nil }
	defer func() { programsLiked, likeProgram = netease.ProgramsLiked, netease.LikeProgram }()
	a := catalogApp()
	posts := testPosts(a)
	a.account.Profile = &netease.Profile{UserID: 1}
	a.library.SetProgramLiked(40, true)
	a.open(route{Page: pagePodcast, ID: 5})
	tt := ui.NewTester(a.view, 1200, 800)
	tt.Frame()
	episodes := a.podcasts.Pages.Get(5).Programs
	if _, ok := tt.Find("取消喜欢 " + episodes[0].Name); !ok {
		t.Fatal("the liked episode shows no heart")
	}
	for _, pr := range episodes {
		if _, ok := tt.Find("喜欢 " + pr.Name); ok {
			t.Fatalf("%s shows a heart with the pointer on no episode", pr.Name)
		}
	}
	r, _ := tt.Find(episodes[2].Name)
	tt.Move(r.X+r.W/2, r.Y+r.H/2)
	tt.Frame()
	refreshPost(t, posts)
	if err := tt.Click("喜欢 " + episodes[2].Name); err != nil {
		t.Fatal(err)
	}
	if id := wait(t, liked, "the like"); id != 42 || !a.library.LikedPrograms[42] {
		t.Errorf("liked %d", id)
	}
	refreshPost(t, posts)
	if a.pl.Current() != nil {
		t.Error("the heart played the episode")
	}
}

// With NM_FRAMES set to a directory, saves the like burst at moments after
// the click there, side by side, to look at.
func TestLikeBurstFrames(t *testing.T) {
	dir := os.Getenv("NM_FRAMES")
	if dir == "" {
		t.Skip()
	}
	likeSong = func(int64, bool) error { return nil }
	defer func() { likeSong = netease.Like }()
	a := newTestApp()
	posts := testPosts(a)
	a.settings = defaultSettings()
	a.account.Profile = &netease.Profile{UserID: 1}
	tr := netease.Track{ID: 7, Name: "宝船", Duration: 200000}
	player.Cue(a.pl, []netease.Track{tr})
	tt := ui.NewTester(a.view, 1200, 800)
	tt.SetDark(true)
	tt.Advance(time.Second)
	tt.Frame()
	r, ok := tt.Find("喜欢 宝船")
	if !ok {
		t.Fatal("no heart")
	}
	tt.Click("喜欢 宝船")
	refreshPost(t, posts)
	moments := []time.Duration{0, 60, 120, 200, 300, 420, 600}
	img := tt.Image()
	scale := float32(img.Bounds().Dx()) / 1200
	cell := int(90 * scale)
	strip := image.NewRGBA(image.Rect(0, 0, cell*len(moments), cell))
	at := time.Duration(0)
	for i, m := range moments {
		tt.Advance(m*time.Millisecond - at)
		at = m * time.Millisecond
		tt.Frame()
		img := tt.Image()
		cx, cy := int((r.X+r.W/2)*scale), int((r.Y+r.H/2)*scale)
		draw.Draw(strip, image.Rect(i*cell, 0, (i+1)*cell, cell), img, image.Pt(cx-cell/2, cy-cell/2), draw.Src)
	}
	f, _ := os.Create(filepath.Join(dir, "burst.png"))
	defer f.Close()
	png.Encode(f, strip)
}

// With NM_FRAMES set, saves the lyrics stage in each of its styles at
// moments through a line.
func TestStageFrames(t *testing.T) {
	dir := os.Getenv("NM_FRAMES")
	if dir == "" {
		t.Skip()
	}
	for _, s := range playbackview.StageStyles {
		name := "stage.png"
		if s.ID != "" {
			name = "stage-" + s.ID + ".png"
		}
		stageFrames(s.ID, filepath.Join(dir, name))
	}
}

func stageFrames(style, path string) {
	a := newTestApp()
	a.settings = defaultSettings()
	a.settings.StageStyle = style
	tr := netease.Track{ID: 7, Name: "海屿你", Artists: []netease.Artist{{Name: "马也_Crabbit"}}, Duration: 200000}
	player.Cue(a.pl, []netease.Track{tr})
	player.SetLyrics(a.pl, lyrics.Build("[00:13.00]我从不主动示弱\n[00:30.56]曾经的那些发生过的开心和难过",
		"[00:13.00]I never show weakness first\n[00:30.56]The joy and sorrow that once happened",
		`[13010,4780](13010,660,0)从(13670,670,0)不(14340,30,0) (14370,480,0)主(14850,610,0)动(15460,30,0) (15490,330,0)示(15820,1070,0)弱
[17790,1770](17790,170,0)我(17960,450,0)们(18410,290,0)的(18700,470,0)过(19170,390,0)去
[30560,6580](30560,220,0)曾(30780,430,0)经(31210,340,0)的(31550,350,0)那(31900,470,0)些(32370,400,0)发(32770,320,0)生(33090,470,0)过(33560,30,0) (33590,210,0)的(33800,550,0)开(34350,250,0)心(34600,360,0)和(34960,640,0)难(35600,1500,0)过`))
	a.playbackState.Open, a.playbackState.Pane = true, playbackview.PaneStage
	tt := ui.NewTester(a.view, 1200, 800)
	tt.SetDark(true)
	tt.Advance(time.Second)
	moments := []time.Duration{12900, 13300, 13800, 14600, 15900, 17300, 17900, 18500, 26000}
	img := tt.Image()
	cw, ch := img.Bounds().Dx()/3, img.Bounds().Dy()/3
	grid := image.NewRGBA(image.Rect(0, 0, cw*3, ch*3))
	for i, m := range moments {
		a.pl.Seek(m * time.Millisecond)
		tt.Advance(16 * time.Millisecond)
		tt.Frame()
		shot := tt.Image()
		small := image.NewRGBA(image.Rect(0, 0, cw, ch))
		for y := range ch {
			for x := range cw {
				small.Set(x, y, shot.At(x*3, y*3))
			}
		}
		draw.Draw(grid, image.Rect((i%3)*cw, (i/3)*ch, (i%3+1)*cw, (i/3+1)*ch), small, image.Point{}, draw.Src)
	}
	f, _ := os.Create(path)
	defer f.Close()
	png.Encode(f, grid)
}

// With NM_FRAMES set, saves Now Playing with a real cover, to look at its
// backdrop.
func TestBackdropFrame(t *testing.T) {
	dir := os.Getenv("NM_FRAMES")
	if dir == "" {
		t.Skip()
	}
	hits, err := netease.Search("春日影 MyGO", 1)
	if err != nil || len(hits) == 0 {
		t.Fatal(err)
	}
	tr := hits[0]
	a := newTestApp()
	a.settings = defaultSettings()
	for _, size := range []int{600, 120, 96} {
		img, err := fetchImage(netease.Sized(tr.Album.PicURL, size))
		if err != nil {
			t.Fatal(err)
		}
		art := &artcache.Image{Bitmap: ui.NewBitmap(img), Tint: artcache.DominantColor(img)}
		top, foot := playbackview.BackdropColors(art.Tint)
		t.Logf("tint %v, backdrop %v to %v", art.Tint, top, foot)
		a.art.Put(netease.Sized(tr.Album.PicURL, size), art)
		if size == 600 {
			bd := *art
			bd.Backdrop = ui.NewBitmap(artcache.Blur(img))
			a.art.Put(netease.Sized(tr.Album.PicURL, size)+"#backdrop", &bd)
		}
	}
	player.Cue(a.pl, []netease.Track{tr})
	player.SetLyrics(a.pl, lyrics.Build("[00:00.00]作词: 織田あすか\n[00:01.00]作曲: 藤田淳平(Elements Garden)\n[00:02.00]编曲: 藤田淳平(Elements Garden)\n[00:15.00]悴んだ心 ふるえる眼差し\n[00:20.00]世界で 僕は ひとりぼっちだった\n[00:25.00]散ることしか知らない春は\n[00:30.00]毎年 冷たくあしらう",
		"[00:15.00]内心满是憔悴 眼神颤抖不止\n[00:20.00]我在这世界孤独一人\n[00:25.00]在不断凋零的春季中\n[00:30.00]每年都只感受到冰冷", ""))
	a.pl.Seek(2500 * time.Millisecond)
	a.playbackState.Open = true
	tt := ui.NewTester(a.view, 1000, 680)
	tt.SetDark(true)
	tt.Advance(2 * time.Second)
	tt.Frame()
	f, _ := os.Create(filepath.Join(dir, "backdrop.png"))
	defer f.Close()
	png.Encode(f, tt.Image())
}

// With NM_FRAMES set, saves the progress bar's comet at moments, in Now
// Playing and in the player bar.
func TestSmokeFrames(t *testing.T) {
	dir := os.Getenv("NM_FRAMES")
	if dir == "" {
		t.Skip()
	}
	hits, err := netease.Search("春日影 MyGO", 1)
	if err != nil || len(hits) == 0 {
		t.Fatal(err)
	}
	tr := hits[0]
	a := newTestApp()
	a.settings = defaultSettings()
	for _, size := range []int{600, 120, 96} {
		img, err := fetchImage(netease.Sized(tr.Album.PicURL, size))
		if err != nil {
			t.Fatal(err)
		}
		art := &artcache.Image{Bitmap: ui.NewBitmap(img), Tint: artcache.DominantColor(img)}
		a.art.Put(netease.Sized(tr.Album.PicURL, size), art)
		if size == 600 {
			bd := *art
			bd.Backdrop = ui.NewBitmap(artcache.Blur(img))
			a.art.Put(netease.Sized(tr.Album.PicURL, size)+"#backdrop", &bd)
		}
	}
	player.Cue(a.pl, []netease.Track{tr})
	player.SetLyrics(a.pl, nil)
	player.Hold(a.pl, 75*time.Second)
	a.settings.Smoke = true
	shots := func(name string, dark bool) {
		tt := ui.NewTester(a.view, 1000, 680)
		tt.SetDark(dark)
		tt.Advance(2 * time.Second)
		tt.Frame()
		r, ok := tt.Find("播放进度")
		if !ok {
			t.Fatal("no progress bar")
		}
		img := tt.Image()
		scale := float32(img.Bounds().Dx()) / 1000
		frac := float32(75.0 / tr.Length().Seconds())
		cx, cy := int((r.X+r.W*frac)*scale), int((r.Y+r.H/2)*scale)
		w, h := int(200*scale), int(70*scale)
		moments := 4
		strip := image.NewRGBA(image.Rect(0, 0, w, h*moments))
		for i := range moments {
			tt.Advance(170 * time.Millisecond)
			tt.Frame()
			draw.Draw(strip, image.Rect(0, i*h, w, (i+1)*h), tt.Image(), image.Pt(cx-w*3/4, cy-h*2/3), draw.Src)
		}
		f, _ := os.Create(filepath.Join(dir, name+".png"))
		png.Encode(f, strip)
		f.Close()
	}
	a.playbackState.Open = true
	shots("smoke-np", true)
	a.playbackState.Open = false
	shots("smoke-bar", false)
}

// With NM_FRAMES set, saves the player bar at window widths from the
// narrowest up.
func TestPlayerBarWidths(t *testing.T) {
	dir := os.Getenv("NM_FRAMES")
	if dir == "" {
		t.Skip()
	}
	a := newTestApp()
	a.settings = defaultSettings()
	tr := netease.Track{ID: 7, Name: "那些你很冒险的梦 (Live)", Artists: []netease.Artist{{Name: "林俊杰"}, {Name: "JJ Lin"}}, Duration: 199000}
	player.Cue(a.pl, []netease.Track{tr})
	a.pl.Seek(25 * time.Second)
	widths := []int{880, 1000, 1180}
	var strip *image.RGBA
	y := 0
	for i, w := range widths {
		tt := ui.NewTester(a.view, w, 600)
		tt.Frame()
		img := tt.Image()
		s := float32(img.Bounds().Dx()) / float32(w)
		bar := image.Rect(int(float32(sidebarWidth)*s), int(float32(600-playerBarHeight)*s), img.Bounds().Dx(), img.Bounds().Dy())
		if i == 0 {
			strip = image.NewRGBA(image.Rect(0, 0, int(float32(1180-sidebarWidth)*s), bar.Dy()*len(widths)))
			draw.Draw(strip, strip.Bounds(), image.NewUniform(color.RGBA{60, 60, 60, 255}), image.Point{}, draw.Src)
		}
		draw.Draw(strip, image.Rect(0, y, bar.Dx(), y+bar.Dy()), img, bar.Min, draw.Src)
		y += bar.Dy()
	}
	f, _ := os.Create(filepath.Join(dir, "bar-widths.png"))
	defer f.Close()
	png.Encode(f, strip)
}

// With NM_FRAMES set, saves the volume popover open, light and dark.
func TestVolumePopoverFrames(t *testing.T) {
	dir := os.Getenv("NM_FRAMES")
	if dir == "" {
		t.Skip()
	}
	for _, dark := range []bool{false, true} {
		a := newTestApp()
		a.settings = defaultSettings()
		tr := netease.Track{ID: 7, Name: "春日影", Artists: []netease.Artist{{Name: "MyGO!!!!!"}}, Duration: 199000}
		player.Cue(a.pl, []netease.Track{tr})
		a.playbackState.Volume = 0.63
		tt := ui.NewTester(a.view, 1000, 600)
		tt.SetDark(dark)
		tt.Frame()
		r, ok := tt.Find("音量")
		if !ok {
			t.Fatal("no volume button")
		}
		tt.Click("音量")
		tt.Advance(time.Second)
		tt.Frame()
		if !a.VolumeOpen || !tt.HasText("63%") {
			t.Fatalf("popover not open: %v %q", a.VolumeOpen, tt.Texts())
		}
		img := tt.Image()
		s := float32(img.Bounds().Dx()) / 1000
		cx := int((r.X + r.W/2) * s)
		crop := image.Rect(cx-int(70*s), int((r.Y-240)*s), cx+int(70*s), int((r.Y+r.H+6)*s))
		out := image.NewRGBA(image.Rect(0, 0, crop.Dx(), crop.Dy()))
		draw.Draw(out, out.Bounds(), img, crop.Min, draw.Src)
		name := "volume-light.png"
		if dark {
			name = "volume-dark.png"
		}
		f, _ := os.Create(filepath.Join(dir, name))
		png.Encode(f, out)
		f.Close()
	}
}

// With NM_FRAMES set, saves the segmented control of the settings, light
// and dark, at twice the scale.
func TestSegmentedFrames(t *testing.T) {
	dir := os.Getenv("NM_FRAMES")
	if dir == "" {
		t.Skip()
	}
	for _, dark := range []bool{false, true} {
		a := newTestApp()
		a.settings = defaultSettings()
		a.settings.Appearance = "light"
		tt := ui.NewTester(a.views.settings.Build, 780, 640)
		tt.SetScale(2)
		tt.SetDark(dark)
		tt.Frame()
		r, ok := tt.Find("跟随系统")
		if !ok {
			t.Fatal("no segmented control")
		}
		img := tt.Image()
		crop := image.Rect(int((r.X-30)*2), int((r.Y-14)*2), int((r.X+250)*2), int((r.Y+r.H+14)*2))
		out := image.NewRGBA(image.Rect(0, 0, crop.Dx(), crop.Dy()))
		draw.Draw(out, out.Bounds(), img, crop.Min, draw.Src)
		name := "segmented-light.png"
		if dark {
			name = "segmented-dark.png"
		}
		f, _ := os.Create(filepath.Join(dir, name))
		png.Encode(f, out)
		f.Close()
	}
}

// ⌘D likes the song playing, and again unlikes it, saying so in a toast.
func TestLikeCurrent(t *testing.T) {
	likeSong = func(int64, bool) error { return nil }
	defer func() { likeSong = netease.Like }()
	a := newTestApp()
	posts := testPosts(a)
	player.Cue(a.pl, []netease.Track{{ID: 7, Name: "宝船"}})
	a.account.Profile = &netease.Profile{UserID: 1}
	a.likeCurrent()
	refreshPost(t, posts)
	if !a.library.Liked[7] || a.toast == "" {
		t.Fatalf("liked %v, toast %q", a.library.Liked[7], a.toast)
	}
	a.likeCurrent()
	refreshPost(t, posts)
	if a.library.Liked[7] || a.toast == "" {
		t.Fatalf("unliked: liked %v, toast %q", a.library.Liked[7], a.toast)
	}
	// An episode playing: the episode, not its audio.
	likeProgram = func(int64, bool) error { return nil }
	defer func() { likeProgram = netease.LikeProgram }()
	player.Cue(a.pl, []netease.Track{{ID: 13, Name: "第一期", ProgramID: 3}})
	a.likeCurrent()
	refreshPost(t, posts)
	if !a.library.LikedPrograms[3] || a.library.Liked[13] {
		t.Fatalf("episode liked %v, its audio %v", a.library.LikedPrograms[3], a.library.Liked[13])
	}
}

// Liking a song saves the likes snapshot without replacing unchanged list or album snapshots.
func TestSongLikeOnlyPersistsChangedSnapshots(t *testing.T) {
	old := likeSong
	defer func() { likeSong = old }()
	likeSong = func(int64, bool) error { return nil }
	a := newTestApp()
	a.account.Profile = &netease.Profile{UserID: 7}
	a.library.Mine = []netease.Playlist{{ID: 12, Name: "existing"}}
	a.library.SubsShown = true
	a.library.SubAlbums = []netease.AlbumProfile{{Album: netease.Album{ID: 13, Name: "existing"}}}
	dir := t.TempDir()
	store := diskcache.New(dir, 1<<20)
	defer store.Close()
	a.browseCache = store
	store.Put("7", "mine", diskcache.Encode(library.Lists{Mine: a.library.Mine}), store.Epoch())
	store.Put("7", "albums", diskcache.Encode(a.library.SubAlbums), store.Epoch())
	_, mineAt := store.Read("7", "mine")
	_, albumsAt := store.Read("7", "albums")
	if mineAt.IsZero() || albumsAt.IsZero() {
		t.Fatal("could not prepare cached library")
	}
	posts := testPosts(a)
	a.toggleLike(&netease.Track{ID: 42})
	refreshPost(t, posts)
	store.Close() // Drain the writes before reading their persisted timestamps.
	reopened := diskcache.New(dir, 1<<20)
	defer reopened.Close()
	if value, _ := reopened.Read("7", "likes"); string(value) != "[42]" {
		t.Fatalf("confirmed likes snapshot was not saved: %s", value)
	}
	for key, want := range map[string]time.Time{"mine": mineAt, "albums": albumsAt} {
		if _, at := reopened.Read("7", key); !at.Equal(want) {
			t.Errorf("%s was rewritten by a like", key)
		}
	}
}
