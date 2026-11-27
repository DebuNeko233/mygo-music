/*
组合正在播放页面、标签、队列和歌词面板，维护展开动画与歌词跟随布局。
*/

package playback

import (
	"strconv"
	"time"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"

	"netease-music-mygo/internal/features/lyrics"
	"netease-music-mygo/internal/features/netease"
	"netease-music-mygo/internal/ui/view/components"
	"netease-music-mygo/internal/ui/view/shell"
	playbackvm "netease-music-mygo/internal/ui/viewmodel/playback"
	radiovm "netease-music-mygo/internal/ui/viewmodel/radio"
)

var White = ui.RGB(255, 255, 255)

var npSpring = ui.Animation{Spring: ui.SpringOptions{Duration: 450 * time.Millisecond, Bounce: 0.08}}

// NPSettle is how long Now Playing takes to come or go, its spring at rest.
const NPSettle = 600 * time.Millisecond

// NPState tracks Now Playing opening and closing, and reports whether it is
// open and whether it has come to rest, open or closed. Call it once a
// build, before the window's content.
func (a *View) NPState(c *ui.Context) (open, settled bool) {
	open = a.Playback.Open && a.Player().Current() != nil
	now := c.Now()
	if open != a.NPOpen {
		a.NPOpen, a.NPChangedAt = open, now
		a.NPPointerUntil = time.Time{}
		if open && a.Playback.Pane == PaneLyrics {
			// The new scroll may clamp before it has measured its
			// contents. Start following before reading that offset.
			a.SeekLyricFollow()
		}
		// A build once it is at rest: to stop painting what it covers, or
		// itself once gone.
		c.After(NPSettle)
		// The window buttons light over its dark backdrop, and as the
		// theme over the pages (Windows).
		if a.Window() != nil {
			buttons := mygo.ThemeSystem
			if open {
				buttons = mygo.ThemeDark
			}
			a.Window().SetWindowButtonsTheme(buttons)
		}
	}
	return open, now.Sub(a.NPChangedAt) >= NPSettle
}

// NowPlayingLayer holds Now Playing over the whole window, rising into
// view as it opens and sinking away as it closes. The layer moves itself,
// staying on top all the way, rather than leaving as an exit transition
// would, under the rest of the window: that would show it going over the
// sidebar's material while the page above it hid it at once. Its pose
// moves it in frames that only paint, building nothing.
func (a *View) NowPlayingLayer(c *ui.Context, open, settled bool) {
	layer := ui.Box(c.Key("now-playing-layer")).Absolute().Top(0).Left(0).Right(0).Bottom(0)
	pose := ui.Pose{Y: 60, Fade: 1}
	if open {
		pose = ui.Pose{}
	}
	layer.AnimatePose(pose, npSpring)
	// Ask on every build: MyGo then rebuilds once when visibility changes.
	// Native hide callbacks alone cannot stop a later hidden build from
	// recreating the resources they just dropped.
	if c.WindowHidden() {
		layer.Invisible()
		a.releaseGraphics()
		return
	}
	if !open {
		a.NPSpectrum = LyricSpectrum{}
	}
	if !open && settled || a.Player().Current() == nil {
		// Closed: nothing to paint or click.
		layer.Invisible()
		a.QueueAt, a.WallState.track = -1, 0
		a.releaseGraphics()
		return
	}
	if !open {
		// On its way out, the window under it takes the clicks.
		layer.PassThrough()
	}
	layer.Children(func() { a.NowPlayingView(c) })
	if a.Playback.Pane != PaneLyrics || !a.Config.LyricEdgeFade {
		// The framebuffer is only reused while the effect is in view.
		a.LyricBlurPass = nil
	}
}

// releaseGraphics drops reconstructible layers after the closing transition,
// or when the owning window is hidden/minimized. Keep the current covers so
// restoring while a new cover loads does not expose a placeholder. The renderer
// owns GPU deletion and in-flight frames; views only release their references.
func (a *View) releaseGraphics() {
	a.BackdropCache = BackdropCache{}
	a.LyricBlurPass = nil
	a.NPSpectrum = LyricSpectrum{}
	a.NPCoverState.Previous = nil
	a.NPCoverState.Switching = false
	a.StageCover.Previous, a.WallCover.Previous = nil, nil
	a.StageCover.ChangedAt, a.WallCover.ChangedAt = time.Time{}, time.Time{}
}

// NowPlayingView fills the window with what plays: its cover over a slow
// drift of its own colors, the controls, and lyrics that follow the song.
func (a *View) NowPlayingView(c *ui.Context) {
	pl := a.Player()
	tr := pl.Current()
	// The player bar, with its timer, stops building once this page settles.
	a.tickTimes(c)
	w, h := c.Size()
	art := a.StageArtwork(tr)
	if cs := &a.NPCoverState; a.Playback.Pane == PaneStage || a.Playback.Pane == PaneWall {
		cs.Previous = nil
		cs.Switching = false
	}
	// The colors stay the last song's while its cover loads, rather than
	// going gray between the two.
	shown := art
	if art == nil && a.HasTrackCover(tr) {
		shown = a.NPArt
	}
	a.NPArt = shown
	tint := ui.RGB(120, 120, 130)
	if shown != nil && !shown.ImageFailed() {
		tint = shown.ImageTint()
	}
	accent := tint.Mix(White, 0.45)

	root := ui.Box(c.Key("now-playing")).Absolute().Top(0).Left(0).Right(0).Bottom(0).
		Background(ui.Hex("#0b0b0d")).TextColor(White)
	// The pointer is over the view whenever it is in the window.
	pointerIn := root.Hovered()
	root.Children(func() {
		a.Backdrop(c, shown, tint)
		a.lyricSpectrumLayer(c, tr, tint, h)
		// Over the backdrop, which is positioned too: positioned elements
		// paint over those in the flow.
		ui.Column(c).Absolute().Top(0).Left(0).Right(0).Bottom(0).Children(func() { a.nowPlayingBody(c, tr, art, accent, w, h, pointerIn) })
	})
}

