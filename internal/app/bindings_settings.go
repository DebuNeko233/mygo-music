/*
为设置视图连接配置、缓存迁移、下载和字幕模型操作。
*/

package app

import (
	"time"

	"github.com/egoist/mygo/ui"

	"netease-music-mygo/internal/features/preferences"
	artcache "netease-music-mygo/internal/ui/view/artwork"
	"netease-music-mygo/internal/ui/view/components"
	playbackview "netease-music-mygo/internal/ui/view/playback"
	settingsview "netease-music-mygo/internal/ui/view/settings"
	"netease-music-mygo/internal/ui/view/shortcuts"
)

func (a *app) bindSettingsViews() {
	p := &a.views.page
	a.views.settings = settingsview.View{Page: p, State: &a.settingsViewState, Config: (*preferences.Settings)(&a.settings), Preferences: &a.preferences, Cache: &a.cacheSettings, Account: &a.account, Links: &a.links, Captions: &a.captions, Downloads: &a.downloads, BrowseUsed: &a.browseUsed, ImagesUsed: &a.imagesUsed, PictureExtensions: artcache.PictureExtensions, SystemFontLabel: playbackview.LyricFontSystem,
		Theme: func(t *ui.Theme) ui.Theme { return a.settings.uiTheme(t) }, Colors: paletteFor, Close: func() {
			if a.settingsWin != nil {
				a.settingsWin.Close()
			}
		}, Changed: a.changed, Save: func() { a.settings.saveSoon() }, RegisterLink: a.registerMusicLinkHandler, ChangeAppIcon: a.changeAppIcon, NormalizeVolume: func() { a.pl.SetVolume(a.playbackState.Volume) }, ShowLogin: func() {
			a.showMain()
			a.openLogin()
			if a.win != nil {
				a.win.Invalidate()
			}
		}, SignOut: a.signOut,
		AddPictures: a.addPictures, ForgetPictures: a.forgetPictures, FontNames: playbackview.LyricFontNames, AccentColor: components.AccentColor, LyricColor: playbackview.LyricColor, HotkeyLabel: shortcuts.Label, HotkeyError: func(name string) string { return a.hotkeyErrs[name] },
		Binding: func(kind, name string) string { return shortcuts.Binding(a.settings.Shortcuts, kind, name) }, ChangedBinding: func(kind, name string) bool { return shortcuts.Changed(a.settings.Shortcuts, kind, name) }, RestoreBinding: func(kind, name string) { a.restoreBinding(kind, name) }, SetBinding: func(kind, name, acc string) { a.setBinding(kind, name, acc) }, Taken: func(kind, name, acc string) (string, bool) {
			return shortcuts.Taken(a.settings.Shortcuts, kind, name, acc)
		}, AcceleratorFor: shortcuts.Format, ValidGlobal: shortcuts.ValidGlobal,
		HasNotch: func() bool { return a.notch != nil }, CaptionsShown: a.captionsShown, Updates: func() bool { return a.updates }, UpdateChecks: func() bool { return updateChecks() }, UpdateDownloads: func() bool { return updateDownloads() }, ModelReady: a.modelReady, LastUpdateCheck: func() time.Time { return lastUpdateCheck() }, SetUpdateChecks: func(on bool) { setUpdateChecks(on) }, SetUpdateDownloads: func(on bool) { setUpdateDownloads(on) }, CheckForUpdates: func() { checkForUpdates() },
		ChooseCacheDirectory: a.chooseCacheDirectory, ApplyCacheLimit: a.applyCacheLimit, ClearCache: a.clearCache, ClearBrowseCache: a.clearBrowseCache, ClearImageCache: a.clearImageCache, CacheDirectoryDescription: a.cacheDirectoryDescription, DownloadDirectory: a.downloadDirectory, ConfirmCacheDirectory: a.confirmCacheDirectory, SubmitCacheDirectory: a.submitCacheDirectory, CancelCacheChange: a.cancelCacheChange, ChooseDownloadDirectory: a.chooseDownloadDirectory, RevealDownload: func(path string) { revealDownload(path) }, CancelDownload: a.downloads.Cancel, RetryDownload: a.retryDownload, SyncCaptions: a.syncCaptions, StartModelDownload: a.startModelDownload, CancelModelDownload: a.captions.CancelModelDownload, DeleteModel: a.deleteModel, CommentTime: components.CommentTime, Avatar: a.views.artwork.Avatar,
	}
	for _, o := range appIcons {
		a.views.settings.AppIcons = append(a.views.settings.AppIcons, settingsview.Choice{Value: o.id, Label: o.label})
	}
	for _, o := range shortcuts.Actions() {
		a.views.settings.Hotkeys = append(a.views.settings.Hotkeys, settingsview.Choice{Value: o.Name, Label: o.Label})
	}
	for _, o := range components.Accents {
		a.views.settings.Accents = append(a.views.settings.Accents, settingsview.Choice{Value: o.Name, Label: o.Name})
	}
	for _, o := range playbackview.LyricColors {
		a.views.settings.LyricColors = append(a.views.settings.LyricColors, settingsview.Choice{Value: o.Name, Label: o.Name})
	}
}
