package app

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
)

// openSettings shows the settings window, opening it the first time.
func (a *app) openSettings() {
	if a.settingsWin != nil {
		a.settingsWin.Show()
		a.settingsWin.Focus()
		a.measureCache()
		return
	}
	a.measureCache()
	a.settingsWin = mygo.NewWindow(mygo.WindowOptions{
		Title:                "设置",
		Width:                780,
		Height:               640,
		MinWidth:             640,
		MinHeight:            420,
		TitleBarStyle:        mygo.TitleBarHidden,
		TitleBarHeight:       buttonsHeight,
		TrafficLightPosition: &mygo.Point{X: 20, Y: 20},
		// Keep the old narrow window's saved dimensions out of this layout.
		StateKey:        "settings-pages",
		DisableMaximize: true,
		DisableMinimize: true,
		// The app's menu has nothing for this window: on Linux and
		// Windows it stays out of sight until Alt shows it.
		AutoHideMenuBar: true,
		BackgroundColor: "light-dark(#f4f4f6, #1c1c1f)",
		Content:         ui.View(a.settingsView),
	})
	a.settingsWin.OnClosed(func() {
		a.settingsWin = nil
		// The dialog asking about the folder goes with the window.
		a.cacheConfirm.open = false
		a.syncDock()
	})
	a.syncDock()
}

// backdropRow is the pictures of one page: those chosen, more added from
// the disk or dropped on the row, and all of them let go.
func (a *app) backdropRow(c *ui.Context, p palette, key string, paths *[]string) {
	ui.Column(c).Gap(8).Children(func() {
		line := ui.Row(c).Gap(8).AlignItems(ui.Center)
		// Anything dropped on the row is a picture of this page's.
		if dropped := line.DroppedFiles(); len(dropped) > 0 {
			a.addPictures(dropped, paths)
		}
		line.Children(func() {
			if ui.Button(c.Key(key+"-add"), "选择图片…").Clicked() {
				files, err := mygo.Dialog.Open(mygo.OpenDialogOptions{
					Title:    "选择背景图片",
					Filters:  []mygo.FileFilter{{Name: "图片", Extensions: pictureExts}},
					Multiple: true,
				})
				if err == nil {
					a.addPictures(files, paths)
				}
			}
			if ui.Button(c.Key(key+"-clear"), "清空").Disabled(len(*paths) == 0).Clicked() {
				*paths = nil
				a.changed()
			}
			ui.Text(c, pictureList(*paths)).FontSize(12).TextColor(p.muted)
		})
	})
}

// changed applies the settings everywhere and keeps them.
func (a *app) changed() {
	a.settings.save()
	a.settings.takeAccent()
	// The appearance is the windows' to take, and without one, as in
	// tests, there is nothing to take it.
	if a.win != nil {
		a.settings.applyAppearance()
	}
	if a.notch != nil {
		a.notch.setEnabled(a.settings.Notch)
	}
	if a.lyrics != nil {
		a.lyrics.apply()
	}
	a.redraw()
}

// settingsRow is a setting of the settings window: what it is, what it means,
// and its control. It shows if a search finds it.
type settingsRow func(label, desc string, control func())

// settingsBlock is anything else in the settings, which a search finds by text.
type settingsBlock func(text string, draw func())

