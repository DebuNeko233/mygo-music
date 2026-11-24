package settings

import (
	"fmt"
	"math"
	"runtime"
	"slices"
	"strings"
	"time"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/plugins/glass"
	"github.com/egoist/mygo/ui"

	"netease-music-mygo/internal/desktop/view/components"
	"netease-music-mygo/internal/desktop/view/page"
	"netease-music-mygo/internal/desktop/view/shell"
	"netease-music-mygo/internal/preferences"
	accountvm "netease-music-mygo/internal/viewmodel/account"
	captionvm "netease-music-mygo/internal/viewmodel/captions"
	downloadvm "netease-music-mygo/internal/viewmodel/downloads"
	linksvm "netease-music-mygo/internal/viewmodel/links"
	settingsvm "netease-music-mygo/internal/viewmodel/settings"
)

type Choice struct{ Value, Label string }
type CacheLimit struct {
	MB    int
	Label string
}
type State struct {
	Recording, RecordingNote string
	SearchBox                ui.Handle
	UnderTop                 bool
	DownloadList             ui.ListState
}

const kindInApp = "inApp"
const kindGlobal = "global"

type View struct {
	*page.Page
	State       *State
	Config      *preferences.Settings
	Preferences *settingsvm.Preferences
	Cache       *settingsvm.CacheSettings
	Account     *accountvm.Account
	Links       *linksvm.Links
	Captions    *captionvm.Captions
	Downloads   *downloadvm.Downloads
	BrowseUsed  *int64
	ImagesUsed  *int64

	AppIcons          []Choice
	Qualities         []Choice
	Hotkeys           []Choice
	Accents           []Choice
	LyricColors       []Choice
	NameFormats       []Choice
	CacheLimits       []CacheLimit
	PictureExtensions []string
	SystemFontLabel   string

	Theme           func(*ui.Theme) ui.Theme
	Colors          func(*ui.Theme) components.Palette
	Close           func()
	Changed         func()
	Save            func()
	RegisterLink    func()
	ChangeAppIcon   func()
	NormalizeVolume func()
	ShowLogin       func()
	SignOut         func()

	AddPictures    func([]string, *[]string)
	ForgetPictures func()
	PictureList    func([]string) string
	FontNames      func() []string
	LyricColor     func(string, ui.Color) ui.Color
	AccentColor    func(string, bool) ui.Color

	HotkeyLabel    func(string) string
	HotkeyError    func(string) string
	Binding        func(string, string) string
	ChangedBinding func(string, string) bool
	RestoreBinding func(string, string)
	SetBinding     func(string, string, string)
	Taken          func(string, string, string) (string, bool)
	AcceleratorFor func(ui.Modifiers, ui.Key) (string, bool)
	ValidGlobal    func(string) bool

	HasNotch           func() bool
	CaptionsShown      func() bool
	Updates            func() bool
	UpdateChecks       func() bool
	UpdateDownloads    func() bool
	ModelReady         func() bool
	LastUpdateCheck    func() time.Time
	SetUpdateChecks    func(bool)
	SetUpdateDownloads func(bool)
	CheckForUpdates    func()

	ChooseCacheDirectory      func()
	ApplyCacheLimit           func()
	ClearCache                func()
	ClearBrowseCache          func()
	ClearImageCache           func()
	CacheUsage                func() string
	CacheDirectoryDescription func() string
	DownloadDirectory         func() string
	CacheMoveLabel            func() string
	ConfirmCacheDirectory     func(string)
	SubmitCacheDirectory      func(bool)
	CancelCacheChange         func()

	ChooseDownloadDirectory func() bool
	RevealDownload          func(string)
	CancelDownload          func(*downloadvm.DownloadJob)
	RetryDownload           func(*downloadvm.DownloadJob)

	SyncCaptions        func()
	StartModelDownload  func()
	CancelModelDownload func()
	DeleteModel         func()
	CommentTime         func(time.Time, time.Time) string
	Avatar              func(*ui.Context, string, float32, ui.Color)
}

