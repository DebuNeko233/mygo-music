/*
呈现本地歌词匹配按钮与搜索对话框，将选择交给匹配命令。
*/

package local

import (
	"strings"

	"github.com/egoist/mygo/ui"

	"netease-music-mygo/internal/ui/view/components"
)

func (a *View) LyricMatchButton(c *ui.Context) {
	if a.Offline() {
		ui.Text(c, "联网后可匹配歌词").FontSize(12).TextColor(ui.RGB(255, 255, 255).Alpha(0.5))
		return
	}
	components.PillButton(c.Key("match-local-lyrics"), nil, "从网易云匹配歌词", ui.RGB(255, 255, 255).Alpha(.16), ui.RGB(255, 255, 255)).
		Disabled(a.Player().MetadataLoading()).OnClick(a.OpenLyricMatch)
}

func (a *View) LyricMatchDialog(c *ui.Context) {
	d := a.Model.LyricMatch
	if d == nil {
		return
	}
	if a.Player().Current() != d.Track {
		d.Open = false
	}
	p := *a.Palette
	ui.DialogBase(c.Key("local-lyric-match"), &d.Open, func(backdrop, panel ui.Element) {
		w, h := c.Size()
		backdrop.Background(ui.RGBA(0, 0, 0, .35))
		panel.Width(min(560, w-48)).Padding(22).Radius(18).Gap(12).
			Background(p.Background).Border(1, p.Hairline).Shadow(0, 24, 60, -12, ui.RGBA(0, 0, 0, .4))
		ui.Row(c).AlignItems(ui.Center).Gap(12).Children(func() {
			ui.Text(c, "从网易云匹配歌词").Grow(1).FontSize(17).FontWeight(700)
			components.IconButton(c.Key("close"), components.IconX, 18, p.Muted, "关闭").OnClick(func() { d.Open = false })
		})
		ui.Text(c, strings.Join([]string{d.Track.Name, components.Clock(d.Track.Length())}, " · ")).
			TextColor(p.Muted).FontSize(13).SingleLine()
		ui.Row(c).Gap(8).Children(func() {
			input := ui.TextInput(c.Key("query"), &d.Query).Grow(1).MinWidth(0).
				Label("歌名和歌手").Placeholder("歌名和歌手").Disabled(d.Loading).AutoFocus()
			if input.Submitted() {
				a.SearchLyrics(d)
			}
			ui.Button(c.Key("search"), "搜索").Disabled(d.Loading || strings.TrimSpace(d.Query) == "").
				OnClick(func() { a.SearchLyrics(d) })
		})
		if d.Searching {
			ui.Text(c, "正在搜索…").TextColor(p.Muted).FontSize(13)
		}
		if len(d.Results) != 0 {
			ui.Scroll(c.Key("versions")).TrackScroll(&a.LyricScroll).MaxHeight(max(100, min(322, h-245))).Gap(4).
				Children(func() {
					for _, track := range d.Results {
						details := make([]string, 0, 3)
						if names := track.ArtistNames(); names != "" {
							details = append(details, names)
						}
						if track.Album.Name != "" {
							details = append(details, track.Album.Name)
						}
						if track.Duration > 0 {
							details = append(details, components.Clock(track.Length()))
						}
						detail := strings.Join(details, " · ")
						row := ui.ButtonBase(c.Key(track.ID)).Label(strings.TrimSpace(track.Name+" "+detail)).
							Disabled(d.Loading).Padding(8, 10).Radius(7)
						if d.Selected == track.ID {
							row.Background(p.Accent.Alpha(.16))
						} else if row.Hovered() {
							row.Background(p.Hover)
						}
						row.Children(func() {
							ui.Column(c).Grow(1).MinWidth(0).Gap(3).Children(func() {
								ui.Text(c, track.Name).SingleLine().FontSize(14).FontWeight(600)
								if detail != "" {
									ui.Text(c, detail).SingleLine().FontSize(12).TextColor(p.Muted)
								}
							})
						})
						row.OnClick(func() { d.Selected = track.ID; d.Status = "" })
					}
				})
		}
		if d.Status != "" {
			ui.Text(c, d.Status).TextColor(p.Muted).FontSize(13)
		}
		ui.Row(c).Justify(ui.End).Children(func() {
			label := "使用此版本"
			if d.Loading {
				label = "正在加载歌词…"
			}
			ui.PrimaryButton(c.Key("confirm"), label).Disabled(d.Searching || d.Loading || d.Selected == 0).
				OnClick(func() { a.ConfirmLyrics(d) })
		})
	})
}
