/*
基准测量主窗口外壳的界面构建开销。
*/

package app

import (
	"testing"
	"time"

	"github.com/egoist/mygo/ui"
)

// Full-window frames include the sidebar, scrolling page and playback overlays.
func BenchmarkShellViewFrame(b *testing.B) {
	for _, page := range []struct {
		name  string
		route route
	}{{"home", route{Page: pageHome}}, {"charts", route{Page: pageCharts}}} {
		b.Run(page.name, func(b *testing.B) {
			a := homePerfApp(30)
			a.home.At = time.Now()
			a.nav.Current = page.route
			tt := ui.NewTester(a.view, 1200, 800)
			tt.Frame()
			tt.Advance(time.Second)
			b.ReportAllocs()
			for b.Loop() {
				tt.Frame()
			}
		})
	}
}
