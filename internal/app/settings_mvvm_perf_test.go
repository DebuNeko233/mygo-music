package app

import (
	"testing"

	"github.com/egoist/mygo/ui"
)

// Settings builds retain the same controls and reuse the published preference data.
func BenchmarkSettingsMVVM(b *testing.B) {
	for _, section := range []string{"playback", "cache", "download"} {
		b.Run(section, func(b *testing.B) {
			a := newTestApp()
			a.settings = defaultSettings()
			a.preferences.Section = section
			a.cacheSettings.Used = 0
			tt := ui.NewTester(a.views.settings.Build, 780, 640)
			tt.Frame()
			b.ReportAllocs()
			for b.Loop() {
				tt.Frame()
			}
		})
	}
}