func (a *View) nowPlayingBody(c *ui.Context, tr *netease.Track, art components.Image, accent ui.Color, w, h float32, pointerIn bool) {
	pl := a.Player()
	if tr.IsLocal() {
		if a.Playback.Pane == PaneComments {
			a.Playback.Pane = a.LyricsPaneChosen()
		}
	} else {
		a.CommentsFor(tr)
	}
	// An episode has what it is about and its captions in place of lyrics;
	// a song, lyrics in place of those.
	switch program := tr.ProgramID != 0; {
	case program && a.Playback.Pane == PaneWall:
		a.Playback.Pane = PaneQueue
	case program && (a.Playback.Pane == PaneLyrics || a.Playback.Pane == PaneStage):
		a.Playback.Pane = PaneProgram
		if a.CaptionsOn() {
			a.Playback.Pane = PaneCaptions
		}
	case program && a.Playback.Pane == PaneCaptions && !a.CaptionsShown():
		a.Playback.Pane = PaneProgram
	case !program && (a.Playback.Pane == PaneProgram || a.Playback.Pane == PaneCaptions):
		a.Playback.Pane = a.LyricsPaneChosen()
	}
	pointerIn = a.npPointerActive(c, pointerIn)
	if a.Playback.Pane != PaneQueue {
		a.QueueAt = -1
	}
	if a.Playback.Pane != PaneWall {
		a.WallState.track = 0
	}
	{
		// The top strip drags the window; the chevron closes the view,
		// following the same pointer fade as the pane tabs.
		var closeBounds ui.Rect
		barHeight := shell.TopBar(c)
		ui.Row(c).Height(barHeight).Justify(ui.Center).AlignItems(ui.Center).DragWindow().Children(func() {
			reveal(ui.Box(c.Key("close-reveal")), pointerIn).Children(func() {
				close := components.IconButton(c.Key("close"), components.IconDown, 22, White.Alpha(0.7), "关闭")
				closeBounds = close.Bounds()
				if close.Clicked() {
					a.Playback.Open = false
				}
			})
		})
		if a.Playback.Pane == PaneStage {
			// The stage takes the whole view: the lyrics are the show, the
			// controls a slim bar at the foot.
			a.NPTabsRow(c, npTabsAnchor(c, w, h), pointerIn).Children(func() { a.PaneTabs(c) })
			_, sunk := npCoverLayout(w, h)
			reveal(ui.Row(c.Key("stage-style-row")).Absolute().Top(shell.TopBar(c)+sunk).Right(npSideX(w)).Height(NPTabsHeight), pointerIn).
				Children(func() { a.stageStyleButton(c) })
			ui.Box(c).Height(sunk + NPTabsHeight + npTabsGap)
			a.Stage(c, tr, accent)
			a.stageBar(c, tr, art, accent)
			return
		}
		if a.Playback.Pane == PaneWall {
			// The wall takes the whole view too, under the song playing.
			a.wallPane(c, tr, art, accent, w, h, pointerIn)
			return
		}

		coverSize, coverSunk := npCoverLayout(w, h)
		side := max(coverSize, 280)
		padX := npSideX(w)
		gap := float32(72)
		immersiveLyrics := false
		if a.Playback.Pane == PaneLyrics {
			display, loaded := pl.LyricsDisplay()
			static := loaded && !display.HasSinging()
			immersiveLyrics = a.Config.LyricEdgeFade && !static
			if static {
				gap = min(gap, max(24, w-2*padX-side-lyricInfoMinWidth))
			}
		}
		columns := ui.Row(c).Gap(gap).AlignItems(ui.Stretch)
		tabsTop := barHeight + coverSunk
		if immersiveLyrics {
			// The controls determine the height, from the tabs through the
			// volume row. The lyric viewport extends past both ends.
			columns.Padding(coverSunk, padX, 0, padX)
			coverSunk = 0
		} else {
			columns.Grow(1).Padding(0, padX, npPaneBottom, padX)
		}
		columns.Children(func() {
			// Navigation, cover and controls share one column. Keep its
			// center even when the cover shrinks below the controls' width.
			ui.Column(c).Width(side).Padding(coverSunk, 0, 0, 0).Gap(0).Children(func() {
				anchor := ui.Box(c.Key("tabs-anchor")).Height(NPTabsHeight)
				a.NPTabsRow(c, anchor, pointerIn).Children(func() { a.PaneTabs(c) })
				ui.Box(c).Height(npTabsGap)
				a.NPCover(c, tr, art, coverSize)

				ui.Row(c).Gap(8).AlignItems(ui.Center).Margin(36, 0, 18, 0).Children(func() {
					ui.Column(c).Grow(1).Shrink(1).MinWidth(0).Gap(4).Children(func() {
						if r := pl.Radio(); r != "" {
							a.radioChip(c, r)
						}
						ui.Text(c, tr.Name).FontSize(22).FontWeight(700).LetterSpacing(-0.3).SingleLine()
						a.ArtistLinks(c, tr, White.Alpha(0.6), White, true).FontSize(15).SingleLine()
					})
					a.LikeButton(c.Key("like"), tr, 22, White.Alpha(0.7))
					a.SongMenu(c.Key("more"), tr, 22, White.Alpha(0.7)).Margin(0, -8, 0, 0)
				})
				a.scrubber(c.Key("np-scrub"), &a.NPScrub, White.Alpha(0.18), White.Alpha(0.9), 16, accent.Mix(White, 0.35))
				a.NowPlayingTime(c)
				ui.Box(c).Margin(14, 0, 18, 0).Children(func() {
					a.Transport(c, White, White.Alpha(0.45), accent, 28)
				})
				ui.Row(c).Justify(ui.Center).Children(func() {
					a.volumeSlider(c, White.Alpha(0.7), White.Alpha(0.18), 180)
				})
			})

			pane := ui.Column(c.Key("pane")).Grow(1).Basis(0).MinWidth(0)
			pane.Children(func() {
				if immersiveLyrics {
					// Keep the lyrics and their hit targets below the window
					// strip. Leave 16 to 32 DIPs at the bottom.
					height := max(0, h-barHeight-2*npLyricOverscan)
					ui.Column(c.Key("lyric-viewport")).Absolute().Top(barHeight - tabsTop).Left(0).Right(0).Bottom(-npLyricOverscan).
						MinHeight(height).MaxHeight(max(0, h-barHeight-npLyricOverscan)).
						Children(func() { a.LyricsPane(c, tr, height, closeBounds, (side-coverSize)/2) })
					return
				}
				// Only the cover column sinks in tall windows; this pane keeps
				// the height above it for lyrics, comments and the queue.
				ui.Box(c).Height(NPTabsHeight + npTabsGap)
				switch a.Playback.Pane {
				case PaneLyrics:
					height := max(0, h-barHeight-NPTabsHeight-npTabsGap-npPaneBottom)
					a.LyricsPane(c, tr, height, ui.Rect{}, (side-coverSize)/2)
				case PaneComments:
					a.CommentsPane(c, tr)
				case PaneProgram:
					a.ProgramPane(c, tr, w-2*padX-side-gap, h-barHeight-NPTabsHeight-npTabsGap-npPaneBottom)
				case PaneCaptions:
					a.CaptionsPane(c, tr, h)
				default:
					a.queuePane(c)
				}
			})
		})
	}
}