// settingsSection is a page of the settings. Its build only says what it has
// through row and block, for a search to go through them without drawing.
type settingsSection struct {
	id, title string
	icon      *ui.SVG
	build     func(row settingsRow, block settingsBlock)
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

func (a *app) settingsView(c *ui.Context) {
	t := *c.Theme()
	p := paletteFor(&t)
	t.Accent, t.AccentText = p.accent, p.onAccent
	c.SetTheme(&t)
	c.Root().Background(p.bg)
	c.OnShortcut(ui.Cmd, ui.KeyW, func() {
		if a.settingsWin != nil {
			a.settingsWin.Close()
		}
	})
	c.OnShortcut(ui.Cmd, ui.KeyF, func() { a.settingsSearchBox.Focus(c) })

	sections := a.settingsSections(c, p)
	current := sections[0]
	for _, sec := range sections {
		if sec.id == a.settingsPage {
			current = sec
		}
	}
	query := strings.ToLower(strings.TrimSpace(a.settingsQuery))

	ui.Row(c).Fill().AlignItems(ui.Stretch).Children(func() {
		// Under the window buttons where they sit over the sidebar. Where nothing
		// takes the press, it moves the window, as the title bar it reaches into would.
		top := float32(14)
		if c.TitleBar().Left > 0 {
			top = topBar(c) + 4
		}
		ui.Column(c).Width(settingsSidebarWidth).Shrink(0).Padding(top, 12, 14, 12).Gap(2).Background(p.sidebar).DragWindow().Children(func() {
			a.settingsSearch(c, p)
			ui.Scroll(c.Key("settings-nav")).Grow(1).MinHeight(0).Gap(2).Children(func() {
				for _, sec := range sections {
					a.settingsNavItem(c, p, sec, query == "" && sec.id == current.id)
				}
			})
		})
		ui.Box(c).Width(1).Shrink(0).Background(p.hairline)
		// The page's title is in the header, the strip the window buttons leave. What
		// scrolls goes under a hard edge there, showing only once there's something under it.
		pad := max(24, topBar(c)+6)
		ui.Box(c).Grow(1).MinWidth(0).Children(func() {
			ui.Scroll(c.Key(current.id+"\x00"+query)).Fill().Padding(pad, 28, 32, 28).Gap(8).Children(func() {
				mark := ui.Box(c.Key("start-mark")).Height(0).Bounds()
				if mark.W > 0 {
					a.settingsUnderTop = mark.Y < pad-0.5
				}
				if query == "" {
					a.settingsGroup(c, p, current, "", "")
					return
				}
				found := false
				for _, sec := range sections {
					if a.settingsFinds(sec, query) {
						found = true
						a.settingsGroup(c, p, sec, sec.title, query)
					}
				}
				if !found {
					ui.Text(c, "未找到相关设置").FontSize(13.5).TextColor(p.muted).Margin(8, 4, 0, 4)
				}
			})
			edge := ui.Box(c.Key("top-edge")).Absolute().Top(0).Left(0).Right(0).Height(topBar(c)).DragWindow().
				Material(glass.ScrollEdge{Hard: true, Background: p.bg})
			title := current.title
			if query != "" {
				title = "搜索结果"
			}
			// Leave room for the window buttons on Linux and Windows.
			right := max(32, c.TitleBar().Right+12)
			header := ui.Row(c.Key("header")).Absolute().Top(0).Left(0).Right(0).Height(topBar(c)).Padding(0, right, 0, 32).AlignItems(ui.Center).DragWindow()
			header.Children(func() {
				ui.Text(c, title).FontSize(15).FontWeight(700).SingleLine().Shrink(1).MinWidth(0)
			})
			// The pointer over it brings the edge up too, as AppKit's does.
			edge.Opacity(edgeShown(edge, a.settingsUnderTop || header.Hovered()))
		})
	})
	a.cacheDirectoryDialog(c, p)
}

// settingsSidebarWidth is the width of the list of the settings' pages.
const settingsSidebarWidth = 200

// settingsSearch is the field that finds settings across all the pages.
func (a *app) settingsSearch(c *ui.Context, p palette) {
	field := ui.Row(c).Height(30).Radius(8).AlignItems(ui.Stretch).Margin(0, 0, 10, 0)
	if a.settingsSearchBox.Focused(c) {
		field.Background(p.bg).Shadow(0, 0, 0, 3, p.accent.Alpha(0.25)).Border(1, p.accent)
	} else {
		field.Background(p.hover).Border(1, p.hairline)
	}
	field.Children(func() {
		// The input fills the field, under its icon, so a click anywhere on
		// the field focuses it.
		ui.TextInputBase(c.Key("search"), &a.settingsQuery).Bind(&a.settingsSearchBox).Grow(1).MinWidth(0).PaddingStart(29).PaddingEnd(9).
			Placeholder("搜索").FontSize(13).Label("搜索设置")
		ui.Icon(c, iconSearch).Size(13, 13).TextColor(p.muted).Absolute().Left(9).Top(7.5).IgnoreInput(true)
	})
}

// settingsNavItem opens a page of the settings, leaving a search.
func (a *app) settingsNavItem(c *ui.Context, p palette, sec settingsSection, selected bool) {
	b := ui.ButtonBase(c.Key(sec.id)).Height(32).PaddingX(9).Gap(9).Radius(8).AlignItems(ui.Center).Justify(ui.Start).
		FocusRing(false).Label(sec.title)
	switch {
	case selected:
		b.Background(p.press)
	case b.Hovered():
		b.Background(p.hover)
	}
	color := p.muted
	if selected {
		color = p.accent
	}
	b.Children(func() {
		ui.Icon(c, sec.icon).Size(15, 15).TextColor(color)
		ui.Text(c, sec.title).FontSize(13).FontWeight(500).SingleLine().Shrink(1).MinWidth(0)
	})
	if b.Clicked() {
		a.settingsPage, a.settingsQuery = sec.id, ""
	}
}

// settingsFinds reports whether a search finds anything in a section: all of it,
// if it's what the section is called.
func (a *app) settingsFinds(sec settingsSection, query string) bool {
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
func (a *app) settingsGroup(c *ui.Context, p palette, sec settingsSection, title, query string) {
	if title != "" {
		ui.Text(c, title).FontSize(12).FontWeight(600).TextColor(p.muted).Margin(14, 4, 4, 4)
	}
	all := query == "" || settingsMatch(query, sec.title)
	ui.Column(c.Key(sec.id)).Radius(12).Background(p.bg).Border(1, p.hairline).Dividers(1, p.hairline).Children(func() {
		sec.build(func(label, desc string, control func()) {
			if !all && !settingsMatch(query, label, desc) {
				return
			}
			ui.Row(c).Padding(12, 16).Gap(16).AlignItems(ui.Center).MinHeight(48).Children(func() {
				ui.Column(c).Grow(1).Shrink(1).MinWidth(0).Gap(3).Children(func() {
					ui.Text(c, label).FontSize(13.5).FontWeight(500)
					if desc != "" {
						ui.Text(c, desc).FontSize(12).TextColor(p.muted).LineHeight(1.4)
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
func (a *app) settingsSections(c *ui.Context, p palette) []settingsSection {
	s := &a.settings
	var sections []settingsSection
	add := func(id, title string, icon *ui.SVG, build func(settingsRow, settingsBlock)) {
		sections = append(sections, settingsSection{id, title, icon, build})
	}

	add("general", "通用", iconSettings, func(row settingsRow, block settingsBlock) {
		row("外观", "", func() {
			modes := []string{"system", "light", "dark"}
			i := 0
			for k, m := range modes {
				if m == s.Appearance {
					i = k
				}
			}
			if segmented(c.Key("appearance"), &i, "跟随系统", "浅色", "深色") {
				s.Appearance = modes[i]
				a.changed()
			}
		})
		if runtime.GOOS == "darwin" {

			row("应用图标", "显示在菜单栏时，关闭窗口后可从菜单栏图标打开", func() {
				labels := make([]string, len(appIcons))
				chosen := appIcons[0].label
				for i, ic := range appIcons {
					labels[i] = ic.label
					if ic.id == s.AppIcon {
						chosen = ic.label
					}
				}
				if ui.Select(c.Key("app-icon"), &chosen, labels).Label("应用图标").Width(150).Changed() {
					for _, ic := range appIcons {
						if ic.label == chosen {
							s.AppIcon = ic.id
						}
					}
					a.changed()
					a.changeAppIcon()
				}
			})

		}
	})

	if a.notch != nil {

		add("notch", "刘海播放器", iconLaptop, func(row settingsRow, block settingsBlock) {

			row("显示刘海播放器", "在屏幕顶部的刘海处显示迷你播放器，鼠标悬停即可展开", func() {
				if ui.Switch(c.Key("notch"), &s.Notch).Label("显示刘海播放器").Changed() {
					a.changed()
				}
			})
			row("播放时显示实时活动", "在刘海两侧显示封面与音频波形，可能遮挡刘海旁的菜单栏图标", func() {
				if ui.Switch(c.Key("activity"), &s.NotchActivity).Label("播放时显示实时活动").Disabled(!s.Notch).Changed() {
					a.changed()
				}
			})
			row("在无刘海的屏幕上常驻显示", "关闭时，将鼠标移至屏幕顶部中央即可唤出，不会遮挡菜单栏", func() {
				if ui.Switch(c.Key("virtual"), &s.NotchVirtual).Label("在无刘海的屏幕上常驻显示").Disabled(!s.Notch).Changed() {
					a.changed()
				}
			})

		})

	}

	add("playback", "播放", iconVolume, func(row settingsRow, block settingsBlock) {

		row("音质", "无损与 Hi-Res 需要黑胶 VIP，没有时播放所能播放的最高音质；下一首歌曲开始生效", func() {
			labels := make([]string, len(qualityLevels))
			chosen := qualityLevels[2].label
			for i, q := range qualityLevels {
				labels[i] = q.label
				if q.level == s.Quality {
					chosen = q.label
				}
			}
			if ui.Select(c.Key("quality"), &chosen, labels).Label("音质").Width(150).Changed() {
				for _, q := range qualityLevels {
					if q.label == chosen {
						s.Quality = q.level
					}
				}
				a.changed()
			}
		})
		row("间奏时显示热评", "前奏、间奏与尾奏时，歌词处换成这首歌的热门评论", func() {
			if ui.Switch(c.Key("hot-comments"), &s.HotComments).Label("间奏时显示热评").Changed() {
				a.changed()
			}
		})
		row("音量均衡", "按网易云音乐测得的响度调整每首歌的音量，让歌曲听起来一样响", func() {
			if ui.Switch(c.Key("normalize"), &s.Normalize).Label("音量均衡").Changed() {
				a.pl.SetVolume(a.volume)
				a.changed()
			}
		})

	})

	add("appearance", "外观", iconPalette, func(row settingsRow, block settingsBlock) {

		// A row whose control needs the width under its label.
		wide := func(label, desc string, control func()) {
			block(label+" "+desc, func() {

				ui.Column(c).Padding(12, 16).Gap(10).Children(func() {
					ui.Column(c).Gap(3).Children(func() {
						ui.Text(c, label).FontSize(13.5).FontWeight(500)
						if desc != "" {
							ui.Text(c, desc).FontSize(12).TextColor(p.muted).LineHeight(1.4)
						}
					})
					control()
				})

			})
		}

		row("主题颜色", "按钮、选中项和进度条的颜色", func() {
			labels := make([]string, len(accents))
			chosen := "跟随外观"
			for i, ac := range accents {
				labels[i] = ac.name
				if ac.name == s.Accent {
					chosen = ac.name
				}
			}
			ui.Row(c).Gap(8).AlignItems(ui.Center).Children(func() {
				if dot := accentColor(s.Accent, p.dark); dot != (ui.Color{}) {
					ui.Box(c).Size(14, 14).Radius(999).Background(dot)
				}
				if ui.Select(c.Key("accent"), &chosen, append([]string{"跟随外观"}, labels...)).Label("选择主题颜色").Width(120).Changed() {
					if chosen == "跟随外观" {
						s.Accent = ""
					} else {
						s.Accent = chosen
					}
					a.changed()
				}
			})
		})
		wide("主页面背景", "主页的底色换成一张或多张图片，切歌时切换一张背景图", func() {
			a.backdropRow(c, p, "home", &s.BackdropHome)
		})
		wide("播放页背景", "播放页的最后一层底色换成一张或多张图片，切歌时切换一张背景图", func() {
			a.backdropRow(c, p, "playing", &s.BackdropPlaying)
		})
		row("背景透明度", "低透明度对比度更好", func() {
			v := s.BackdropOpacity
			// A Slider, never a SliderBase: the base draws nothing of
			// its own, and a settings page has no look to give it.
			slider := ui.Slider(c.Key("backdrop-opacity"), &v, 0.05, 0.9).Width(150).Label("调整背景透明度")
			if slider.Changed() {
				s.BackdropOpacity = v
				a.changed()
			}
		})

	})

	add("lyrics", "桌面歌词", iconDeskLyrics, func(row settingsRow, block settingsBlock) {

		// A font is one of the system's, named as the system names it,
		// with the system's own for the first: typed into a field that
		// narrows the list as the letters come, as a system may have
		// hundreds of them.
		font := func(key, label, desc string, chosen *string) {
			row(label, desc, func() {
				names := lyricFontNames()
				now := lyricFontSystem
				if *chosen != "" {
					now = *chosen
				}
				// A choice an older settings file holds is none of the
				// system's names: it keeps the place beside the system's
				// own, showing as it did until it is replaced.
				if !slices.Contains(names, now) {
					names = append([]string{names[0], now}, names[1:]...)
				}
				if ui.Combobox(c.Key(key), &now, names).Label("选择" + label).Width(150).Changed() {
					*chosen = ""
					if now != lyricFontSystem {
						*chosen = now
					}
					a.changed()
				}
			})
		}
		// So is a colour, beside a dot of it.
		colour := func(key, label, desc string, chosen *string) {
			row(label, desc, func() {
				names := make([]string, len(lyricColors))
				now := lyricColors[0].name
				for i, lc := range lyricColors {
					names[i] = lc.name
					if lc.name == *chosen {
						now = lc.name
					}
				}
				ui.Row(c).Gap(8).AlignItems(ui.Center).Children(func() {
					ui.Box(c).Size(12, 12).Radius(999).Border(1, p.hairline).Background(lyricColor(*chosen, p.text))
					if ui.Select(c.Key(key), &now, names).Label(label).Width(110).Changed() {
						*chosen = now
						a.changed()
					}
				})
			})
		}

		row("显示桌面歌词", "歌词单独浮在其它窗口之上，可以拖到屏幕的任意位置", func() {
			if ui.Switch(c.Key("lyric-window"), &s.LyricWindow).Label("显示桌面歌词").Changed() {
				a.changed()
			}
		})
		row("行数", "窗口一次显示一句或本句和下句", func() {
			i := 1
			if s.LyricLines == 1 {
				i = 0
			}
			if segmented(c.Key("lyric-lines"), &i, "单行", "双行") {
				s.LyricLines = i + 1
				a.changed()
			}
		})
		row("字号", "歌词文字的大小", func() {
			v := min(max(s.LyricSize, lyricSizeMin), lyricSizeMax)
			slider := ui.Slider(c.Key("lyric-size"), &v, lyricSizeMin, lyricSizeMax).Width(150).Label("调整歌词字号")
			if slider.Changed() {
				s.LyricSize = math.Round(v)
				a.changed()
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
				a.changed()
			}
		})
		font("lyric-font", "字体", "歌词文字所用的字体", &s.LyricFont)
		colour("lyric-played", "已播放颜色", "正在播放的这一句的颜色", &s.LyricPlayed)
		colour("lyric-idle", "未播放颜色", "未播放的句子的颜色", &s.LyricIdle)
		row("描边厚度", "歌词文字描边的粗细（像素），0 为无描边", func() {
			v := min(max(s.LyricOutlineWidth, 0), 2.5)
			slider := ui.StepSlider(c.Key("lyric-outline-width"), &v, 0, 2.5, 0.1).Width(150).Label("调整描边厚度")
			if slider.Changed() {
				s.LyricOutlineWidth = v
				a.changed()
			}
			ui.Text(c, fmt.Sprintf("%.1f", v)).FontSize(12).TextColor(p.muted)
		})
		colour("lyric-outline-color", "描边颜色", "歌词文字描边的颜色", &s.LyricOutlineColor)
		row("翻译", "在歌词下方显示翻译", func() {
			if ui.Switch(c.Key("lyric-trans"), &s.LyricTrans).Label("显示翻译").Changed() {
				a.changed()
			}
		})
		font("lyric-trans-font", "翻译字体", "翻译文字所用的字体", &s.LyricTransFont)
		colour("lyric-trans-color", "翻译颜色", "翻译文字的颜色", &s.LyricTransColor)
		row("锁定位置", "锁定桌面歌词窗格位置", func() {
			if ui.Switch(c.Key("lyric-locked"), &s.LyricLocked).Label("锁定歌词位置").Changed() {
				a.changed()
			}
		})

	})

	add("cache", "缓存", iconDatabase, func(row settingsRow, block settingsBlock) {

		row("歌曲缓存目录", a.cacheDirectoryDescription(), func() {
			if ui.Button(c.Key("cache-directory"), "更改…").Label("更改歌曲缓存目录").Disabled(a.cacheMoving).Clicked() {
				a.chooseCacheDirectory()
			}
		})
		if s.CacheDir != "" {
			row("默认缓存目录", "恢复到系统默认位置，立即生效", func() {
				if ui.Button(c.Key("reset-cache-directory"), "恢复默认").Label("恢复默认歌曲缓存目录").Disabled(a.cacheMoving).Clicked() {
					a.confirmCacheDirectory("")
				}
			})
		}
		row("歌曲缓存", "听过的歌曲保存在本机，再次播放无需等待，也不耗流量；"+a.cacheUsage(), func() {
			labels := make([]string, len(cacheLimits))
			chosen := cacheLimits[2].label
			for i, l := range cacheLimits {
				labels[i] = l.label
				if l.mb == s.CacheMB {
					chosen = l.label
				}
			}
			if ui.Select(c.Key("cache"), &chosen, labels).Label("歌曲缓存").Width(150).Disabled(a.cacheMoving).Changed() {
				for _, l := range cacheLimits {
					if l.label == chosen {
						s.CacheMB = l.mb
					}
				}
				a.changed()
				a.applyCacheLimit()
			}
		})
		row("清除缓存", "删除保存在本机的全部歌曲", func() {
			if ui.Button(c.Key("clear-cache"), "清除").Disabled(a.cacheUsed <= 0 || a.cacheMoving).Clicked() {
				a.clearCache()
			}
		})

	})

	if a.captionsShown() {

		add("podcasts", "播客", iconPodcast, func(row settingsRow, block settingsBlock) {
			a.captionSettings(row, c)
		})

	}

	add("experiments", "实验性功能", iconFlask, func(row settingsRow, block settingsBlock) {

		row("进度条烟雾特效", "播放时进度条化作拖着烟雾的光点，随音乐起伏；会多用一些电量", func() {
			if ui.Switch(c.Key("smoke"), &s.Smoke).Label("进度条烟雾特效").Changed() {
				a.changed()
			}
		})
		row("烟雾背景", "播放页的背景化作封面色调的烟雾，缓缓升起，随低音起伏；仍在打磨，会多用一些电量", func() {
			if ui.Switch(c.Key("fog"), &s.Fog).Label("烟雾背景").Changed() {
				a.changed()
			}
		})

	})

	add("account", "账号", iconUser, func(row settingsRow, block settingsBlock) {

		if a.profile == nil {
			row("未登录", "登录后，每日推荐与歌单将根据你的口味生成", func() {
				if ui.PrimaryButton(c.Key("login"), "扫码登录").Clicked() {
					a.showMain()
					a.openLogin()
					a.win.Invalidate()
				}
			})
			return
		}
		block(a.profile.Nickname+" 已登录网易云音乐 退出登录", func() {

			prof := a.profile
			ui.Row(c).Padding(12, 16).Gap(12).AlignItems(ui.Center).Children(func() {
				a.avatar(c, prof.AvatarURL, 36, p.press)
				ui.Column(c).Grow(1).Gap(2).Children(func() {
					ui.Text(c, prof.Nickname).FontSize(13.5).FontWeight(600)
					ui.Text(c, "已登录网易云音乐").FontSize(12).TextColor(p.muted)
				})
				if ui.Button(c.Key("logout"), "退出登录").Clicked() {
					a.signOut()
				}
			})

		})

	})

	if a.updates {

		add("updates", "更新", iconReload, func(row settingsRow, block settingsBlock) {

			desc := "每天检查一次是否有新版本"
			if t := lastUpdateCheck(); !t.IsZero() {
				desc += "；上次检查：" + commentTime(t, time.Now())
			}
			row("自动检查更新", desc, func() {
				on := updateChecks()
				if ui.Switch(c.Key("update-checks"), &on).Label("自动检查更新").Changed() {
					setUpdateChecks(on)
				}
			})
			row("自动下载并安装", "在后台下载新版本，下次打开时生效", func() {
				on := updateDownloads()
				if ui.Switch(c.Key("update-downloads"), &on).Label("自动下载并安装").Disabled(!updateChecks()).Changed() {
					setUpdateDownloads(on)
				}
			})
			row("当前版本 "+mygo.App.Version(), "", func() {
				if ui.Button(c.Key("check-updates"), "检查更新").Clicked() {
					checkForUpdates()
				}
			})

		})

	}
	return sections
}