// backdropRow is the pictures of one page: those chosen, more added from
// the disk or dropped on the row, and all of them let go.
func (a *View) backdropRow(c *ui.Context, p components.Palette, key string, paths *[]string) {
	ui.Column(c).Gap(8).Children(func() {
		line := ui.Row(c).Gap(8).AlignItems(ui.Center)
		// Anything dropped on the row is a picture of this page's.
		if dropped := line.DroppedFiles(); len(dropped) > 0 {
			a.AddPictures(dropped, paths)
		}
		line.Children(func() {
			if ui.Button(c.Key(key+"-add"), "选择图片…").Tooltip("选择多张图片时，切歌会轮换背景").Clicked() {
				files, err := mygo.Dialog.Open(mygo.OpenDialogOptions{
					Title:    "选择背景图片",
					Filters:  []mygo.FileFilter{{Name: "图片", Extensions: a.PictureExtensions}},
					Multiple: true,
				})
				if err == nil {
					a.AddPictures(files, paths)
				}
			}
			if ui.Button(c.Key(key+"-clear"), "清空").Disabled(len(*paths) == 0).Clicked() {
				*paths = nil
				a.ForgetPictures()
				a.Changed()
			}
			summary := a.PictureList(*paths)
			status := ui.Text(c, summary).FontSize(12).TextColor(p.Muted).SingleLine().Shrink(1).MinWidth(0)
			if len(*paths) != 0 {
				status.Tooltip(summary)
			}
		})
	})
}

// keyField is the control of a shortcut: the key an action has, which the
// listener changes by clicking it and pressing the key they want, and a
// button giving the action its own key back once it has another.
func (a *View) keyField(c *ui.Context, p components.Palette, kind string, name string) {
	recording := a.State.Recording == string(kind)+"/"+name
	label := a.HotkeyLabel(a.Binding(kind, name))
	if recording {
		label = "按下按键…"
	}
	// The default is not a button: only a key the listener changed can be
	// given back.
	changed := a.ChangedBinding(kind, name)
	ui.Row(c).Gap(8).AlignItems(ui.Center).Children(func() {
		// The default is not a button: only a key the listener changed can
		// be given back. It sits before the key, which keeps the keys in one
		// column whether or not they were changed.
		if changed && !recording {
			if components.IconButton(c.Key("default-"+string(kind)+"-"+name), components.IconReload, 16, p.Muted, "恢复默认").Clicked() {
				a.RestoreBinding(kind, name)
			}
		} else {
			// A key with its own needs no button, and the room the button
			// would take keeps every key in one column.
			ui.Box(c).Size(16, 16)
		}
		field := ui.Button(c.Key("key-"+string(kind)+"-"+name), label).Width(150)
		switch {
		case recording:
			field.Focusable().Focus().HandleInput(func(ev ui.InputEvent) bool {
				return a.RecordKey(kind, name, ev)
			})
			if a.State.RecordingNote != "" {
				ui.Text(c, a.State.RecordingNote).FontSize(12).TextColor(p.Muted)
			}
		case field.Clicked():
			a.State.Recording, a.State.RecordingNote = string(kind)+"/"+name, ""
		default:
			// The system keeps the key for another app, or another action
			// of the app asked for it first.
			if err := a.HotkeyError(name); kind == kindGlobal && err != "" {
				ui.Text(c, err).FontSize(12).TextColor(p.Warn)
			}
		}
	})
}

// RecordKey takes the key pressed for the binding being recorded, and gives
// the action that key. It reports false for an event it leaves alone.
func (a *View) RecordKey(kind string, name string, ev ui.InputEvent) bool {
	if ev.Kind != ui.InputKeyDown {
		return false
	}
	switch ev.Key {
	case ui.KeyEscape:
		a.State.Recording, a.State.RecordingNote = "", ""
		return true
	case ui.KeyBackspace, ui.KeyDelete:
		a.SetBinding(kind, name, "")
		a.State.Recording, a.State.RecordingNote = "", ""
		return true
	}
	acc, ok := a.AcceleratorFor(ev.Mods, ev.Key)
	if !ok {
		// A modifier on its own: the key it goes with is still to come.
		return true
	}
	if kind == kindGlobal && !a.ValidGlobal(acc) {
		a.State.RecordingNote = "全局快捷键要带 Ctrl、⌘ 或 Alt"
		return true
	}
	if other, dup := a.Taken(kind, name, acc); dup {
		a.State.RecordingNote = other + "已经是" + a.HotkeyLabel(acc)
		return true
	}
	a.SetBinding(kind, name, acc)
	a.State.Recording, a.State.RecordingNote = "", ""
	return true
}

// Row is a setting of the settings window: what it is, what it means,
// and its control. It shows if a search finds it.
type Row func(label, desc string, control func())

// Block is anything else in the settings, which a search finds by text.
type Block func(text string, draw func())

// settingsSection is a page of the settings. Its build only says what it has
// through row and block, for a search to go through them without drawing.
type settingsSection struct {
	id, title string
	icon      *ui.SVG
	build     func(row Row, block Block)
}

// settingsMatch reports whether a search, lower case, finds any of the texts.
func settingsMatch(query string, texts ...string) bool {
	for _, t := range texts {
		if strings.Contains(strings.ToLower(t), query) {
			return true
		}
	}
	return false
}