// npMaxWidth is how wide the cover and the pane beside it go, at most.
const npMaxWidth = 1280

// npSideX is how far in from the sides of a view w wide the cover and the
// pane beside it are: in a wide window they keep together in the middle,
// rather than the pane taking all the width the cover leaves.
func npSideX(w float32) float32 {
	return max(72, (w-npMaxWidth)/2)
}

// NPTabsHeight is how tall the row of Now Playing's tabs is, for what is
// under it to start below it.
const NPTabsHeight = 28

// npTabsGap is the clear space between the tabs and the cover, in DIPs.
const npTabsGap = 24

const npPaneBottom = 44

// npLyricOverscan gives the lyric fade room past the control column, in DIPs.
const npLyricOverscan = 16

// Give static lyrics room for the notice and wrapping credits in narrow windows.
const lyricInfoMinWidth = 200

// npCoverLayout leaves room for navigation and playback controls, then
// puts 35% of the spare height above the group for an upper visual center.
// All panes use it so switching panes cannot move the navigation.
func npCoverLayout(w, h float32) (size, sunk float32) {
	room := max(1, h-436)
	// Windows gives a minimized window a zero-sized content area. The cover
	// still builds in the hidden frame, and its gestures divide by this size.
	size = max(1, min(w*0.36, 440, room))
	return size, (room - size) * 0.35
}

// npTabsAnchor gives panes without a cover column its navigation position,
// so switching panes keeps the same shared column center and top.
func npTabsAnchor(c *ui.Context, w, h float32) ui.Element {
	size, sunk := npCoverLayout(w, h)
	return ui.Box(c.Key("tabs-anchor")).Absolute().Top(shell.TopBar(c)+sunk).Left(npSideX(w)).Size(max(size, 280), NPTabsHeight)
}

// npTabsWidth measures the visible buttons without widening the cover column.
func (a *View) npTabsWidth(c *ui.Context) float32 {
	tabs := a.NPTabs()
	width := float32(len(tabs)-1) * 6
	for _, tab := range tabs {
		text, _ := c.MeasureText(0, ui.Span{Text: tab.Label, Size: 13, Weight: 600})
		width += text + 28
		if tab.Pane == PaneComments {
			var counter string
			if n := a.CommentCount(); n > 0 {
				counter = " " + components.Count(n)
			}
			width += components.RollCountWidth(c, counter)
		}
	}
	return width
}

// NPTabsRow follows the cover's axis, with a 6-DIP optical shift right.
// Clamp the visible buttons, without spare width for the count; that
// transparent space can push the group inward with wider Linux fonts.
func (a *View) NPTabsRow(c *ui.Context, anchor ui.Element, pointerIn bool) ui.Element {
	width := a.npTabsWidth(c)
	row := ui.Row(c.Key("tabs")).AttachTo(anchor, ui.AnchorCenter, ui.AnchorCenter).AttachEdgeMargin(12).Left(6).
		Width(width).Height(NPTabsHeight).Gap(6).AlignItems(ui.Center).Justify(ui.Center)
	return reveal(row, !a.Player().Playing() || pointerIn)
}

const npPointerIdleDelay = 2 * time.Second

// npPointerActive treats an idle pointer like one that left the window.
func (a *View) npPointerActive(c *ui.Context, pointerIn bool) bool {
	if !pointerIn || !a.Player().Playing() || (a.Playback.Pane != PaneLyrics && a.Playback.Pane != PaneStage) {
		a.NPPointerUntil = time.Time{}
		return pointerIn
	}
	// Use window coordinates so animating the page or switching panes
	// cannot look like pointer movement.
	x, y, _ := c.Root().PointerPosition()
	pointer := ui.Point{X: x, Y: y}
	if a.NPPointerUntil.IsZero() || pointer != a.NPPointer {
		a.NPPointer = pointer
		a.NPPointerUntil = c.Now().Add(npPointerIdleDelay)
	}
	if left := a.NPPointerUntil.Sub(c.Now()); left > 0 {
		c.After(left)
		return true
	}
	return false
}

// reveal fades e in while the pointer is in the window, and out when it
// leaves.
func reveal(e ui.Element, shown bool) ui.Element {
	pose := ui.Pose{Fade: 1}
	if shown {
		pose = ui.Pose{}
	}
	return e.AnimatePose(pose, ui.Animation{Duration: 200 * time.Millisecond})
}

// LyricsPaneChosen is the pane of lyrics the listener chose last: what
// the app starts on, before the player is made. Now Playing shows an
// episode's in place of either.
func (a *View) LyricsPaneChosen() int {
	return LyricsPaneChosen(a.Config.LyricsStage)
}

func LyricsPaneChosen(stage bool) int {
	if stage {
		return PaneStage
	}
	return PaneLyrics
}

// The panes of Now Playing.
const (
	PaneLyrics = iota
	PaneQueue
	PaneStage
	PaneComments
	PaneProgram
	PaneCaptions
	PaneWall
)

// NPTab is a tab of Now Playing: its pane, and what it says.
type NPTab struct {
	Pane  int
	Label string
}

// NPTabs returns the tabs of Now Playing, in order, for what plays: ⌘1
// chooses the first, ⌘2 the second, and on.
func (a *View) NPTabs() []NPTab {
	if tr := a.Player().Current(); tr != nil && tr.ProgramID != 0 {
		tabs := []NPTab{{PaneProgram, "简介"}}
		if a.CaptionsShown() {
			tabs = append(tabs, NPTab{PaneCaptions, "字幕"})
		}
		return append(tabs, NPTab{PaneQueue, "待播清单"}, NPTab{PaneComments, "评论"})
	}
	tabs := []NPTab{{PaneLyrics, "歌词"}, {PaneStage, "律动"}, {PaneQueue, "待播清单"}, {PaneWall, "选歌"}}
	if tr := a.Player().Current(); tr == nil || !tr.IsLocal() {
		tabs = append(tabs, NPTab{PaneComments, "评论"})
	}
	return tabs
}

func (a *View) PaneTabs(c *ui.Context) {
	for _, t := range a.NPTabs() {
		label := t.Label
		if t.Pane == PaneComments {
			label = a.CommentsTab()
		}
		a.paneTab(c, t.Pane, label)
	}
}

