package app

import (
	"fmt"
	"testing"

	"github.com/egoist/mygo/ui"
	"rsc.io/qr"

	"netease-music-mygo/internal/netease"
	accountvm "netease-music-mygo/internal/viewmodel/account"
)

// Account controls reuse retained UI state without polling or rebuilding their data.
func BenchmarkAccountSurfaceFrame(b *testing.B) {
	for _, dialog := range []bool{false, true} {
		b.Run(fmt.Sprint(dialog), func(b *testing.B) {
			a := newTestApp()
			a.settings = defaultSettings()
			a.account.Profile = &netease.Profile{UserID: 1, Nickname: "小明"}
			a.account.Login = accountvm.LoginState{Open: dialog, Status: netease.QRWaiting}
			a.account.Login.Code, _ = qr.Encode("https://music.163.com/login?code=test", qr.M)
			tt := ui.NewTester(func(c *ui.Context) {
				a.pal = paletteFor(c.Theme())
				if dialog {
					a.loginDialog(c)
				} else {
					a.accountRow(c)
				}
			}, 800, 600)
			tt.Frame()
			b.ReportAllocs()
			for b.Loop() {
				tt.Frame()
			}
		})
	}
}
