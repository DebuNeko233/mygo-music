package app

import (
	"strconv"
	"time"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"

	"netease-music-mygo/internal/lyrics"
	"netease-music-mygo/internal/netease"
)

var white = ui.RGB(255, 255, 255)

var npSpring = ui.Animation{Spring: ui.SpringOptions{Duration: 450 * time.Millisecond, Bounce: 0.08}}

// npSettle is how long Now Playing takes to come or go, its spring at rest.
const npSettle = 600 * time.Millisecond

// npState tracks Now Playing opening and closing, and reports whether it is
// open and whether it has come to rest, open or closed. Call it once a
// build, before the window's content.
func (a *app) npState(c *ui.Context) (open, settled bool) {
	open = a.nowPlaying && a.pl.Current() != nil
	now := c.Now()
	if open != a.npOpen {
		a.npOpen, a.npChangedAt = open, now
		a.npPointerUntil = time.Time{}
		if open && a.npPane == paneLyrics {
			// The new scroll may clamp before it has measured its
			// contents. Start following before reading that offset.
			a.seekLyricFollow()
		}
		// A build once it is at rest: to stop painting what it covers, or
		// itself once gone.
		c.After(npSettle)
		// The window buttons light over its dark backdrop, and as the
		// theme over the pages (Windows).
		if a.win != nil {
			buttons := mygo.ThemeSystem
			if open {
				buttons = mygo.ThemeDark
			}
			a.win.SetWindowButtonsTheme(buttons)
		}
	}
	return open, now.Sub(a.npChangedAt) >= npSettle
}

// nowPlayingLayer holds Now Playing over the whole window, rising into
// view as it opens and sinking away as it closes. The layer moves itself,
// staying on top all the way, rather than leaving as an exit transition
// would, under the rest of the window: that would show it going over the
// sidebar's material while the page above it hid it at once. Its pose
// moves it in frames that only paint, building nothing.
func (a *app) nowPlayingLayer(c *ui.Context, open, settled bool) {
	layer := ui.Box(c.Key("now-playing-layer")).Absolute().Top(0).Left(0).Right(0).Bottom(0)
	pose := ui.Pose{Y: 60, Fade: 1}
	if open {
		pose = ui.Pose{}
	}
	layer.AnimatePose(pose, npSpring)
	if !open {
		a.npSpectrum = lyricSpectrum{}
	}
	if !open && settled || a.pl.Current() == nil {
		// Closed: nothing to paint or click.
		layer.Invisible()
		a.queueAt, a.wallState.track = -1, 0
		a.npCoverState.previous = nil
		a.npCoverState.switching = false
		a.lyricBlurPass = nil
		return
	}
	if !open {
		// On its way out, the window under it takes the clicks.
		layer.PassThrough()
	}
	layer.Children(func() { a.nowPlayingView(c) })
	if a.npPane != paneLyrics || !a.settings.LyricEdgeFade {
		// The framebuffer is only reused while the effect is in view.
		a.lyricBlurPass = nil
	}
}

// nowPlayingView fills the window with what plays: its cover over a slow
// drift of its own colors, the controls, and lyrics that follow the song.
func (a *app) nowPlayingView(c *ui.Context) {
	pl := a.pl
	tr := pl.Current()
	// The player bar, with its timer, stops building once this page settles.
	a.tickTimes(c)
	w, h := c.Size()
	var art *artwork
	if tr.IsLocal() {
		art = a.trackArtwork(tr, nowPlayingCoverPixels, true)
	} else {
		art = a.playbackArtwork(tr.Album.PicURL, nowPlayingCoverRequest)
	}
	if cs := &a.npCoverState; a.npPane == paneStage || a.npPane == paneWall {
		cs.previous = nil
		cs.switching = false
	}
	// The colors stay the last song's while its cover loads, rather than
	// going gray between the two.
	shown := art
	if art == nil && a.hasTrackCover(tr) {
		shown = a.npArt
	}
	a.npArt = shown
	tint := ui.RGB(120, 120, 130)
	if shown != nil && !shown.failed {
		tint = shown.tint
	}
	accent := tint.Mix(white, 0.45)

	root := ui.Box(c.Key("now-playing")).Absolute().Top(0).Left(0).Right(0).Bottom(0).
		Background(ui.Hex("#0b0b0d")).TextColor(white)
	// The pointer is over the view whenever it is in the window.
	pointerIn := root.Hovered()
	root.Children(func() {
		a.backdrop(c, shown, tint)
		a.lyricSpectrumLayer(c, tr, tint, h)
		// Over the backdrop, which is positioned too: positioned elements
		// paint over those in the flow.
		ui.Column(c).Absolute().Top(0).Left(0).Right(0).Bottom(0).Children(func() { a.nowPlayingBody(c, tr, art, accent, w, h, pointerIn) })
	})
}

