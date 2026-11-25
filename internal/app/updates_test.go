/*
验证关闭自动更新检查，以及从设置页手动检查更新。
*/

package app

import (
	"testing"
	"time"

	"github.com/egoist/mygo/ui"

	"netease-music-mygo/internal/netease"
)

// fakeUpdater swaps the update plugin's preferences for ones in memory,
// counting the checks asked for, until the returned function puts them
// back.
func fakeUpdater(checks, downloads *bool, checked *int) func() {
	check, getChecks, setChecks := checkForUpdates, updateChecks, setUpdateChecks
	getDownloads, setDownloads, last := updateDownloads, setUpdateDownloads, lastUpdateCheck
	checkForUpdates = func() { *checked++ }
	updateChecks = func() bool { return *checks }
	setUpdateChecks = func(on bool) { *checks = on }
	updateDownloads = func() bool { return *downloads }
	setUpdateDownloads = func(on bool) { *downloads = on }
	lastUpdateCheck = func() time.Time { return time.Time{} }
	return func() {
		checkForUpdates, updateChecks, setUpdateChecks = check, getChecks, setChecks
		updateDownloads, setUpdateDownloads, lastUpdateCheck = getDownloads, setDownloads, last
	}
}

// Turning off checking for updates turns it off in the updater, and
// installing updates without asking can't be turned on without it.
func TestTurnOffUpdateChecks(t *testing.T) {
	checks, downloads, checked := true, false, 0
	defer fakeUpdater(&checks, &downloads, &checked)()
	a := newTestApp()
	a.settings = defaultSettings()
	a.updates = true
	tt := ui.NewTester(a.views.settings.Build, 780, 640)
	showSettingsPage(t, tt, "更新")
	tt.Frame()
	// The row's title is labelled as its switch is: click the switch, at
	// the row's end.
	toggle := func(title, desc string) {
		t.Helper()
		top, ok1 := tt.Find(title)
		bottom, ok2 := tt.Find(desc)
		if !ok1 || !ok2 {
			t.Fatalf("no row %s", title)
		}
		tt.ClickAt(780-28-16-12, (top.Y+bottom.Y+bottom.H)/2)
		tt.Frame()
	}
	toggle("自动检查更新", "每天检查一次是否有新版本")
	if checks {
		t.Fatal("checks for updates still")
	}
	toggle("自动下载并安装", "在后台下载新版本，下次打开时生效")
	if downloads {
		t.Fatal("turned on installing updates without checking for them")
	}
	// It does turn on while the updater checks.
	checks = true
	tt.Frame()
	toggle("自动下载并安装", "在后台下载新版本，下次打开时生效")
	if !downloads {
		t.Fatal("didn't turn on installing updates")
	}
}

// 检查更新 in the settings checks for updates as the user asked.
func TestCheckForUpdatesFromSettings(t *testing.T) {
	checks, downloads, checked := true, false, 0
	defer fakeUpdater(&checks, &downloads, &checked)()
	a := newTestApp()
	a.settings = defaultSettings()
	a.updates = true
	a.account.Profile = &netease.Profile{UserID: 1}
	tt := ui.NewTester(a.views.settings.Build, 780, 640)
	showSettingsPage(t, tt, "更新")
	tt.Frame()
	if err := tt.Click("检查更新"); err != nil {
		t.Fatal(err)
	}
	if checked != 1 {
		t.Fatalf("checked %d times", checked)
	}
}
