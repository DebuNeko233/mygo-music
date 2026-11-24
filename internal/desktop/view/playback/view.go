package playback

import (
	"time"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"

	"netease-music-mygo/internal/desktop/view/components"
	"netease-music-mygo/internal/desktop/view/page"
	"netease-music-mygo/internal/netease"
	"netease-music-mygo/internal/player"
	"netease-music-mygo/internal/preferences"
	captionsvm "netease-music-mygo/internal/viewmodel/captions"
	commentsvm "netease-music-mygo/internal/viewmodel/comments"
	playbackvm "netease-music-mygo/internal/viewmodel/playback"
)

// View borrows playback data and app commands; State owns only its drawing and input state.
type View struct {
	*page.Page
	*State
	Playback *playbackvm.Playback
	Reading  *playbackvm.LyricReading
	Captions *captionsvm.Captions
	Comments *commentsvm.Comments
	Config   *preferences.Settings
	Picked   *PickedUp

	Player        func() *player.Player
	Window        func() *mygo.Window
	Quitting      func() bool
	Notch         func() *Notch
	DesktopLyrics func() *DeskLyrics
	CursorPoint   func() mygo.Point
	Theme         func(*ui.Theme) ui.Theme
	SaveSettings  func()

	ProgramPane           func(*ui.Context, *netease.Track, float32, float32)
	CommentsPane          func(*ui.Context, *netease.Track)
	ArtistLinks           func(*ui.Context, *netease.Track, ui.Color, ui.Color, bool) ui.Element
	TrackCover            func(*ui.Context, *netease.Track, int, float32, float32) ui.Element
	LikeButton            func(*ui.Context, *netease.Track, float32, ui.Color) ui.Element
	SongMenu              func(*ui.Context, *netease.Track, float32, ui.Color) ui.Element
	SongItems             func(*netease.Track, int64) func(*ui.Menu)
	ModelCard             func(*ui.Context)
	LocalLyricMatchButton func(*ui.Context)
	Avatar                func(*ui.Context, string, float32, ui.Color)

	CommentsFor     func(*netease.Track) *commentsvm.Comments
	CommentsTab     func() string
	CommentCount    func() int
	CaptionsOn      func() bool
	CaptionsShown   func() bool
	CaptionsWritten func() bool
	ModelReady      func() bool
	CaptionsWriting func(time.Duration) bool

	SyncCaptions    func()
	SyncVolume      func()
	SettingsChanged func()
	Redraw          func()
	OpenSettings    func()
	ShowMain        func()
	ExitRadio       func()
	SyncWindows     func()
	Dispatch        func(func(), bool)

	VolumeServices func() playbackvm.VolumeServices

	Pictures      func([]string) []*BackdropPicture
	TrackArtwork  func(*netease.Track, int, bool) components.Image
	HasTrackCover func(*netease.Track) bool
	BarArtwork    func(*netease.Track) components.Image
	StageArtwork  func(*netease.Track) components.Image
	ListenNative  func(func())
}

func (a *View) palette() components.Palette { return *a.Palette }

type settings = preferences.Settings
type rollState = components.RollState
type popover = components.Popover