func (a *app) nowPlayingBody(c *ui.Context, tr *netease.Track, art *artwork, accent ui.Color, w, h float32, pointerIn bool) {
	pl := a.pl
	if tr.IsLocal() {
		if a.npPane == paneComments {
			a.npPane = a.lyricsPaneChosen()
		}
	} else {
		a.commentsFor(tr)
	}
	// An episode has what it is about and its captions in place of lyrics;
	// a song, lyrics in place of those.
	switch program := tr.ProgramID != 0; {
	case program && a.npPane == paneWall:
		a.npPane = paneQueue
	case program && (a.npPane == paneLyrics || a.npPane == paneStage):
		a.npPane = paneProgram
		if a.captionsOn() {
			a.npPane = paneCaptions
		}
	case program && a.npPane == paneCaptions && !a.captionsShown():
		a.npPane = paneProgram
	case !program && (a.npPane == paneProgram || a.npPane == paneCaptions):
		a.npPane = a.lyricsPaneChosen()
	}
	pointerIn = a.npPointerActive(c, pointerIn)
	if a.npPane != paneQueue {
		a.queueAt = -1
	}
	if a.npPane != paneWall {
		a.wallState.track = 0
	}
	{
		// The top strip drags the window; the chevron closes the view,
		// following the same pointer fade as the pane tabs.
		var closeBounds ui.Rect
		barHeight := topBar(c)
		ui.Row(c).Height(barHeight).Justify(ui.Center).AlignItems(ui.Center).DragWindow().Children(func() {
			reveal(ui.Box(c.Key("close-reveal")), pointerIn).Children(func() {
				close := iconButton(c.Key("close"), iconDown, 22, white.Alpha(0.7), "关闭")
				closeBounds = close.Bounds()
				if close.Clicked() {
					a.nowPlaying = false
				}
			})
		})
		if a.npPane == paneStage {
			// The stage takes the whole view: the lyrics are the show, the
			// controls a slim bar at the foot.
			a.npTabsRow(c, npTabsAnchor(c, w, h), pointerIn).Children(func() { a.paneTabs(c) })
			_, sunk := npCoverLayout(w, h)
			reveal(ui.Row(c.Key("stage-style-row")).Absolute().Top(topBar(c)+sunk).Right(npSideX(w)).Height(npTabsHeight), pointerIn).
				Children(func() { a.stageStyleButton(c) })
			ui.Box(c).Height(sunk + npTabsHeight + npTabsGap)
			a.stage(c, tr, accent)
			a.stageBar(c, tr, art, accent)
			return
		}
		if a.npPane == paneWall {
			// The wall takes the whole view too, under the song playing.
			a.wallPane(c, tr, art, accent, w, h, pointerIn)
			return
		}

		coverSize, coverSunk := npCoverLayout(w, h)
		side := max(coverSize, 280)
		padX := npSideX(w)
		gap := float32(72)
		immersiveLyrics := false
		if a.npPane == paneLyrics {
			display, loaded := pl.LyricsDisplay()
			static := loaded && !display.HasSinging()
			immersiveLyrics = a.settings.LyricEdgeFade && !static
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
				anchor := ui.Box(c.Key("tabs-anchor")).Height(npTabsHeight)
				a.npTabsRow(c, anchor, pointerIn).Children(func() { a.paneTabs(c) })
				ui.Box(c).Height(npTabsGap)
				a.npCover(c, tr, art, coverSize)

				ui.Row(c).Gap(8).AlignItems(ui.Center).Margin(36, 0, 18, 0).Children(func() {
					ui.Column(c).Grow(1).Shrink(1).MinWidth(0).Gap(4).Children(func() {
						if r := pl.Radio(); r != "" {
							a.radioChip(c, r)
						}
						ui.Text(c, tr.Name).FontSize(22).FontWeight(700).LetterSpacing(-0.3).SingleLine()
						a.artistLinks(c, tr, white.Alpha(0.6), white, true).FontSize(15).SingleLine()
					})
					a.likeButton(c.Key("like"), tr, 22, white.Alpha(0.7))
					a.songMenu(c.Key("more"), tr, 22, white.Alpha(0.7)).Margin(0, -8, 0, 0)
				})
				a.scrubber(c.Key("np-scrub"), &a.npScrub, white.Alpha(0.18), white.Alpha(0.9), 16, accent.Mix(white, 0.35))
				a.nowPlayingTime(c)
				ui.Box(c).Margin(14, 0, 18, 0).Children(func() {
					a.transport(c, white, white.Alpha(0.45), accent, 28)
				})
				ui.Row(c).Justify(ui.Center).Children(func() {
					a.volumeSlider(c, white.Alpha(0.7), white.Alpha(0.18), 180)
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
						Children(func() { a.lyricsPane(c, tr, height, closeBounds, (side-coverSize)/2) })
					return
				}
				// Only the cover column sinks in tall windows; this pane keeps
				// the height above it for lyrics, comments and the queue.
				ui.Box(c).Height(npTabsHeight + npTabsGap)
				switch a.npPane {
				case paneLyrics:
					height := max(0, h-barHeight-npTabsHeight-npTabsGap-npPaneBottom)
					a.lyricsPane(c, tr, height, ui.Rect{}, (side-coverSize)/2)
				case paneComments:
					a.commentsPane(c, tr)
				case paneProgram:
					a.programPane(c, tr, w-2*padX-side-gap, h-barHeight-npTabsHeight-npTabsGap-npPaneBottom)
				case paneCaptions:
					a.captionsPane(c, tr, h)
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

// npTabsHeight is how tall the row of Now Playing's tabs is, for what is
// under it to start below it.
const npTabsHeight = 28

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
	return ui.Box(c.Key("tabs-anchor")).Absolute().Top(topBar(c)+sunk).Left(npSideX(w)).Size(max(size, 280), npTabsHeight)
}

// npTabsWidth measures the visible buttons without widening the cover column.
func (a *app) npTabsWidth(c *ui.Context) float32 {
	tabs := a.npTabs()
	width := float32(len(tabs)-1) * 6
	for _, tab := range tabs {
		text, _ := c.MeasureText(0, ui.Span{Text: tab.label, Size: 13, Weight: 600})
		width += text + 28
		if tab.pane == paneComments {
			var counter string
			if n := a.commentCount(); n > 0 {
				counter = " " + count(n)
			}
			width += rollCountWidth(c, counter)
		}
	}
	return width
}

// npTabsRow follows the cover's axis, with a 6-DIP optical shift right.
// Clamp the visible buttons, without spare width for the count; that
// transparent space can push the group inward with wider Linux fonts.
func (a *app) npTabsRow(c *ui.Context, anchor ui.Element, pointerIn bool) ui.Element {
	width := a.npTabsWidth(c)
	row := ui.Row(c.Key("tabs")).AttachTo(anchor, ui.AnchorCenter, ui.AnchorCenter).AttachEdgeMargin(12).Left(6).
		Width(width).Height(npTabsHeight).Gap(6).AlignItems(ui.Center).Justify(ui.Center)
	return reveal(row, !a.pl.Playing() || pointerIn)
}

const npPointerIdleDelay = 2 * time.Second

// npPointerActive treats an idle pointer like one that left the window.
func (a *app) npPointerActive(c *ui.Context, pointerIn bool) bool {
	if !pointerIn || !a.pl.Playing() || (a.npPane != paneLyrics && a.npPane != paneStage) {
		a.npPointerUntil = time.Time{}
		return pointerIn
	}
	// Use window coordinates so animating the page or switching panes
	// cannot look like pointer movement.
	x, y, _ := c.Root().PointerPosition()
	pointer := ui.Point{X: x, Y: y}
	if a.npPointerUntil.IsZero() || pointer != a.npPointer {
		a.npPointer = pointer
		a.npPointerUntil = c.Now().Add(npPointerIdleDelay)
	}
	if left := a.npPointerUntil.Sub(c.Now()); left > 0 {
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

// lyricsPaneChosen is the pane of lyrics the listener chose last: what
// the app starts on, before the player is made. Now Playing shows an
// episode's in place of either.
func (a *app) lyricsPaneChosen() int {
	if a.settings.LyricsStage {
		return paneStage
	}
	return paneLyrics
}

// The panes of Now Playing.
const (
	paneLyrics = iota
	paneQueue
	paneStage
	paneComments
	paneProgram
	paneCaptions
	paneWall
)

// npTab is a tab of Now Playing: its pane, and what it says.
type npTab struct {
	pane  int
	label string
}

// npTabs returns the tabs of Now Playing, in order, for what plays: ⌘1
// chooses the first, ⌘2 the second, and on.
func (a *app) npTabs() []npTab {
	if tr := a.pl.Current(); tr != nil && tr.ProgramID != 0 {
		tabs := []npTab{{paneProgram, "简介"}}
		if a.captionsShown() {
			tabs = append(tabs, npTab{paneCaptions, "字幕"})
		}
		return append(tabs, npTab{paneQueue, "待播清单"}, npTab{paneComments, "评论"})
	}
	tabs := []npTab{{paneLyrics, "歌词"}, {paneStage, "律动"}, {paneQueue, "待播清单"}, {paneWall, "选歌"}}
	if tr := a.pl.Current(); tr == nil || !tr.IsLocal() {
		tabs = append(tabs, npTab{paneComments, "评论"})
	}
	return tabs
}

func (a *app) paneTabs(c *ui.Context) {
	for _, t := range a.npTabs() {
		label := t.label
		if t.pane == paneComments {
			label = a.commentsTab()
		}
		a.paneTab(c, t.pane, label)
	}
}

// choosePane shows pane i in Now Playing, remembering lyrics or the stage,
// whichever was chosen, for the next song.
func (a *app) choosePane(i int) {
	if i == paneLyrics && a.npPane != paneLyrics {
		a.seekLyricFollow()
	}
	a.npPane = i
	if (i == paneLyrics || i == paneStage) && a.settings.LyricsStage != (i == paneStage) {
		a.settings.LyricsStage = i == paneStage
		a.settings.saveSoon()
	}
}

// radioChip says which radio plays, over the title: a click on it, or on
// its cross, leaves the radio, playing on the song.
func (a *app) radioChip(c *ui.Context, name string) {
	icon, exit := iconRadio, "退出私人 FM"
	if name == heartRadio {
		icon, exit = iconHeartFill, "退出心动模式"
	}
	chip := ui.ButtonBase(c.Key("radio-chip")).Gap(5).Padding(3, 6, 3, 8).Margin(0, 0, 4, -8).Radius(999).
		AlignItems(ui.Center).AlignSelf(ui.Start).FocusRing(false).Label(exit).Tooltip(exit)
	bg, fg, cross := ui.Transparent, white.Alpha(0.6), float32(0.5)
	if chip.Hovered() {
		bg, fg, cross = white.Alpha(0.12), white.Alpha(0.85), 1
	}
	chip.Background(chip.AnimateColor("bg", bg, ui.Animation{Duration: 120 * time.Millisecond})).TextColor(fg)
	chip.Children(func() {
		ui.Icon(c, icon).Size(12, 12)
		ui.Text(c, radioLabel(name)).FontSize(11.5).FontWeight(700).LetterSpacing(0.3)
		ui.Icon(c, iconX).Size(11, 11).Opacity(cross)
	})
	if chip.Clicked() {
		a.exitRadio()
	}
}

// The time labels mirror the thumb while dragging. During playback they
// rebuild at a clock boundary, independently of the scrubber's paint-only frames.
func (a *app) nowPlayingTime(c *ui.Context) {
	pl := a.pl
	duration, position := pl.Duration(), pl.Position()
	preview := a.npScrub.active
	if preview {
		position = secs(a.npScrub.v)
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
		status = playbackClock(remaining)
		if remaining > 0 {
			status = "−" + status
		}
	}
	loading := pl.Loading()
	if loading {
		status = "正在缓冲…"
	} else if pl.Err() != "" && !pl.PlaybackRetryable() {
		status = pl.Err()
	}
	if !preview && !loading && pl.Playing() && (duration <= 0 || position < duration) {
		c.After(max(16*time.Millisecond, next))
	}

	// Equal side columns keep the quality badge centered as the labels change.
	ui.Row(c.Key("progress-time")).Height(16).AlignItems(ui.Center).Children(func() {
		label := func(text string) ui.Element {
			return ui.Text(c, text).Grow(1).Basis(0).MinWidth(0).SingleLine().
				FontSize(11).FontFeatures("tnum").TextColor(white.Alpha(0.6))
		}
		label(playbackClock(elapsed))
		if tr := pl.Current(); tr != nil && tr.IsLocal() {
			ui.Text(c, "本地").FontSize(11).TextColor(white.Alpha(0.6))
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
func (a *app) stageBar(c *ui.Context, tr *netease.Track, art *artwork, accent ui.Color) {
	pl := a.pl
	ui.Column(c).Padding(0, 48, 28, 48).Gap(10).Children(func() {
		a.scrubber(c.Key("stage-scrub"), &a.npScrub, white.Alpha(0.18), white.Alpha(0.9), 14, accent.Mix(white, 0.35))
		ui.Row(c).Gap(14).AlignItems(ui.Center).Children(func() {
			ui.Row(c).Grow(1).Basis(0).MinWidth(0).Gap(12).AlignItems(ui.Center).Children(func() {
				ui.Box(c.Key("stage-cover")).Size(44, 44).Label("律动封面").Children(func() {
					a.stageCover.draw(c, art, a.hasTrackCover(tr), 44, 8, white.Alpha(0.08), white.Alpha(0.35))
				})
				ui.Column(c).Shrink(1).MinWidth(0).Gap(2).Children(func() {
					ui.Text(c, tr.Name).FontSize(14).FontWeight(700).SingleLine()
					a.artistLinks(c, tr, white.Alpha(0.55), white).FontSize(12).SingleLine()
				})
				a.likeButton(c.Key("like"), tr, 17, white.Alpha(0.6))
				a.songMenu(c.Key("more"), tr, 17, white.Alpha(0.6)).Margin(0, 0, 0, -10)
			})
			a.transport(c, white, white.Alpha(0.45), white, 22)
			ui.Row(c).Grow(1).Basis(0).MinWidth(0).Justify(ui.End).Children(func() {
				ui.Text(c, clock(pl.Position())+" / "+clock(pl.Duration())).FontSize(11.5).FontFeatures("tnum").TextColor(white.Alpha(0.5))
			})
		})
	})
}

func (a *app) paneTab(c *ui.Context, i int, label string) {
	b := ui.ButtonBase(c.Key(i)).Padding(6, 14).Radius(999).FocusRing(false).Label(label)
	bg, fg := ui.Transparent, white.Alpha(0.55)
	if a.npPane == i {
		bg, fg = white.Alpha(0.14), white
	} else if b.Hovered() {
		bg = white.Alpha(0.06)
	}
	b.Background(b.AnimateColor("bg", bg, ui.Animation{Duration: 120 * time.Millisecond}))
	b.Children(func() {
		if i != paneComments {
			ui.Text(c, label).FontSize(13).FontWeight(600).TextColor(fg)
			return
		}
		// The count rolls as it changes, from one song's to the next.
		n, text := a.commentCount(), ""
		if n > 0 {
			text = " " + count(n)
		}
		ui.Text(c, "评论").FontSize(13).FontWeight(600).TextColor(fg)
		rollCount(c, &a.tabCount, n, text, fg)
	})
	if b.Clicked() {
		a.choosePane(i)
	}
}

func (a *app) lyricsPane(c *ui.Context, tr *netease.Track, height float32, closeBounds ui.Rect, coverInset float32) {
	pl := a.pl
	display, loaded := pl.LyricsDisplay()
	lines := display.Lines
	var first *lyrics.Line
	if len(lines) > 0 {
		first = &lines[0]
	}
	if a.lyricTrack != tr.Key() || a.lyricFirst != first {
		a.lyricTrack, a.lyricFirst = tr.Key(), first
		a.lyricScroll = ui.ScrollState{}
		a.seekLyricFollow()
	}
	if revision := pl.SeekRevision(); revision != a.lyricSeekRevision {
		a.lyricSeekRevision = revision
		if revision == a.lyricClickSeekRevision && a.lyricAt != lyricInitialFollow {
			// A clicked row is already in view. Follow it from the visible
			// position, including when the listener had scrolled manually.
			a.lyricGlide.offset = a.lyricScroll.Y
			a.resumeLyricFollow()
		} else if a.settings.LyricEdgeFade {
			a.seekLyricFollow()
		}
	}
	hasSinging := display.HasSinging()
	if !hasSinging {
		a.lyricBlurPass = nil
	}
	if !loaded && len(lines) == 0 {
		ui.Column(c).Grow(1).Center().Children(func() {
			if pl.LyricsError() != "" {
				a.lyricsNotice(c, 16)
				return
			}
			lyricsSkeleton(c, npShimmer(c))
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
	scroll := ui.Scroll(c.Key(key)).Grow(1).TrackScroll(&a.lyricScroll).Margin(0, -extend, 0, 0).Label("歌词")
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
	font := a.playbackTypeface(original, c.Locale())
	translatedFont := a.playbackTypeface(translation, c.Locale())
	if !hasSinging {
		// A static block centers when short and remains readable by scrolling
		// when long. It has no playback position or automatic scrolling.
		// Credits and instrumental notices leave the whole song eligible for hot comments.
		dim, _ := a.quietShown(c, scroll, tr, nil, loaded)
		quietUnder(scroll, dim)
		defer a.quietCard(c, dim, h*0.18)
		if viewport.W <= 0 || w != a.lyricWindowW || h != a.lyricWindowH {
			// Recheck the scrollbar inset once the pane has a measured width.
			a.lyricWindowW, a.lyricWindowH = w, h
			c.AnimationFrame()
		}
		scroll.Children(func() {
			ui.Column(c).MinHeight(height).Shrink(0).Center().Padding(16, 0).Children(func() {
				ui.Column(c).WidthPercent(100).MaxWidth(480).MinWidth(0).Gap(timedLineGap).Children(func() {
					lang := pl.LyricLanguage()
					notice := lyrics.Line{Text: display.EmptyText(), Kind: lyrics.Instrumental}
					lyricInfoRow(c.Key("notice"), &notice, lang, translatedFont)
					if tr.IsLocal() {
						ui.Row(c).Children(func() { a.localLyricMatchButton(c) })
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
	style := linesStyle{size: 30, weight: 600, lineHeight: 1.25, quiet: quiet, centerShort: true, lang: font.lang,
		family: font.family, translation: translatedFont, immersive: a.settings.LyricEdgeFade, rightInset: gutter}
	style.viewportHeight = height
	if style.immersive {
		// The volume row can extend the viewport beyond its minimum.
		// Keep that extension when resizing until layout measures it again.
		measured := viewport.H
		if measured > 0 && a.lyricWindowH != 0 {
			measured += h - a.lyricWindowH + viewport.Y - topBar(c)
		}
		style.viewportHeight = min(h, height+npLyricOverscan, max(height, measured))
	}
	style.introLine = display.FirstSung()
	if style.immersive {
		// Keep the opening credits out of the card's entrance and exit.
		style.hideOpeningCredits = quiet || dim > quietCardThreshold
		style.closeBounds = closeBounds
		scroll.HandleInput(func(ev ui.InputEvent) bool {
			if ev.Kind == ui.InputScroll && ev.DY != 0 && ev.Mods&ui.Shift == 0 && a.lyricScroll.MaxY > 0 {
				a.lyricBrowseUntil = ev.Time.Add(lyricBrowseDelay)
			}
			return false
		})
		style.browsing = a.lyricBrowsing(c)
		gap := lyricInterludeAt(lines, cur, next)
		switch {
		case gap.to > pos && pos >= gap.from && !quiet:
			if gap.intro {
				style.intro = gap
			} else if !style.browsing {
				defer func() {
					dots := ui.Box(c.Key("interlude")).Absolute().Right(12).Bottom(8).Size(54, 12)
					a.paintLyricInterlude(c, dots, gap)
				}()
			}
		case gap.to > gap.from && pos < gap.from && pl.Playing():
			c.After(gap.from - pos)
		}
	} else {
		a.lyricBrowseUntil = time.Time{}
		a.lyricResizeAnchor = lyricReadingAnchor{}
	}
	a.timedLines(c, scroll, lines, cur, style, nil)
}

const lyricBrowseDelay = 4 * time.Second

// The scroll state's offset differs from the one we applied when the
// listener moves the wheel, touchpad, or scrollbar.
func (a *app) lyricBrowsing(c *ui.Context) bool {
	if a.lyricGlide.posed && a.lyricScroll.Y != a.lyricGlide.offset {
		a.lyricBrowseUntil = c.Now().Add(lyricBrowseDelay)
	}
	if a.lyricBrowseUntil.IsZero() {
		return false
	}
	if !a.pl.Playing() {
		return true
	}
	if left := a.lyricBrowseUntil.Sub(c.Now()); left > 0 {
		c.After(left)
		return true
	}
	a.resumeLyricFollow()
	return false
}

const lyricInitialFollow = -2

// Resume following even if playback is still on the same line.
const lyricResumeFollow = -3

func (a *app) resumeLyricFollow() {
	a.lyricBrowseUntil = time.Time{}
	a.lyricResizeAnchor = lyricReadingAnchor{}
	a.lyricAt = lyricResumeFollow
}

func (a *app) seekLyricFollow() {
	a.lyricBrowseUntil = time.Time{}
	a.lyricResizeAnchor = lyricReadingAnchor{}
	a.lyricGlide = lyricGlide{}
	a.lyricAt = lyricInitialFollow
}

type lyricReadingAnchor struct {
	line  *lyrics.Line
	inset float32
}

// Both lyric layouts share the same recovery action and settled error state.
// Desktop lyrics use the player's short text without interactive controls.
func (a *app) lyricsNotice(c *ui.Context, size float32) {
	ui.Row(c).Gap(8).AlignItems(ui.Center).Children(func() {
		ui.Text(c, a.pl.LyricsError()).FontSize(size).TextColor(white.Alpha(.5))
		if a.pl.LyricsRetryable() && iconButton(c.Key("retry-lyrics"), iconReload, 18, white.Alpha(.5), "重新加载歌词").Tooltip("重新加载歌词").Clicked() {
			a.pl.RetryMetadata()
		}
	})
}

// linesStyle is how timedLines sets its lines: the size, weight and
// leading of their type; quiet nearly hides them, under a hot comment.
type linesStyle struct {
	size               float32
	weight             int
	lineHeight         float32
	family             string // empty follows the app's theme; desktop lyrics have their own font
	translation        lyricTypeface
	quiet              bool
	lang               string
	centerShort        bool // center a whole lyric block that fits; captions keep their growing list
	immersive          bool
	browsing           bool
	intro              lyricInterlude
	introLine          int
	hideOpeningCredits bool
	closeBounds        ui.Rect
	rightInset         float32 // lyrics reserve room for the scrollbar; captions use the default
	viewportHeight     float32 // current lyric viewport height, before layout
}

// timedLines lights cur at pos, centering short lyrics when requested.
// Overflowing lines follow playback at the upper third when cur or the
// viewport changes; cur -1 precedes the first line. Clicking a line seeks
// to it. foot, if set, follows the lines.
func (a *app) timedLines(c *ui.Context, scroll ui.Element, lines []lyrics.Line, cur int, st linesStyle, foot func()) {
	initialFollow := st.centerShort && a.lyricAt == lyricInitialFollow
	// Bounds below belong to the last layout. A width-only window resize
	// may leave the cover and pane top unchanged: schedule a pass after the
	// new text wrapping is laid out, even if this pass sees the old viewport.
	w, h := c.Size()
	windowChanged := w != a.lyricWindowW || h != a.lyricWindowH
	if windowChanged {
		a.lyricWindowW, a.lyricWindowH = w, h
		c.AnimationFrame()
	}
	if len(lines) >= timedListThreshold {
		a.longTimedLines(c, scroll, lines, cur, st, foot)
		return
	}
	var active, hover, firstSung, slot, dots, content, body, lastRow ui.Element
	var openingCredits []ui.Element
	var credits [2]ui.Element
	viewport := scroll.LayoutBounds()
	box := st.viewport(viewport)
	var textH float32
	var intro lyricIntroMotion
	draw := func() {
		key := c
		if st.centerShort {
			// New lyrics need a fresh measurement, even at the same line index.
			key = c.Key(&lines[0])
		}
		content = ui.Column(key).Shrink(0)
		if !st.immersive {
			content.Gap(timedLineGap)
		}
		content.Children(func() {
			width := st.lineWidth(viewport.W)
			for i := range lines {
				if st.intro.intro && i == st.introLine {
					slot, dots = lyricIntroDots(c)
				}
				line, hovered := a.timedLine(c.Key(i), &lines[i], i-cur, width, st)
				lastRow = line
				st.collectLyricRow(line, hovered, i, cur, lines[i].Kind, &active, &hover, &firstSung, &credits)
				if st.intro.intro && i < st.introLine {
					openingCredits = append(openingCredits, line)
				}
				if st.browsing {
					a.keepLyricReadingPosition(c, &lines[i], line.LayoutBounds(), viewport, windowChanged)
				}
				if st.immersive && i+1 < len(lines) {
					ui.Box(c.Key(timedLineAfter(i))).Height(timedLineGap).Shrink(0)
				}
			}
		})
		if st.centerShort {
			textH = content.LayoutBounds().H
		}
	}
	var moving ui.Element
	scroll.Children(func() {
		if st.centerShort {
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
		a.lyricResizeAnchor = lyricReadingAnchor{}
	}
	waitForLayout := false
	maxY := a.lyricScroll.MaxY
	if st.centerShort {
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
			a.lyricBlurPass = nil
			a.lyricAt = cur
			a.lyricViewportW, a.lyricViewportH = box.W, box.H
			a.lyricScroll.Y, a.lyricGlide, a.lyricBrowseUntil = 0, lyricGlide{}, time.Time{}
			a.lyricResizeAnchor = lyricReadingAnchor{}
			return
		}
		// Reserve headroom so the first line can reach the reading
		// position with a nonnegative scroll offset.
		head := lyricHeadroom(box.H, st)
		tail := lyricTailPadding(h, box.H, lastRow.LayoutBounds().H)
		body.Padding(head, 0, tail, 0)
		maxY = max(0, textH+head+tail-box.H)
		waitForLayout = abs32(body.LayoutBounds().H-textH-head-tail) > 1
	}
	heightChanged := st.centerShort && box.H != a.lyricViewportH && a.lyricViewportH > 0
	// A settled line should move with the viewport. Leave active glides
	// and manual scrolling alone while the text is remeasured.
	snapResize := heightChanged && cur == a.lyricAt && !st.browsing &&
		(!a.lyricGlide.posed || a.lyricScroll.Y == a.lyricGlide.offset) &&
		!a.lyricGlide.pending && !moving.Transitioning()
	if st.immersive {
		gap := st.intro
		if !slot.Valid() || textH <= box.H {
			gap = lyricInterlude{}
		}
		intro = a.lyricIntro(c, scroll, gap)
	}
	var remaining func() float32
	if intro.gap.intro {
		remaining = a.introDots(c, slot, dots, intro)
		extra := float32(0)
		if !st.browsing {
			extra = lyricHeadroom(box.H, st) - lyricReadInset(box.H, firstSung.LayoutBounds().H)
		}
		intro.move(body, lyricIntroSpace+extra)
		for _, credit := range openingCredits {
			intro.move(credit, -lyricIntroSpace)
		}
		a.lyricGlide = lyricGlide{offset: a.lyricScroll.Y, posed: true}
	} else {
		dots = ui.Element{}
		if !initialFollow && !snapResize {
			a.lyricGlide.place(c, &a.lyricScroll, scroll, moving, st.browsing)
		}
	}
	if st.immersive {
		a.lyricEdges(scroll, active, hover, dots, firstSung, remaining, st.browsing, credits, st.rightInset)
	}
	if waitForLayout && !snapResize {
		// Wait for the new padding or wrapping before following the line.
		// Recompute the destination without treating reflow as a new song.
		a.lyricViewportW, a.lyricViewportH = 0, 0
		if initialFollow {
			body.Invisible()
		}
		c.AnimationFrame()
		return
	}
	// Follow a new line, or a resized viewport whose width can reflow it.
	// Unchanged geometry leaves a listener's manual scrolling alone.
	resized := box.W > 0 && box.H > 0 && (box.W != a.lyricViewportW || box.H != a.lyricViewportH)
	if st.shouldFollow(cur != a.lyricAt, resized, intro.gap.intro) {
		target := float32(0)
		followRow := active
		if st.followRow(cur, intro.gap.intro) != cur {
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
		snap := st.immersive && (intro.gap.intro || !a.lyricGlide.posed ||
			lyricShouldSnap(c, target, a.lyricScroll.Y, box.H, a.lyricAt == lyricResumeFollow))
		a.lyricAt = cur
		a.lyricViewportW, a.lyricViewportH = box.W, box.H
		target = min(max(0, target), maxY)
		if initialFollow || snapResize {
			// Apply the measured position in this layout without starting a glide.
			a.lyricScroll.Y = target
			a.lyricGlide = lyricGlide{offset: target, posed: true}
		} else if snap && abs32(target-a.lyricScroll.Y) > 0.5 {
			a.lyricScroll.Y, a.lyricGlide = target, lyricGlide{}
			c.AnimationFrame()
		} else if !snap {
			a.lyricGlide.to(c, a.lyricScroll.Y, target)
		}
	}
}

func (st linesStyle) viewport(box ui.Rect) ui.Rect {
	if st.centerShort {
		box.H = st.viewportHeight
	}
	return box
}

// On resize, retain the first visible lyric and its offset through reflow.
func (a *app) keepLyricReadingPosition(c *ui.Context, line *lyrics.Line, row, viewport ui.Rect, windowChanged bool) {
	anchor := &a.lyricResizeAnchor
	if row.H <= 0 {
		return
	}
	if windowChanged {
		if anchor.line == nil && row.Y+row.H > viewport.Y {
			*anchor = lyricReadingAnchor{line: line, inset: max(0, viewport.Y-row.Y)}
		}
	} else if anchor.line == line {
		inset := min(anchor.inset, row.H-1)
		a.lyricScroll.Y = max(0, min(a.lyricScroll.MaxY, a.lyricScroll.Y+row.Y-viewport.Y+inset))
		a.lyricGlide = lyricGlide{}
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

func (st linesStyle) shouldFollow(lineChanged, reflow, intro bool) bool {
	return !st.browsing && (lineChanged || reflow || intro)
}

func (st linesStyle) followRow(current int, intro bool) int {
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
	return c.Preferences().ReduceMotion || !resuming && abs32(target-current) > viewport
}

func lyricHeadroom(viewport float32, st linesStyle) float32 {
	if st.immersive {
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
func (a *app) queuePane(c *ui.Context) {
	pl := a.pl
	queue := pl.Queue()
	order, at := pl.PlayOrder()
	revision := pl.QueueRevision()
	// Opened, it shows the track playing at its top; after, it follows it
	// as little as keeps it in view.
	if at >= 0 && at != a.queueAt {
		if a.queueAt < 0 {
			a.queueList.ScrollTo(at, ui.Start)
		} else {
			a.queueList.ScrollIntoView(at)
		}
		a.queueAt = at
	}
	a.queueList.Key = func(row int) any { return order[row] }
	ui.List(c.Key("queue"), &a.queueList, len(order), func(r int) {
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
			row.ContextMenu(a.songItems(tr, 0))
		}
		switch {
		case playing:
			row.Background(white.Alpha(0.12))
		case row.Hovered():
			row.Background(white.Alpha(0.08))
		}
		if !tr.Playable() {
			row.Opacity(0.4)
		}
		row.Children(func() {
			a.trackCover(c, tr, 96, 42, 7)
			ui.Column(c).Grow(1).MinWidth(0).Gap(3).Children(func() {
				ui.Text(c, tr.Name).FontSize(14).FontWeight(600).SingleLine()
				a.artistLinks(c, tr, white.Alpha(0.55), white).FontSize(12.5).SingleLine()
			})
			if local {
				ui.Text(c, "本地").FontSize(11).TextColor(white.Alpha(0.6)).SingleLine().Shrink(0)
			}
			ui.Text(c, clock(tr.Length())).FontSize(12).FontFeatures("tnum").TextColor(white.Alpha(0.4))
		})
		if row.Clicked() && tr.Playable() && !playing {
			pl.Jump(i)
		}
	}).Grow(1).Padding(8, 0, 40, 0).Gap(2)
}
