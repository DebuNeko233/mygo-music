package app

import (
	"context"
	"errors"
	"path/filepath"
	"testing"

	"github.com/egoist/mygo/ui"
)

func captionSettingsApp(t *testing.T) (*app, *ui.Tester, chan func()) {
	t.Helper()
	isolateUserData(t)
	old := captionsAvailable
	captionsAvailable = func() bool { return true }
	t.Cleanup(func() { captionsAvailable = old })
	a := newTestApp()
	a.settings = defaultSettings()
	a.settings.path = filepath.Join(t.TempDir(), "settings.json")
	a.captions.model.checked = true
	posts := testPosts(a)
	tt := ui.NewTester(a.settingsView, 780, 640)
	showSettingsPage(t, tt, "播客")
	return a, tt, posts
}

func checkCaptionSwitch(t *testing.T, tt *ui.Tester, checked, disabled bool) {
	t.Helper()
	sw, ok := tt.AccessByRole(ui.RoleSwitch, "播客字幕")
	if !ok || sw.Checked != checked || sw.Disabled != disabled {
		t.Fatalf("caption switch found %v, checked %v, disabled %v; want %v, %v", ok, sw.Checked, sw.Disabled, checked, disabled)
	}
}

// Missing models show captions off and disabled while keeping the listener's preference for an installed model.
func TestCaptionSettingsRequireModel(t *testing.T) {
	a, tt, _ := captionSettingsApp(t)
	for _, preference := range []bool{true, false} {
		a.settings.Captions = preference
		tt.Frame()
		checkCaptionSwitch(t, tt, false, true)
		clickSettingsSwitch(t, tt, "播客字幕")
		if a.settings.Captions != preference {
			t.Fatal("a disabled switch changed the saved preference")
		}
		a.captions.model.installed = true
		tt.Frame()
		checkCaptionSwitch(t, tt, preference, false)
		clickSettingsSwitch(t, tt, "播客字幕")
		checkCaptionSwitch(t, tt, !preference, false)
		if a.settings.Captions != !preference || readCacheSettings(t, a.settings.path).Captions != !preference {
			t.Fatal("toggling captions with a model did not apply and save")
		}
		a.captions.model.installed = false
		tt.Frame()
		checkCaptionSwitch(t, tt, false, true)
	}
}

// Downloading, failed and cancelled models keep captions off; a completed download enables them and deletion disables them immediately.
func TestCaptionSettingsModelLifecycle(t *testing.T) {
	for _, outcome := range []string{"failed", "cancelled", "installed"} {
		t.Run(outcome, func(t *testing.T) {
			a, tt, posts := captionSettingsApp(t)
			oldDownload, oldRemove := downloadModel, removeModel
			defer func() { downloadModel, removeModel = oldDownload, oldRemove }()
			result := make(chan error, 1)
			downloadModel = func(ctx context.Context, _ string, _ func(int64, int64)) error {
				select {
				case err := <-result:
					return err
				case <-ctx.Done():
					return ctx.Err()
				}
			}
			removeModel = func(string) error { return nil }
			if err := tt.Click("下载"); err != nil {
				t.Fatal(err)
			}
			tt.Frame()
			checkCaptionSwitch(t, tt, false, true)
			switch outcome {
			case "failed":
				result <- errors.New("offline")
			case "cancelled":
				if err := tt.Click("取消"); err != nil {
					t.Fatal(err)
				}
			default:
				result <- nil
			}
			refreshPost(t, posts)
			tt.Frame()
			if outcome != "installed" {
				checkCaptionSwitch(t, tt, false, true)
				return
			}
			checkCaptionSwitch(t, tt, true, false)
			if !readCacheSettings(t, a.settings.path).Captions {
				t.Fatal("a completed download did not save enabled captions")
			}
			if err := tt.Click("删除"); err != nil {
				t.Fatal(err)
			}
			tt.Frame()
			checkCaptionSwitch(t, tt, false, true)
			// Isolate the asynchronous removal from a subsequent disk check.
			oldInstalled := modelInstalled
			modelInstalled = func(string) bool { return false }
			defer func() { modelInstalled = oldInstalled }()
			refreshPost(t, posts)
			tt.Frame()
			checkCaptionSwitch(t, tt, false, true)
		})
	}
}