// ChoosePane shows pane i in Now Playing, remembering lyrics or the stage,
// whichever was chosen, for the next song.
func (a *View) ChoosePane(i int) {
	if i == PaneLyrics && a.Playback.Pane != PaneLyrics {
		a.SeekLyricFollow()
	}
	a.Playback.Pane = i
	if (i == PaneLyrics || i == PaneStage) && a.Config.LyricsStage != (i == PaneStage) {
		a.Config.LyricsStage = i == PaneStage
		a.SaveSettings()
	}
}

// radioChip says which radio plays, over the title: a click on it, or on
// its cross, leaves the radio, playing on the song.
func (a *View) radioChip(c *ui.Context, name string) {
	icon, exit := components.IconRadio, "退出私人 FM"
	if name == radiovm.HeartRadio {
		icon, exit = components.IconHeartFill, "退出心动模式"
	}
	chip := ui.ButtonBase(c.Key("radio-chip")).Gap(5).Padding(3, 6, 3, 8).Margin(0, 0, 4, -8).Radius(999).
		AlignItems(ui.Center).AlignSelf(ui.Start).FocusRing(false).Label(exit).Tooltip(exit)
	bg, fg, cross := ui.Transparent, White.Alpha(0.6), float32(0.5)
	if chip.Hovered() {
		bg, fg, cross = White.Alpha(0.12), White.Alpha(0.85), 1
	}
	chip.Background(chip.AnimateColor("bg", bg, ui.Animation{Duration: 120 * time.Millisecond})).TextColor(fg)
	chip.Children(func() {
		ui.Icon(c, icon).Size(12, 12)
		ui.Text(c, radiovm.RadioLabel(name)).FontSize(11.5).FontWeight(700).LetterSpacing(0.3)
		ui.Icon(c, components.IconX).Size(11, 11).Opacity(cross)
	})
	if chip.Clicked() {
		a.ExitRadio()
	}
}

// The time labels mirror the thumb while dragging. During playback they
// rebuild at a clock boundary, independently of the scrubber's paint-only frames.
func (a *View) NowPlayingTime(c *ui.Context) {
	pl := a.Player()
	duration, position := pl.Duration(), pl.Position()
	preview := a.NPScrub.active
	if preview {
		position = secs(a.NPScrub.v)
	}
	position = max(0, position)
	if duration > 0 {
		position = min(position, duration)
	}

	elapsed := position.Truncate(time.Second)
	status := "—"
	next := time.Second - (position - elapsed)
	if duration > 0 {
		left := duration - position
		remaining := left.Truncate(time.Second)
		if fraction := left - remaining; fraction > 0 {
			next = min(next, fraction)
			remaining += time.Second
		}
		status = components.PlaybackClock(remaining)
		if remaining > 0 {
			status = "−" + status
		}
	}
	loading := pl.Loading()
	if pl.Buffering() {
		status = "正在缓冲…"
	} else if !loading && pl.Err() != "" && !pl.PlaybackRetryable() {
		status = pl.Err()
	}
	if !preview && !loading && pl.Playing() && (duration <= 0 || position < duration) {
		c.After(max(16*time.Millisecond, next))
	}

	// Equal side columns keep the quality badge centered as the labels change.
	ui.Row(c.Key("progress-time")).Height(16).AlignItems(ui.Center).Children(func() {
		label := func(text string) ui.Element {
			return ui.Text(c, text).Grow(1).Basis(0).MinWidth(0).SingleLine().
				FontSize(11).FontFeatures("tnum").TextColor(White.Alpha(0.6))
		}
		label(components.PlaybackClock(elapsed))
		if tr := pl.Current(); tr != nil && tr.IsLocal() {
			ui.Text(c, "本地").FontSize(11).TextColor(White.Alpha(0.6))
		} else {
			qualityBadge(c, pl.Stream())
		}
		label(status).TextAlign(ui.End)
	})
}

// qualityBadge marks a lossless stream, as it plays.
func qualityBadge(c *ui.Context, st *netease.Stream) {
	if st == nil || !netease.Lossless(st.Level) {
		return
	}
	label := "无损"
	if st.Level == "hires" {
		label = "Hi-Res"
	}
	ui.Text(c, label).FontSize(9.5).FontWeight(800).LetterSpacing(0.6).TextColor(ui.Hex("#f5d58a")).
		Padding(1, 6).Radius(4).Border(1, ui.Hex("#f5d58a").Alpha(0.55))
}

// stageBar is the slim bar along the foot of the stage: what plays, where
// it is, and the controls.
func (a *View) stageBar(c *ui.Context, tr *netease.Track, art components.Image, accent ui.Color) {
	pl := a.Player()
	ui.Column(c).Padding(0, 48, 28, 48).Gap(10).Children(func() {
		a.scrubber(c.Key("stage-scrub"), &a.NPScrub, White.Alpha(0.18), White.Alpha(0.9), 14, accent.Mix(White, 0.35))
		ui.Row(c).Gap(14).AlignItems(ui.Center).Children(func() {
			ui.Row(c).Grow(1).Basis(0).MinWidth(0).Gap(12).AlignItems(ui.Center).Children(func() {
				ui.Box(c.Key("stage-cover")).Size(44, 44).Label("律动封面").Children(func() {
					a.StageCover.draw(c, art, a.HasTrackCover(tr), 44, 8, White.Alpha(0.08), White.Alpha(0.35))
				})
				ui.Column(c).Shrink(1).MinWidth(0).Gap(2).Children(func() {
					ui.Text(c, tr.Name).FontSize(14).FontWeight(700).SingleLine()
					a.ArtistLinks(c, tr, White.Alpha(0.55), White, false).FontSize(12).SingleLine()
				})
				a.LikeButton(c.Key("like"), tr, 17, White.Alpha(0.6))
				a.SongMenu(c.Key("more"), tr, 17, White.Alpha(0.6)).Margin(0, 0, 0, -10)
			})
			a.Transport(c, White, White.Alpha(0.45), White, 22)
			ui.Row(c).Grow(1).Basis(0).MinWidth(0).Justify(ui.End).Children(func() {
				ui.Text(c, components.Clock(pl.Position())+" / "+components.Clock(pl.Duration())).FontSize(11.5).FontFeatures("tnum").TextColor(White.Alpha(0.5))
			})
		})
	})
}