func (a *View) Build(c *ui.Context) {
	t := a.Theme(c.Theme())
	p := a.Colors(&t)
	p.ApplyAccent(&t)
	c.SetTheme(&t)
	c.Root().Background(p.Background)
	c.OnShortcut(ui.Cmd, ui.KeyW, func() {
		a.Close()
	})
	c.OnShortcut(ui.Cmd, ui.KeyF, func() { a.State.SearchBox.Focus(c) })

	sections := a.sections(c, p)
	current := sections[0]
	for _, sec := range sections {
		if sec.id == a.Preferences.Section {
			current = sec
		}
	}
	query := strings.ToLower(strings.TrimSpace(a.Preferences.Query))

	ui.Row(c).Fill().AlignItems(ui.Stretch).Children(func() {
		// Under the window buttons where they sit over the sidebar. Where nothing
		// takes the press, it moves the window, as the title bar it reaches into would.
		top := float32(14)
		if c.TitleBar().Left > 0 {
			top = shell.TopBar(c) + 4
		}
		ui.Column(c).Width(settingsSidebarWidth).Shrink(0).Padding(top, 12, 14, 12).Gap(2).Background(p.Sidebar).DragWindow().Children(func() {
			a.Search(c, p)
			ui.Scroll(c.Key("settings-nav")).Grow(1).MinHeight(0).Gap(2).Children(func() {
				for _, sec := range sections {
					a.navItem(c, p, sec, query == "" && sec.id == current.id)
				}
			})
		})
		ui.Box(c).Width(1).Shrink(0).Background(p.Hairline)
		// The page's title is in the header, the strip the window buttons leave. What
		// scrolls goes under a hard edge there, showing only once there's something under it.
		pad := max(24, shell.TopBar(c)+6)
		ui.Box(c).Grow(1).MinWidth(0).Children(func() {
			ui.Scroll(c.Key(current.id+"\x00"+query)).Fill().Padding(pad, 28, 32, 28).Gap(8).Children(func() {
				mark := ui.Box(c.Key("start-mark")).Height(0).Bounds()
				if mark.W > 0 {
					a.State.UnderTop = mark.Y < pad-0.5
				}
				if query == "" {
					a.group(c, p, current, "", "")
					return
				}
				found := false
				for _, sec := range sections {
					if a.finds(sec, query) {
						found = true
						a.group(c, p, sec, sec.title, query)
					}
				}
				if !found {
					ui.Text(c, "未找到相关设置").FontSize(13.5).TextColor(p.Muted).Margin(8, 4, 0, 4)
				}
			})
			edge := ui.Box(c.Key("top-edge")).Absolute().Top(0).Left(0).Right(0).Height(shell.TopBar(c)).DragWindow().
				Material(glass.ScrollEdge{Hard: true, Background: p.Background})
			title := current.title
			if query != "" {
				title = "搜索结果"
			}
			// Leave room for the window buttons on Linux and Windows.
			right := max(32, c.TitleBar().Right+12)
			header := ui.Row(c.Key("header")).Absolute().Top(0).Left(0).Right(0).Height(shell.TopBar(c)).Padding(0, right, 0, 32).AlignItems(ui.Center).DragWindow()
			header.Children(func() {
				ui.Text(c, title).FontSize(15).FontWeight(700).SingleLine().Shrink(1).MinWidth(0)
			})
			// The pointer over it brings the edge up too, as AppKit's does.
			edge.Opacity(shell.EdgeShown(edge, a.State.UnderTop || header.Hovered()))
		})
	})
	a.CacheDialog(c, p)
}

// settingsSidebarWidth is the width of the list of the settings' pages.
const settingsSidebarWidth = 200

// settingsSearch is the field that finds settings across all the pages.
func (a *View) Search(c *ui.Context, p components.Palette) {
	field := ui.Row(c).Height(30).Radius(8).AlignItems(ui.Stretch).Margin(0, 0, 10, 0)
	if a.State.SearchBox.Focused(c) {
		field.Background(p.Background).Shadow(0, 0, 0, 3, p.Accent.Alpha(0.25)).Border(1, p.Accent)
	} else {
		field.Background(p.Hover).Border(1, p.Hairline)
	}
	field.Children(func() {
		// The input fills the field, under its icon, so a click anywhere on
		// the field focuses it.
		ui.TextInputBase(c.Key("search"), &a.Preferences.Query).Bind(&a.State.SearchBox).Grow(1).MinWidth(0).PaddingStart(29).PaddingEnd(9).
			Placeholder("搜索").FontSize(13).Label("搜索设置")
		ui.Icon(c, components.IconSearch).Size(13, 13).TextColor(p.Muted).Absolute().Left(9).Top(7.5).IgnoreInput(true)
	})
}

