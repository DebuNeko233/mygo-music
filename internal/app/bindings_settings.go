package app

import (
	"github.com/egoist/mygo/ui"
	playbackview "netease-music-mygo/internal/desktop/view/playback"
	settingsview "netease-music-mygo/internal/desktop/view/settings"
	"netease-music-mygo/internal/preferences"
	"time"
)

func (a *app) bindSettingsViews() {
	p := &a.views.page
	a.views.settings = settingsview.View{Page: p, State: &a.settingsViewState, Config: (*preferences.Settings)(&a.settings), Preferences: &a.preferences, Cache: &a.cacheSettings, Account: &a.account, Links: &a.links, Captions: &a.captions, Downloads: &a.downloads, BrowseUsed: &a.browseUsed, ImagesUsed: &a.imagesUsed, PictureExtensions: pictureExts, SystemFontLabel: playbackview.LyricFontSystem,
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
		AddPictures: a.addPictures, ForgetPictures: a.forgetPictures, PictureList: pictureList, FontNames: playbackview.LyricFontNames, AccentColor: accentColor, LyricColor: playbackview.LyricColor, HotkeyLabel: hotkeyLabel, HotkeyError: func(name string) string { return a.hotkeyErrs[name] },
		Binding: func(kind, name string) string { return a.binding(keyKind(kind), name) }, ChangedBinding: func(kind, name string) bool { return a.changedBinding(keyKind(kind), name) }, RestoreBinding: func(kind, name string) { a.restoreBinding(keyKind(kind), name) }, SetBinding: func(kind, name, acc string) { a.setBinding(keyKind(kind), name, acc) }, Taken: func(kind, name, acc string) (string, bool) { return a.taken(keyKind(kind), name, acc) }, AcceleratorFor: acceleratorFor, ValidGlobal: validGlobal,
		HasNotch: func() bool { return a.notch != nil }, CaptionsShown: a.captionsShown, Updates: func() bool { return a.updates }, UpdateChecks: func() bool { return updateChecks() }, UpdateDownloads: func() bool { return updateDownloads() }, ModelReady: a.modelReady, LastUpdateCheck: func() time.Time { return lastUpdateCheck() }, SetUpdateChecks: func(on bool) { setUpdateChecks(on) }, SetUpdateDownloads: func(on bool) { setUpdateDownloads(on) }, CheckForUpdates: func() { checkForUpdates() },
		ChooseCacheDirectory: a.chooseCacheDirectory, ApplyCacheLimit: a.applyCacheLimit, ClearCache: a.clearCache, ClearBrowseCache: a.clearBrowseCache, ClearImageCache: a.clearImageCache, CacheUsage: a.cacheUsage, CacheDirectoryDescription: a.cacheDirectoryDescription, DownloadDirectory: a.downloadDirectory, CacheMoveLabel: a.cacheMoveLabel, ConfirmCacheDirectory: a.confirmCacheDirectory, SubmitCacheDirectory: a.submitCacheDirectory, CancelCacheChange: a.cancelCacheChange, ChooseDownloadDirectory: a.chooseDownloadDirectory, RevealDownload: func(path string) { revealDownload(path) }, CancelDownload: a.cancelDownload, RetryDownload: a.retryDownload, SyncCaptions: a.syncCaptions, StartModelDownload: a.startModelDownload, CancelModelDownload: a.cancelModelDownload, DeleteModel: a.deleteModel, CommentTime: commentTime, Avatar: a.views.artwork.Avatar,
	}
	for _, o := range appIcons {
		a.views.settings.AppIcons = append(a.views.settings.AppIcons, settingsview.Choice{Value: o.id, Label: o.label})
	}
	for _, o := range qualityLevels {
		a.views.settings.Qualities = append(a.views.settings.Qualities, settingsview.Choice{Value: o.level, Label: o.label})
	}
	for _, o := range hotkeys {
		a.views.settings.Hotkeys = append(a.views.settings.Hotkeys, settingsview.Choice{Value: o.name, Label: o.label})
	}
	for _, o := range accents {
		a.views.settings.Accents = append(a.views.settings.Accents, settingsview.Choice{Value: o.name, Label: o.name})
	}
	for _, o := range playbackview.LyricColors {
		a.views.settings.LyricColors = append(a.views.settings.LyricColors, settingsview.Choice{Value: o.Name, Label: o.Name})
	}
	for _, o := range cacheLimits {
		a.views.settings.CacheLimits = append(a.views.settings.CacheLimits, settingsview.CacheLimit{MB: o.mb, Label: o.label})
	}
	for _, o := range downloadNameFormats {
		a.views.settings.NameFormats = append(a.views.settings.NameFormats, settingsview.Choice{Value: o.value, Label: o.label})
	}
}