func (a *View) paneTab(c *ui.Context, i int, label string) {
	b := ui.ButtonBase(c.Key(i)).Padding(6, 14).Radius(999).FocusRing(false).Label(label)
	bg, fg := ui.Transparent, White.Alpha(0.55)
	if a.Playback.Pane == i {
		bg, fg = White.Alpha(0.14), White
	} else if b.Hovered() {
		bg = White.Alpha(0.06)
	}
	b.Background(b.AnimateColor("bg", bg, ui.Animation{Duration: 120 * time.Millisecond}))
	b.Children(func() {
		if i != PaneComments {
			ui.Text(c, label).FontSize(13).FontWeight(600).TextColor(fg)
			return
		}
		// The count rolls as it changes, from one song's to the next.
		n, text := a.CommentCount(), ""
		if n > 0 {
			text = " " + components.Count(n)
		}
		ui.Text(c, "评论").FontSize(13).FontWeight(600).TextColor(fg)
		components.RollCount(c, &a.TabCount, n, text, fg)
	})
	if b.Clicked() {
		a.ChoosePane(i)
	}
}

func (a *View) LyricsPane(c *ui.Context, tr *netease.Track, height float32, closeBounds ui.Rect, coverInset float32) {
	pl := a.Player()
	display, loaded := pl.LyricsDisplay()
	lines := display.Lines
	var first *lyrics.Line
	if len(lines) > 0 {
		first = &lines[0]
	}
	if a.Reading.Select(tr.Key(), first) {
		a.LyricScroll = ui.ScrollState{}
		a.SeekLyricFollow()
	}
	switch a.Reading.FollowSeek(pl.SeekRevision(), a.Config.LyricEdgeFade) {
	case playbackvm.LyricResumeFollow:
		a.LyricGlide.offset = a.LyricScroll.Y
		a.resumeLyricFollow()
	case playbackvm.LyricInitialFollow:
		a.SeekLyricFollow()
	}
	hasSinging := display.HasSinging()
	if !hasSinging {
		a.LyricBlurPass = nil
	}
	if !loaded && len(lines) == 0 {
		ui.Column(c).Grow(1).Center().Children(func() {
			if pl.LyricsError() != "" {
				a.lyricsNotice(c, 16)
				return
			}
			components.LyricsSkeleton(c, components.PlayerShimmer(c))
		})
		return
	}
	bar := (ui.ScrollbarStyle{}).Resolve(c.Theme())
	// Match the visible thumb to the cover, accounting for its inset in the track.
	thumbInset := (bar.TrackWidth - bar.ThumbWidth) / 2
	right := max(0, coverInset-thumbInset)
	extend := max(0, thumbInset-coverInset)
	key := "lyrics"
	if !hasSinging {
		key = "lyric-info"
	}
	scroll := ui.Scroll(c.Key(key)).Grow(1).TrackScroll(&a.LyricScroll).Margin(0, -extend, 0, 0).Label("歌词")
	if a.Config.LyricEdgeFade {
		scroll.Cache()
	}
	viewport := scroll.LayoutBounds()
	if !hasSinging && viewport.H > 0 {
		right = min(right, max(0, viewport.W-lyricInfoMinWidth-bar.TrackWidth))
	}
	gutter := timedPaneInsetRight + extend
	if hasSinging {
		gutter = max(gutter, right+bar.TrackWidth)
	} else {
		// Credits and the instrumental notice use the available width. Their
		// scrollbar overlays the edge rather than wrapping short lines.
		gutter = 0
	}
	scroll.Padding(0, gutter, 0, 0).ScrollbarInsets(0, right, 0, 0)
	w, h := c.Size()
	original, translation := pl.LyricScripts()
	font := a.PlaybackTypeface(original, c.Locale())
	translatedFont := a.PlaybackTypeface(translation, c.Locale())
	if !hasSinging {
		// A static block centers when short and remains readable by scrolling
		// when long. It has no playback position or automatic scrolling.
		// Credits and instrumental notices leave the whole song eligible for hot comments.
		dim, _ := a.quietShown(c, scroll, tr, nil, loaded)
		quietUnder(scroll, dim)
		defer a.quietCard(c, dim, h*0.18)
		if viewport.W <= 0 || w != a.LyricWindowW || h != a.LyricWindowH {
			// Recheck the scrollbar inset once the pane has a measured width.
			a.LyricWindowW, a.LyricWindowH = w, h
			c.AnimationFrame()
		}
		scroll.Children(func() {
			ui.Column(c).MinHeight(height).Shrink(0).Center().Padding(16, 0).Children(func() {
				ui.Column(c).WidthPercent(100).MaxWidth(480).MinWidth(0).Gap(TimedLineGap).Children(func() {
					lang := pl.LyricLanguage()
					notice := lyrics.Line{Text: display.EmptyText(), Kind: lyrics.Instrumental}
					lyricInfoRow(c.Key("notice"), &notice, lang, translatedFont)
					if tr.IsLocal() {
						ui.Row(c).Children(func() { a.LocalLyricMatchButton(c) })
					}
					for i := range lines {
						if lines[i].Kind == lyrics.Credit {
							lyricInfoRow(c.Key(i), &lines[i], lang, translatedFont)
						}
					}
				})
			})
		})
		return
	}
	pos := pl.Position()
	cur, next := display.Position(pos)
	// Redraw as the next line begins.
	if pl.Playing() && next >= 0 {
		c.After(lines[next].At - pos + 20*time.Millisecond)
	}
	dim, quiet := a.quietShown(c, scroll, tr, lines, loaded)
	if dim > 0.98 {
		// Under the hot comment, gone, not drawn faint.
		scroll.Invisible()
	}
	defer a.quietCard(c, dim, h*0.18)
	style := LinesStyle{Size: 30, Weight: 600, LineHeight: 1.25, quiet: quiet, CenterShort: true, lang: font.Lang,
		family: font.Family, translation: translatedFont, Immersive: a.Config.LyricEdgeFade, rightInset: gutter}
	style.viewportHeight = height
	if style.Immersive {
		// The volume row can extend the viewport beyond its minimum.
		// Keep that extension when resizing until layout measures it again.
		measured := viewport.H
		if measured > 0 && a.LyricWindowH != 0 {
			measured += h - a.LyricWindowH + viewport.Y - shell.TopBar(c)
		}
		style.viewportHeight = min(h, height+npLyricOverscan, max(height, measured))
	}
	// Rewrapping can shrink the scroll extent below the old destination.
	// TrackScroll then clamps it during layout, without any user gesture.
	// Keep that correction out of manual-browse detection and follow the newly
	// measured row even if the viewport dimensions have already stabilized.
	style.reflowed = a.LyricGlide.clamped(&a.LyricScroll)
	if style.reflowed {
		a.LyricGlide.offset = a.LyricScroll.Y
	}
	style.introLine = display.FirstSung()
	if style.Immersive {
		// Keep the opening credits out of the card's entrance and exit.
		style.hideOpeningCredits = quiet || dim > quietCardThreshold
		style.CloseBounds = closeBounds
		scroll.HandleInput(func(ev ui.InputEvent) bool {
			if ev.Kind == ui.InputScroll && ev.DY != 0 && ev.Mods&ui.Shift == 0 && a.LyricScroll.MaxY > 0 {
				a.Reading.Browse(ev.Time)
			}
			return false
		})
		style.browsing = a.lyricBrowsing(c)
		gap := LyricInterludeAt(lines, cur, next)
		switch {
		case gap.To > pos && pos >= gap.From && !quiet:
			if gap.Intro {
				style.intro = gap
			} else if !style.browsing {
				defer func() {
					dots := ui.Box(c.Key("interlude")).Absolute().Right(12).Bottom(8).Size(54, 12)
					a.paintLyricInterlude(c, dots, gap)
				}()
			}
		case gap.To > gap.From && pos < gap.From && pl.Playing():
			c.After(gap.From - pos)
		}
	} else {
		a.Reading.BrowseUntil = time.Time{}
		a.LyricResizeAnchor = lyricReadingAnchor{}
	}
	a.timedLines(c, scroll, lines, cur, style, nil)
}