// settingsNavItem opens a page of the settings, leaving a search.
func (a *View) navItem(c *ui.Context, p components.Palette, sec settingsSection, selected bool) {
	b := ui.ButtonBase(c.Key(sec.id)).Height(32).PaddingX(9).Gap(9).Radius(8).AlignItems(ui.Center).Justify(ui.Start).
		FocusRing(false).Label(sec.title)
	switch {
	case selected:
		b.Background(p.Press)
	case b.Hovered():
		b.Background(p.Hover)
	}
	color := p.Muted
	if selected {
		color = p.Accent
	}
	b.Children(func() {
		ui.Icon(c, sec.icon).Size(15, 15).TextColor(color)
		ui.Text(c, sec.title).FontSize(13).FontWeight(500).SingleLine().Shrink(1).MinWidth(0)
	})
	if b.Clicked() {
		a.Preferences.Open(sec.id)
	}
}

// settingsFinds reports whether a search finds anything in a section: all of it,
// if it's what the section is called.
func (a *View) finds(sec settingsSection, query string) bool {
	if settingsMatch(query, sec.title) {
		return true
	}
	found := false
	sec.build(func(label, desc string, _ func()) {
		found = found || settingsMatch(query, label, desc)
	}, func(text string, _ func()) {
		found = found || settingsMatch(query, text)
	})
	return found
}

// settingsGroup draws a section's rows, those a search finds if there is one,
// under its title if it has one.
func (a *View) group(c *ui.Context, p components.Palette, sec settingsSection, title, query string) {
	if title != "" {
		ui.Text(c, title).FontSize(12).FontWeight(600).TextColor(p.Muted).Margin(14, 4, 4, 4)
	}
	all := query == "" || settingsMatch(query, sec.title)
	ui.Column(c.Key(sec.id)).Radius(12).Background(p.Background).Border(1, p.Hairline).Dividers(1, p.Hairline).Children(func() {
		sec.build(func(label, desc string, control func()) {
			if !all && !settingsMatch(query, label, desc) {
				return
			}
			ui.Row(c).Padding(12, 16).Gap(16).AlignItems(ui.Center).MinHeight(48).Children(func() {
				ui.Column(c).Grow(1).Shrink(1).MinWidth(0).Gap(3).Children(func() {
					ui.Text(c, label).FontSize(13.5).FontWeight(500)
					if desc != "" {
						ui.Text(c, desc).FontSize(12).TextColor(p.Muted).LineHeight(1.4)
					}
				})
				control()
			})
		}, func(text string, draw func()) {
			if all || settingsMatch(query, text) {
				draw()
			}
		})
	})
}

