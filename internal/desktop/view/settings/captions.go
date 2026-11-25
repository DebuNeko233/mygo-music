/*
呈现字幕设置与模型卡片，提供模型下载、删除及大小显示。
*/

package settings

import (
	"fmt"

	"github.com/egoist/mygo/ui"

	"netease-music-mygo/internal/desktop/view/components"
	"netease-music-mygo/internal/speech"
)

// ModelCard offers to download the model that writes captions, and shows
// how far its download is.
func (a *View) ModelCard(c *ui.Context) {
	m := &a.Captions.Model
	ui.Column(c).Grow(1).Justify(ui.Center).Gap(14).Padding(0, 12, 48, 12).MaxWidth(440).Children(func() {
		ui.Text(c, "为播客生成字幕").FontSize(24).FontWeight(800).LetterSpacing(-0.3)
		ui.Text(c, fmt.Sprintf("下载语音识别模型（约 %s）后，节目会在本机边播边转写成字幕，像歌词一样随播放滚动，点一句即可跳到那里。音频不会上传。", Megabytes(speech.ModelSize()))).
			FontSize(14).LineHeight(1.6).TextColor(ui.RGB(255, 255, 255).Alpha(0.6))
		if m.Downloading {
			share := float32(0)
			if m.Total > 0 {
				share = float32(m.Done) / float32(m.Total)
			}
			ui.Box(c).Height(6).Radius(3).Background(ui.RGB(255, 255, 255).Alpha(0.14)).Margin(6, 0, 0, 0).Children(func() {
				ui.Box(c).Height(6).Radius(3).WidthPercent(share * 100).Background(ui.RGB(255, 255, 255).Alpha(0.85))
			})
			ui.Row(c).AlignItems(ui.Center).Children(func() {
				ui.Text(c, fmt.Sprintf("正在下载 %s / %s", Megabytes(m.Done), Megabytes(m.Total))).FontSize(12.5).FontFeatures("tnum").TextColor(ui.RGB(255, 255, 255).Alpha(0.5))
				ui.Spacer(c)
				if components.PillButton(c.Key("cancel"), nil, "取消", ui.RGB(255, 255, 255).Alpha(0.14), ui.RGB(255, 255, 255)).Clicked() {
					a.CancelModelDownload()
				}
			})
			return
		}
		if m.Error != "" {
			ui.Text(c, m.Error).FontSize(13).TextColor(ui.Hex("#ff8a80"))
		}
		label := "下载模型"
		if m.Error != "" {
			label = "重试"
		}
		ui.Row(c).Margin(6, 0, 0, 0).Children(func() {
			if components.PillButton(c.Key("download"), nil, label, ui.RGB(255, 255, 255), ui.Hex("#141416")).Clicked() {
				a.StartModelDownload()
			}
		})
	})
}

// Megabytes says n bytes in megabytes, or gigabytes once there are many.
func Megabytes(n int64) string {
	if n >= 1<<30 {
		return fmt.Sprintf("%.2f GB", float64(n)/(1<<30))
	}
	return fmt.Sprintf("%d MB", n>>20)
}

// CaptionSettings are the settings' rows for captions.
func (a *View) CaptionSettings(row func(label, desc string, control func()), c *ui.Context) {
	m := &a.Captions.Model
	s := a.Config
	row("播客字幕", "播放节目时在本机转写语音，生成可滚动的字幕；转写时会多用一些电量", func() {
		ready := a.ModelReady()
		// Show the effective state without discarding the saved preference when the model is absent.
		on := ready && s.Captions
		ui.Switch(c.Key("captions"), &on).Label("播客字幕").Disabled(!ready).OnClick(func() {
			s.Captions = on
			a.Changed()
			a.SyncCaptions()
		})
	})
	var desc string
	switch {
	case m.Downloading:
		desc = fmt.Sprintf("正在下载 %s / %s", Megabytes(m.Done), Megabytes(m.Total))
	case a.ModelReady():
		desc = "Qwen3-ASR，已下载，占用 " + Megabytes(speech.ModelSize())
	default:
		desc = "Qwen3-ASR，约 " + Megabytes(speech.ModelSize()) + "，下载后才能生成字幕"
	}
	row("语音识别模型", desc, func() {
		switch {
		case m.Downloading:
			if ui.Button(c.Key("model"), "取消").Clicked() {
				a.CancelModelDownload()
			}
		case a.ModelReady():
			if ui.Button(c.Key("model"), "删除").Clicked() {
				a.DeleteModel()
			}
		default:
			if ui.Button(c.Key("model"), "下载").Label("下载字幕模型").Clicked() {
				a.StartModelDownload()
			}
		}
	})
}