const LyricBrowseDelay = playbackvm.LyricBrowseDelay

const lyricInitialFollow = playbackvm.LyricInitialFollow

const lyricResumeFollow = playbackvm.LyricResumeFollow

// Layout owns the scroll/glide; the ViewModel decides when manual browsing ends.
func (a *View) lyricBrowsing(c *ui.Context) bool {
	if a.LyricGlide.posed && a.LyricScroll.Y != a.LyricGlide.offset {
		a.Reading.Browse(c.Now())
	}
	browsing, after, resumed := a.Reading.Browsing(c.Now(), a.Player().Playing())
	if after > 0 {
		c.After(after)
	}
	if resumed {
		a.resumeLyricFollow()
	}
	return browsing
}

func (a *View) resumeLyricFollow() {
	a.Reading.Resume()
	a.LyricResizeAnchor = lyricReadingAnchor{}
}

func (a *View) SeekLyricFollow() {
	a.Reading.Seek()
	a.LyricResizeAnchor = lyricReadingAnchor{}
	a.LyricGlide = lyricGlide{}
}

type lyricReadingAnchor struct {
	line  *lyrics.Line
	inset float32
}

// Both lyric layouts share the same recovery action and settled error state.
// Desktop lyrics use the player's short text without interactive controls.
func (a *View) lyricsNotice(c *ui.Context, size float32) {
	ui.Row(c).Gap(8).AlignItems(ui.Center).Children(func() {
		ui.Text(c, a.Player().LyricsError()).FontSize(size).TextColor(White.Alpha(.5))
		if a.Player().LyricsRetryable() && components.IconButton(c.Key("retry-lyrics"), components.IconReload, 18, White.Alpha(.5), "重新加载歌词").Tooltip("重新加载歌词").Clicked() {
			a.Player().RetryMetadata()
		}
	})
}

// LinesStyle is how timedLines sets its lines: the size, weight and
// leading of their type; quiet nearly hides them, under a hot comment.
type LinesStyle struct {
	Size               float32
	Weight             int
	LineHeight         float32
	family             string // empty follows the app's theme; desktop lyrics have their own font
	translation        LyricTypeface
	quiet              bool
	lang               string
	CenterShort        bool // center a whole lyric block that fits; captions keep their growing list
	Immersive          bool
	browsing           bool
	intro              LyricInterlude
	introLine          int
	hideOpeningCredits bool
	CloseBounds        ui.Rect
	rightInset         float32 // lyrics reserve room for the scrollbar; captions use the default
	viewportHeight     float32 // current lyric viewport height, before layout
	reflowed           bool    // layout clamped the previous scroll destination
}