// settingsSections describe the fork's existing controls without constructing
// them. Search can inspect labels/descriptions without running UI or I/O.
func (a *View) sections(c *ui.Context, p components.Palette) []settingsSection {
	s := a.Config
	var sections []settingsSection
	add := func(id, title string, icon *ui.SVG, build func(Row, Block)) {
		sections = append(sections, settingsSection{id, title, icon, build})
	}
	font := func(row Row, key, label, desc string, chosen *string) {
		row(label, desc, func() {
			names := a.FontNames()
			current := *chosen
			if current == "" {
				current = a.SystemFontLabel
			}
			// Keep showing a saved choice from another system or an older version.
			if !slices.Contains(names, current) {
				names = append([]string{a.SystemFontLabel, current}, names[1:]...)
			}
			if ui.Combobox(c.Key(key), &current, names).Label("选择" + label).Width(180).Changed() {
				if current == a.SystemFontLabel {
					current = ""
				}
				*chosen = current
				a.Changed()
				c.AnimationFrame()
			}
		})
	}

	add("general", "通用", components.IconSettings, func(row Row, block Block) {
		row("用本应用打开网易云链接", "", func() {
			label := "设为默认打开"
			if a.Links.Registered {
				label = "已设为默认"
			}
			ui.Column(c).Gap(6).AlignItems(ui.End).Children(func() {
				if ui.Button(c.Key("music-link-handler"), label).Label("设为网易云链接默认应用").Disabled(a.Links.Registered).Clicked() {
					a.RegisterLink()
				}
				if a.Links.RegistrationError != "" {
					ui.Text(c, a.Links.RegistrationError).FontSize(12).TextColor(p.Warn)
				}
			})
		})
		row("外观", "", func() {
			modes := []string{"system", "light", "dark"}
			i := 0
			for k, m := range modes {
				if m == s.Appearance {
					i = k
				}
			}
			if components.Segmented(c.Key("appearance"), &i, "跟随系统", "浅色", "深色") {
				s.Appearance = modes[i]
				a.Changed()
			}
		})
		if runtime.GOOS == "darwin" {

			row("应用图标", "显示在菜单栏时，关闭窗口后可从菜单栏图标打开", func() {
				labels := make([]string, len(a.AppIcons))
				chosen := a.AppIcons[0].Label
				for i, ic := range a.AppIcons {
					labels[i] = ic.Label
					if ic.Value == s.AppIcon {
						chosen = ic.Label
					}
				}
				if ui.Select(c.Key("app-icon"), &chosen, labels).Label("应用图标").Width(150).Changed() {
					for _, ic := range a.AppIcons {
						if ic.Label == chosen {
							s.AppIcon = ic.Value
						}
					}
					a.Changed()
					a.ChangeAppIcon()
				}
			})

		}
	})

	if a.HasNotch() {

		add("notch", "刘海播放器", components.IconLaptop, func(row Row, block Block) {

			row("显示刘海播放器", "在屏幕顶部的刘海处显示迷你播放器，鼠标悬停即可展开", func() {
				if ui.Switch(c.Key("notch"), &s.Notch).Label("显示刘海播放器").Changed() {
					a.Changed()
				}
			})
			row("播放时显示实时活动", "在刘海两侧显示封面与音频波形，可能遮挡刘海旁的菜单栏图标", func() {
				if ui.Switch(c.Key("activity"), &s.NotchActivity).Label("播放时显示实时活动").Disabled(!s.Notch).Changed() {
					a.Changed()
				}
			})
			row("在无刘海的屏幕上常驻显示", "关闭时，将鼠标移至屏幕顶部中央即可唤出，不会遮挡菜单栏", func() {
				if ui.Switch(c.Key("virtual"), &s.NotchVirtual).Label("在无刘海的屏幕上常驻显示").Disabled(!s.Notch).Changed() {
					a.Changed()
				}
			})

		})

	}

	add("playback", "播放", components.IconVolume, func(row Row, block Block) {

		row("音质", "无损与 Hi-Res 需要黑胶 VIP，没有时播放所能播放的最高音质；下一首歌曲开始生效", func() {
			if a.qualitySelect(c.Key("quality"), &s.Quality, "音质") {
				a.Changed()
			}
		})
		row("间奏时显示热评", "前奏、间奏与尾奏时，歌词处换成这首歌的热门评论", func() {
			if ui.Switch(c.Key("hot-comments"), &s.HotComments).Label("间奏时显示热评").Changed() {
				a.Changed()
			}
		})
		row("音量均衡", "按网易云音乐测得的响度调整每首歌的音量，让歌曲听起来一样响", func() {
			if ui.Switch(c.Key("normalize"), &s.Normalize).Label("音量均衡").Changed() {
				a.NormalizeVolume()
				a.Changed()
			}
		})

	})

	add("download", "下载", components.IconDownload, func(row Row, block Block) {
		a.DownloadSettings(c, p, row, block)
	})

	add("keys", "快捷键", components.IconKeyboard, func(row Row, block Block) {

		block("键盘 快捷键 按键", func() {
			ui.Column(c).Padding(12, 16).Children(func() {
				ui.Text(c, "这些键在应用内按下即生效。点一下右边的键，再按下想要的按键；Backspace 清除，Esc 取消。改动过的键旁边有一个按钮，点了就收回默认的键。").
					FontSize(12.5).TextColor(p.Muted).LineHeight(1.4).Wrap()
			})
		})
		for _, h := range a.Hotkeys {
			row(h.Label, "", func() { a.keyField(c, p, kindInApp, h.Value) })
		}

	})

	add("globalkeys", "全局快捷键", components.IconKeyboard, func(row Row, block Block) {

		block("键盘 全局快捷键 系统 后台", func() {
			ui.Column(c).Padding(12, 16).Children(func() {
				ui.Text(c, "应用在后台时也能用的键。系统的键由所有应用共用，所以都要带 Ctrl、⌘ 或 Alt，或者用多媒体键；多媒体键在应用内录不到，换掉之后点旁边的按钮就回到它。").
					FontSize(12.5).TextColor(p.Muted).LineHeight(1.4).Wrap()
			})
		})
		for _, h := range a.Hotkeys {
			row(h.Label, "", func() { a.keyField(c, p, kindGlobal, h.Value) })
		}

	})

	add("appearance", "外观", components.IconPalette, func(row Row, block Block) {

		background := func(key, label string, paths *[]string, opacity *float64) {
			block(label+" 背景图片 透明度", func() {
				ui.Column(c).Padding(12, 16).Gap(10).Children(func() {
					ui.Row(c).Gap(8).AlignItems(ui.Center).Children(func() {
						ui.Text(c, label).FontSize(13.5).FontWeight(500)
						ui.Spacer(c)
						ui.Text(c, "透明度").FontSize(12).TextColor(p.Muted)
						if ui.Slider(c.Key(key+"-opacity"), opacity, 0.05, 0.9).Width(150).Label("调整" + label + "透明度").Changed() {
							a.Changed()
						}
					})
					a.backdropRow(c, p, key, paths)
				})
			})
		}

		row("主题颜色", "按钮、选中项和进度条的颜色", func() {
			labels := make([]string, len(a.Accents))
			chosen := "跟随外观"
			for i, ac := range a.Accents {
				labels[i] = ac.Label
				if ac.Label == s.Accent {
					chosen = ac.Label
				}
			}
			ui.Row(c).Gap(8).AlignItems(ui.Center).Children(func() {
				if dot := a.AccentColor(s.Accent, p.Dark); dot != (ui.Color{}) {
					ui.Box(c).Size(14, 14).Radius(999).Background(dot)
				}
				if ui.Select(c.Key("accent"), &chosen, append([]string{"跟随外观"}, labels...)).Label("选择主题颜色").Width(120).Changed() {
					if chosen == "跟随外观" {
						s.Accent = ""
					} else {
						s.Accent = chosen
					}
					a.Changed()
				}
			})
		})
		font(row, "ui-font", "UI 字体", "应用界面的文字字体", &s.UIFont)
		font(row, "playing-lyric-font", "歌词字体", "播放页的歌词字体", &s.PlaybackLyricFont)
		background("home", "主页面背景", &s.BackdropHome, &s.BackdropHomeOpacity)
		background("playing", "播放页背景", &s.BackdropPlaying, &s.BackdropPlayingOpacity)
	})

	add("lyrics", "桌面歌词", components.IconDeskLyrics, func(row Row, block Block) {

		// So is a colour, beside a dot of it.
		colour := func(key, label, desc string, chosen *string) {
			row(label, desc, func() {
				names := make([]string, len(a.LyricColors))
				now := a.LyricColors[0].Label
				for i, lc := range a.LyricColors {
					names[i] = lc.Label
					if lc.Label == *chosen {
						now = lc.Label
					}
				}
				ui.Row(c).Gap(8).AlignItems(ui.Center).Children(func() {
					ui.Box(c).Size(12, 12).Radius(999).Border(1, p.Hairline).Background(a.LyricColor(*chosen, p.Text))
					if ui.Select(c.Key(key), &now, names).Label(label).Width(110).Changed() {
						*chosen = now
						a.Changed()
					}
				})
			})
		}

		row("显示桌面歌词", "歌词单独浮在其它窗口之上，可以拖到屏幕的任意位置", func() {
			if ui.Switch(c.Key("lyric-window"), &s.LyricWindow).Label("显示桌面歌词").Changed() {
				a.Changed()
			}
		})
		row("逐字歌词", "当前句按歌曲提供的逐字时间逐渐高亮", func() {
			if ui.Switch(c.Key("lyric-karaoke"), &s.LyricKaraoke).Label("逐字歌词").Changed() {
				a.Changed()
			}
		})
		row("行数", "窗口一次显示一句或本句和下句", func() {
			i := 1
			if s.LyricLines == 1 {
				i = 0
			}
			if components.Segmented(c.Key("lyric-lines"), &i, "单行", "双行") {
				s.LyricLines = i + 1
				a.Changed()
			}
		})
		alignment := func(key, label, desc string, value *int) {
			row(label, desc, func() {
				if components.Segmented(c.Key(key), value, "靠左", "居中", "靠右") {
					a.Changed()
				}
			})
		}
		if s.LyricLines == 1 {
			alignment("lyric-align", "歌词对齐", "原文与翻译使用相同的水平对齐方式", &s.LyricAlign)
		} else {
			alignment("lyric-align", "当前行对齐", "当前行及其翻译的水平对齐方式", &s.LyricAlign)
			alignment("lyric-next-align", "下一行对齐", "下一行及其翻译的水平对齐方式", &s.LyricNextAlign)
			row("行距", "当前行与下一行之间的距离", func() {
				gap := min(max(s.LyricLineGap, 0), 40)
				if ui.Slider(c.Key("lyric-line-gap"), &gap, 0, 40).Width(150).Label("调整双行歌词行距").Changed() {
					s.LyricLineGap = math.Round(gap)
					a.Changed()
				}
			})
		}
		row("字号", "歌词文字的大小", func() {
			v := min(max(s.LyricSize, components.LyricSizeMin), components.LyricSizeMax)
			slider := ui.Slider(c.Key("lyric-size"), &v, components.LyricSizeMin, components.LyricSizeMax).Width(150).Label("调整歌词字号")
			if slider.Changed() {
				s.LyricSize = math.Round(v)
				a.Changed()
			}
		})
		row("字重", "歌词文字的粗细", func() {
			weights := []int{300, 400, 600, 700, 800}
			names := []string{"细", "常规", "中黑", "粗", "特粗"}
			now := names[3]
			for i, w := range weights {
				if w == s.LyricWeight {
					now = names[i]
				}
			}
			if ui.Select(c.Key("lyric-weight"), &now, names).Label("选择字重").Width(110).Changed() {
				for i, name := range names {
					if name == now {
						s.LyricWeight = weights[i]
					}
				}
				a.Changed()
			}
		})
		font(row, "lyric-font", "字体", "歌词文字所用的字体", &s.LyricFont)
		colour("lyric-played", "已播放颜色", "正在播放的这一句的颜色", &s.LyricPlayed)
		colour("lyric-idle", "未播放颜色", "未播放的句子的颜色", &s.LyricIdle)
		row("描边厚度", "歌词文字描边的粗细（像素），0 为无描边", func() {
			v := min(max(s.LyricOutlineWidth, 0), 2.5)
			slider := ui.StepSlider(c.Key("lyric-outline-width"), &v, 0, 2.5, 0.1).Width(150).Label("调整描边厚度")
			if slider.Changed() {
				s.LyricOutlineWidth = v
				a.Changed()
			}
			ui.Text(c, fmt.Sprintf("%.1f", v)).FontSize(12).TextColor(p.Muted)
		})
		colour("lyric-outline-color", "描边颜色", "歌词文字描边的颜色", &s.LyricOutlineColor)
		row("翻译", "在歌词下方显示翻译", func() {
			if ui.Switch(c.Key("lyric-trans"), &s.LyricTrans).Label("显示翻译").Changed() {
				a.Changed()
			}
		})
		font(row, "lyric-trans-font", "翻译字体", "翻译文字所用的字体", &s.LyricTransFont)
		colour("lyric-trans-color", "翻译颜色", "翻译文字的颜色", &s.LyricTransColor)
		row("锁定位置", "锁定桌面歌词窗格位置", func() {
			if ui.Switch(c.Key("lyric-locked"), &s.LyricLocked).Label("锁定歌词位置").Changed() {
				a.Changed()
			}
		})

	})

	add("cache", "缓存", components.IconDatabase, func(row Row, block Block) {

		row("缓存目录", a.CacheDirectoryDescription(), func() {
			if ui.Button(c.Key("cache-directory"), "更改…").Label("更改缓存目录").Disabled(a.Cache.Moving).Clicked() {
				a.ChooseCacheDirectory()
			}
		})
		if s.CacheDir != "" {
			row("默认缓存目录", "恢复到系统默认位置，立即生效", func() {
				if ui.Button(c.Key("reset-cache-directory"), "恢复默认").Label("恢复默认缓存目录").Disabled(a.Cache.Moving).Clicked() {
					a.ConfirmCacheDirectory("")
				}
			})
		}
		row("歌曲缓存大小", "设置歌曲缓存的空间上限", func() {
			labels := make([]string, len(a.CacheLimits))
			chosen := a.CacheLimits[2].Label
			for i, l := range a.CacheLimits {
				labels[i] = l.Label
				if l.MB == s.CacheMB {
					chosen = l.Label
				}
			}
			if ui.Select(c.Key("cache"), &chosen, labels).Label("歌曲缓存大小").Width(150).Disabled(a.Cache.Moving).Changed() {
				for _, l := range a.CacheLimits {
					if l.Label == chosen {
						s.CacheMB = l.MB
					}
				}
				a.Changed()
				a.ApplyCacheLimit()
			}
		})
		row("歌曲缓存", "听过的歌曲可离线播放；"+a.CacheUsage(), func() {
			if ui.Button(c.Key("clear-cache"), "清除歌曲缓存").Disabled(a.Cache.Used <= 0 || a.Cache.Moving).Clicked() {
				a.ClearCache()
			}
		})

		row("浏览数据与歌词", fmt.Sprintf("已浏览页面和歌词可离线读取；上限 64 MiB，已用 %.1f MiB", float64(*a.BrowseUsed)/(1<<20)), func() {
			if ui.Button(c.Key("clear-browse-cache"), "清除浏览缓存").Disabled(a.Cache.Moving).Clicked() {
				a.ClearBrowseCache()
			}
		})
		row("图片缓存", fmt.Sprintf("所有页面共用封面缓存；上限 256 MiB，已用 %.1f MiB", float64(*a.ImagesUsed)/(1<<20)), func() {
			if ui.Button(c.Key("clear-image-cache"), "清除图片缓存").Disabled(a.Cache.Moving).Clicked() {
				a.ClearImageCache()
			}
		})
	})

	if a.CaptionsShown() {

		add("podcasts", "播客", components.IconPodcast, func(row Row, block Block) {
			a.CaptionSettings(row, c)
		})

	}

	add("experiments", "实验性功能", components.IconFlask, func(row Row, block Block) {

		row("进度条烟雾特效", "播放时进度条化作拖着烟雾的光点，随音乐起伏；会多用一些电量", func() {
			if ui.Switch(c.Key("smoke"), &s.Smoke).Label("进度条烟雾特效").Changed() {
				a.Changed()
			}
		})
		row("烟雾背景", "播放页的背景化作封面色调的烟雾，缓缓升起，随低音起伏；仍在打磨，会多用一些电量", func() {
			if ui.Switch(c.Key("fog"), &s.Fog).Label("烟雾背景").Changed() {
				a.Changed()
			}
		})
		row("沉浸式歌词", "播放时当前句随演唱整句或逐字点亮，上下边缘的歌词逐渐淡出", func() {
			if ui.Switch(c.Key("lyric-edge-fade"), &s.LyricEdgeFade).Label("沉浸式歌词").Changed() {
				a.Changed()
			}
		})
		row("播放页频谱", "在歌词和律动页底部显示随音乐起伏的频谱", func() {
			if ui.Switch(c.Key("lyric-spectrum"), &s.LyricSpectrum).Label("播放页频谱").Changed() {
				a.Changed()
			}
		})

	})

	add("account", "账号", components.IconUser, func(row Row, block Block) {

		if a.Account.Profile == nil {
			row("未登录", "登录后，每日推荐与歌单将根据你的口味生成", func() {
				if ui.PrimaryButton(c.Key("login"), "扫码登录").Clicked() {
					a.ShowLogin()
				}
			})
			return
		}
		block(a.Account.Profile.Nickname+" 已登录网易云音乐 退出登录", func() {

			prof := a.Account.Profile
			ui.Row(c).Padding(12, 16).Gap(12).AlignItems(ui.Center).Children(func() {
				a.Avatar(c, prof.AvatarURL, 36, p.Press)
				ui.Column(c).Grow(1).Gap(2).Children(func() {
					ui.Text(c, prof.Nickname).FontSize(13.5).FontWeight(600)
					ui.Text(c, "已登录网易云音乐").FontSize(12).TextColor(p.Muted)
				})
				if ui.Button(c.Key("logout"), "退出登录").Clicked() {
					a.SignOut()
				}
			})

		})

	})

	if a.Updates() {

		add("updates", "更新", components.IconReload, func(row Row, block Block) {

			desc := "每天检查一次是否有新版本"
			if t := a.LastUpdateCheck(); !t.IsZero() {
				desc += "；上次检查：" + a.CommentTime(t, time.Now())
			}
			row("自动检查更新", desc, func() {
				on := a.UpdateChecks()
				if ui.Switch(c.Key("update-checks"), &on).Label("自动检查更新").Changed() {
					a.SetUpdateChecks(on)
				}
			})
			row("自动下载并安装", "在后台下载新版本，下次打开时生效", func() {
				on := a.UpdateDownloads()
				if ui.Switch(c.Key("update-downloads"), &on).Label("自动下载并安装").Disabled(!a.UpdateChecks()).Changed() {
					a.SetUpdateDownloads(on)
				}
			})
			row("当前版本 "+mygo.App.Version(), "", func() {
				if ui.Button(c.Key("check-updates"), "检查更新").Clicked() {
					a.CheckForUpdates()
				}
			})

		})

	}
	return sections
}

// Playback and downloads share choices while persisting separate preferences.
func (a *View) qualitySelect(c *ui.Context, level *string, label string) bool {
	labels := make([]string, len(a.Qualities))
	chosen := a.Qualities[2].Label
	for i, q := range a.Qualities {
		labels[i] = q.Label
		if q.Value == *level {
			chosen = q.Label
		}
	}
	if !ui.Select(c, &chosen, labels).Label(label).Width(150).Changed() {
		return false
	}
	for _, q := range a.Qualities {
		if chosen == q.Label {
			*level = q.Value
			return true
		}
	}
	return false
}