// timedLines lights cur at pos, centering short lyrics when requested.
// Overflowing lines follow playback at the upper third when cur or the
// viewport changes; cur -1 precedes the first line. Clicking a line seeks
// to it. foot, if set, follows the lines.
func (a *View) timedLines(c *ui.Context, scroll ui.Element, lines []lyrics.Line, cur int, st LinesStyle, foot func()) {
	initialFollow := st.CenterShort && a.Reading.At == lyricInitialFollow
	// Bounds below belong to the last layout. A width-only window resize
	// may leave the cover and pane top unchanged: schedule a pass after the
	// new text wrapping is laid out, even if this pass sees the old viewport.
	// Full-screen title-bar changes can arrive after that final resize.
	w, h := c.Size()
	windowChanged := w != a.LyricWindowW || h != a.LyricWindowH || shell.TopBar(c) != a.LyricWindowTop
	if windowChanged {
		a.LyricWindowW, a.LyricWindowH, a.LyricWindowTop = w, h, shell.TopBar(c)
		c.AnimationFrame()
	}
	if len(lines) >= TimedListThreshold {
		a.longTimedLines(c, scroll, lines, cur, st, foot)
		return
	}
	var active, hover, firstSung, slot, dots, content, body, lastRow ui.Element
	var openingCredits []ui.Element
	var credits [2]ui.Element
	viewport := scroll.LayoutBounds()
	box := st.viewport(viewport)
	var textH float32
	var intro LyricIntroMotion
	draw := func() {
		key := c
		if st.CenterShort {
			// New lyrics need a fresh measurement, even at the same line index.
			key = c.Key(&lines[0])
		}
		content = ui.Column(key).Shrink(0)
		if !st.Immersive {
			content.Gap(TimedLineGap)
		}
		content.Children(func() {
			width := st.lineWidth(viewport.W)
			for i := range lines {
				if st.intro.Intro && i == st.introLine {
					slot, dots = LyricIntroDots(c)
				}
				line, hovered := a.TimedLine(c.Key(i), &lines[i], i-cur, width, st)
				lastRow = line
				st.collectLyricRow(line, hovered, i, cur, lines[i].Kind, &active, &hover, &firstSung, &credits)
				if st.intro.Intro && i < st.introLine {
					openingCredits = append(openingCredits, line)
				}
				if st.browsing {
					a.keepLyricReadingPosition(c, &lines[i], line.LayoutBounds(), viewport, windowChanged)
				}
				if st.Immersive && i+1 < len(lines) {
					ui.Box(c.Key(timedLineAfter(i))).Height(TimedLineGap).Shrink(0)
				}
			}
		})
		if st.CenterShort {
			textH = content.LayoutBounds().H
		}
	}
	var moving ui.Element
	scroll.Children(func() {
		if st.CenterShort {
			body = ui.Column(c).MinHeight(box.H).Justify(ui.Center)
			body.Children(draw)
			moving = content
		} else {
			// The recognition status follows the transcript when it moves.
			moving = ui.Column(c).Shrink(0).Children(func() {
				draw()
				if foot != nil {
					foot()
				}
			})
		}
	})
	if !windowChanged {
		a.LyricResizeAnchor = lyricReadingAnchor{}
	}
	waitForLayout := false
	maxY := a.LyricScroll.MaxY
	if st.CenterShort {
		// Layout includes wrapping, translations and per-word text. Measure
		// only the lines, so long lyrics' trailing scroll space cannot make
		// a short block look too tall to center.
		if box.H <= 0 || textH <= 0 {
			// Keep the measurement pass out of view. Until it runs, a new
			// song's rows can sit at the top of an unmeasured viewport.
			body.Invisible()
			c.AnimationFrame()
			return
		}
		if textH <= box.H {
			a.LyricBlurPass = nil
			a.Reading.At = cur
			a.LyricViewportW, a.LyricViewportH = box.W, box.H
			a.LyricScroll.Y, a.LyricGlide, a.Reading.BrowseUntil = 0, lyricGlide{}, time.Time{}
			a.LyricResizeAnchor = lyricReadingAnchor{}
			return
		}
		// Reserve headroom so the first line can reach the reading
		// position with a nonnegative scroll offset.
		head := lyricHeadroom(box.H, st)
		tail := lyricTailPadding(h, box.H, lastRow.LayoutBounds().H)
		body.Padding(head, 0, tail, 0)
		maxY = max(0, textH+head+tail-box.H)
		waitForLayout = Abs32(body.LayoutBounds().H-textH-head-tail) > 1
	}
	resized := box.W > 0 && box.H > 0 && (st.reflowed || box.W != a.LyricViewportW || box.H != a.LyricViewportH)
	// Window geometry already moves the reading position. Do not add a glide
	// from coordinates of the previous viewport, even if a line was moving.
	// Manual browsing retains its own row anchor instead.
	snapResize := st.CenterShort && resized && !st.browsing &&
		(!a.LyricGlide.posed || a.LyricScroll.Y == a.LyricGlide.offset)
	if st.Immersive {
		gap := st.intro
		if !slot.Valid() || textH <= box.H {
			gap = LyricInterlude{}
		}
		intro = a.lyricIntro(c, scroll, gap)
	}
	var remaining func() float32
	if intro.Gap.Intro {
		remaining = a.IntroDots(c, slot, dots, intro)
		extra := float32(0)
		if !st.browsing {
			extra = lyricHeadroom(box.H, st) - lyricReadInset(box.H, firstSung.LayoutBounds().H)
		}
		intro.Move(body, LyricIntroSpace+extra)
		for _, credit := range openingCredits {
			intro.Move(credit, -LyricIntroSpace)
		}
		a.LyricGlide = lyricGlide{offset: a.LyricScroll.Y, posed: true}
	} else {
		dots = ui.Element{}
		if !initialFollow && !snapResize {
			a.LyricGlide.place(c, &a.LyricScroll, scroll, moving, st.browsing)
		}
	}
	if st.Immersive {
		a.LyricEdges(scroll, active, hover, dots, firstSung, remaining, st.browsing, credits, st.rightInset)
	}
	if waitForLayout && !snapResize {
		// Wait for the new padding or wrapping before following the line.
		// Recompute the destination without treating reflow as a new song.
		a.LyricViewportW, a.LyricViewportH = 0, 0
		if initialFollow {
			body.Invisible()
		}
		c.AnimationFrame()
		return
	}
	// Follow a new line, or a resized viewport whose width can reflow it.
	// Unchanged geometry leaves a listener's manual scrolling alone.
	if st.shouldFollow(cur != a.Reading.At, resized, intro.Gap.Intro) {
		target := float32(0)
		followRow := active
		if st.followRow(cur, intro.Gap.Intro) != cur {
			followRow = firstSung
		}
		if followRow.Valid() {
			// Where the glide is going, without where it still is.
			if lb := followRow.LayoutBounds(); lb.H > 0 && box.H > 0 {
				// The line and its column share the same layout offset,
				// even when the listener scrolls during this frame.
				rowTop := lb.Y - content.LayoutBounds().Y
				target = lyricFollowTarget(box.H, lb.H, rowTop, lyricHeadroom(box.H, st))
			} else {
				if initialFollow {
					body.Invisible()
				}
				c.AnimationFrame()
				return // not laid out yet: the next frame scrolls
			}
		}
		snap := st.Immersive && (intro.Gap.Intro || !a.LyricGlide.posed ||
			lyricShouldSnap(c, target, a.LyricScroll.Y, box.H, a.Reading.At == lyricResumeFollow))
		a.Reading.At = cur
		a.LyricViewportW, a.LyricViewportH = box.W, box.H
		target = min(max(0, target), maxY)
		if initialFollow || snapResize {
			// Apply the measured position in this layout without starting a glide.
			a.LyricScroll.Y = target
			a.LyricGlide = lyricGlide{offset: target, posed: true}
		} else if snap && Abs32(target-a.LyricScroll.Y) > 0.5 {
			a.LyricScroll.Y, a.LyricGlide = target, lyricGlide{}
			c.AnimationFrame()
		} else if !snap {
			a.LyricGlide.to(c, a.LyricScroll.Y, target)
		}
	}
}

func (st LinesStyle) viewport(box ui.Rect) ui.Rect {
	if st.CenterShort {
		box.H = st.viewportHeight
	}
	return box
}

// On resize, retain the first visible lyric and its offset through reflow.
func (a *View) keepLyricReadingPosition(c *ui.Context, line *lyrics.Line, row, viewport ui.Rect, windowChanged bool) {
	anchor := &a.LyricResizeAnchor
	if row.H <= 0 {
		return
	}
	if windowChanged {
		if anchor.line == nil && row.Y+row.H > viewport.Y {
			*anchor = lyricReadingAnchor{line: line, inset: max(0, viewport.Y-row.Y)}
		}
	} else if anchor.line == line {
		inset := min(anchor.inset, row.H-1)
		a.LyricScroll.Y = max(0, min(a.LyricScroll.MaxY, a.LyricScroll.Y+row.Y-viewport.Y+inset))
		a.LyricGlide = lyricGlide{}
		c.AnimationFrame()
	}
}

func lyricReadInset(viewport, line float32) float32 {
	return min(viewport*0.3, max(0, viewport-line))
}

// Leave enough space after the last row for it to reach the reading inset.
func lyricTailPadding(window, viewport, lastRow float32) float32 {
	return max(window*0.5, viewport-lyricReadInset(viewport, lastRow)-lastRow)
}

func (st LinesStyle) shouldFollow(lineChanged, reflow, intro bool) bool {
	return !st.browsing && (lineChanged || reflow || intro)
}

func (st LinesStyle) followRow(current int, intro bool) int {
	if intro {
		return st.introLine
	}
	return current
}

// rowTop excludes the headroom in either list layout.
func lyricFollowTarget(viewport, rowHeight, rowTop, head float32) float32 {
	return max(0, head+rowTop-lyricReadInset(viewport, rowHeight))
}

func lyricShouldSnap(c *ui.Context, target, current, viewport float32, resuming bool) bool {
	return c.Preferences().ReduceMotion || !resuming && Abs32(target-current) > viewport
}

func lyricHeadroom(viewport float32, st LinesStyle) float32 {
	if st.Immersive {
		return viewport * 0.3
	}
	return 0
}

// lyricGlide puts the scroll layout at its destination in one build. A
// matching translation cancels that jump, and AnimatePose slides the whole
// column there in frames that reuse the existing layout and text.
type lyricGlide struct {
	offset, target float32
	posed, pending bool
	settleAt       time.Time
}

// Keep the same critically damped spring, at half-pixel precision.
var lyricScrollMotion = ui.Animation{Spring: ui.SpringOptions{Stiffness: 100, Damping: 20, Precision: 0.5}}

// A changed offset alone is not evidence of input: layout clamps an offset
// outside its new extent. In-range wheel and scrollbar movement still counts.
func (g lyricGlide) clamped(s *ui.ScrollState) bool {
	return g.posed && s.Y != g.offset && s.Y == min(max(0, g.offset), s.MaxY)
}

func (g *lyricGlide) to(c *ui.Context, offset, target float32) {
	if offset == target {
		g.pending = false
		return
	}
	g.target, g.pending = target, true
	c.AnimationFrame()
}

func (g *lyricGlide) place(c *ui.Context, s *ui.ScrollState, scroll, content ui.Element, browsing bool) {
	if g.posed && (s.Y != g.offset || browsing && content.Transitioning()) {
		// A wheel, trackpad or scrollbar moved the layout offset. Take over
		// at the currently painted position, including that input's delta.
		if box := content.LayoutBounds(); box.H > 0 && content.Transitioning() {
			s.Y = min(max(0, s.Y+lyricPoseLag(scroll, content)), s.MaxY)
		}
		*g = lyricGlide{}
		return
	}
	if browsing {
		g.pending = false
	}
	if g.pending {
		s.Y, g.pending = min(g.target, s.MaxY), false
		if g.posed && !c.Preferences().ReduceMotion && !c.Instant() {
			g.settleAt = c.Now().Add(time.Second)
		}
	}
	if !g.settleAt.IsZero() && !c.Now().Before(g.settleAt) && content.Transitioning() {
		// A large seek can take longer to settle than a single line.
		g.settleAt = c.Now().Add(200 * time.Millisecond)
	}
	if left := g.settleAt.Sub(c.Now()); left > 0 {
		// Paint-only frames move the lines, but a build commits the final
		// clipping and visibility for accessibility after they arrive.
		c.After(left)
	} else {
		g.settleAt = time.Time{}
	}
	g.offset, g.posed = s.Y, true
	content.Translate(0, s.Y).AnimatePose(ui.Pose{Y: -s.Y}, lyricScrollMotion)
}

// lyricPoseLag is the visual offset left between the scroll layout and its painted content.
func lyricPoseLag(scroll, content ui.Element) float32 {
	x, y := content.LocalToWindow(0, 0)
	if _, shown, ok := scroll.WindowToLocal(x, y); ok {
		return content.LayoutBounds().Y - scroll.LayoutBounds().Y - shown
	}
	return 0
}

// queuePane lists the whole queue in play order, the track playing marked
// among the others: a click plays one there, and the list stays as it is.
func (a *View) queuePane(c *ui.Context) {
	pl := a.Player()
	queue := pl.Queue()
	order, at := pl.PlayOrder()
	revision := pl.QueueRevision()
	// Opened, it shows the track playing at its top; after, it follows it
	// as little as keeps it in view.
	if at >= 0 && at != a.QueueAt {
		if a.QueueAt < 0 {
			a.QueueList.ScrollTo(at, ui.Start)
		} else {
			a.QueueList.ScrollIntoView(at)
		}
		a.QueueAt = at
	}
	a.QueueList.Key = func(row int) any { return order[row] }
	ui.List(c.Key("queue"), &a.QueueList, len(order), func(r int) {
		i := order[r]
		tr := &queue[i]
		playing := r == at
		local := tr.IsLocal()
		row := ui.ButtonBase(c.Key(i)).Height(58).PaddingX(10).Gap(14).Radius(12).AlignItems(ui.Center).FocusRing(false).Label(tr.Name)
		if local {
			row.ContextMenu(func(m *ui.Menu) {
				// The same index can hold another song after a queue edit.
				if m.Item("从待播清单移除").ID(strconv.FormatUint(revision, 10)).Disabled(playing).Chosen() {
					pl.RemoveQueueEntry(i)
				}
			})
		} else {
			row.ContextMenu(a.SongItems(tr, 0))
		}
		switch {
		case playing:
			row.Background(White.Alpha(0.12))
		case row.Hovered():
			row.Background(White.Alpha(0.08))
		}
		if !tr.Playable() {
			row.Opacity(0.4)
		}
		row.Children(func() {
			a.TrackCover(c, tr, 96, 42, 7)
			ui.Column(c).Grow(1).MinWidth(0).Gap(3).Children(func() {
				ui.Text(c, tr.Name).FontSize(14).FontWeight(600).SingleLine()
				a.ArtistLinks(c, tr, White.Alpha(0.55), White, false).FontSize(12.5).SingleLine()
			})
			if local {
				ui.Text(c, "本地").FontSize(11).TextColor(White.Alpha(0.6)).SingleLine().Shrink(0)
			}
			ui.Text(c, components.Clock(tr.Length())).FontSize(12).FontFeatures("tnum").TextColor(White.Alpha(0.4))
		})
		if row.Clicked() && tr.Playable() && !playing {
			pl.Jump(i)
		}
	}).Grow(1).Padding(8, 0, 40, 0).Gap(2)
}
