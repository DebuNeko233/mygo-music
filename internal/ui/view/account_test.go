/*
验证钥匙串帮助的复制失败提示，以及重新登录命令的触发。
*/

package view

import (
	"errors"
	"image/png"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/egoist/mygo/ui"
	"netease-music-mygo/internal/features/netease"
	"netease-music-mygo/internal/ui/view/components"
	"netease-music-mygo/internal/ui/view/page"
	accountvm "netease-music-mygo/internal/ui/viewmodel/account"
)

// Keyring guidance reports clipboard failures and lets the listener start a
// fresh login through the injected command, without owning account services.
func TestKeyringHelpCopyFailureAndRetry(t *testing.T) {
	for _, dark := range []bool{false, true} {
		model := accountvm.Account{Login: accountvm.LoginState{Open: true, Keyring: true, Err: "登录凭据保存失败，本次登录仅在应用运行期间有效"}}
		var palette components.Palette
		retries, notifications := 0, 0
		v := Account{Page: &page.Page{Palette: &palette}, Model: &model,
			CopyText: func(text string) error {
				if text != netease.KeyringHelp || text == "" {
					t.Fatal("missing keyring instructions")
				}
				return errors.New("clipboard unavailable")
			},
			Notify: func(message string) {
				if message == "" {
					t.Fatal("empty failure notification")
				}
				notifications++
			},
			OpenLogin: func() { retries++ },
		}
		tt := ui.NewTester(func(c *ui.Context) {
			palette = components.PaletteFor(c.Theme(), components.AccentColor("", c.Theme().Dark))
			v.Login(c)
		}, 800, 760)
		tt.SetDark(dark)
		tt.Advance(time.Second)
		tt.Frame()
		if err := tt.Click("复制设置帮助"); err != nil {
			t.Fatal(err)
		}
		tt.Frame()
		if notifications != 1 {
			t.Fatal("copy failure was not reported")
		}
		if err := tt.Click("重试登录"); err != nil {
			t.Fatal(err)
		}
		tt.Frame()
		if retries != 1 {
			t.Fatal("retry did not invoke the login command")
		}
		if dir := os.Getenv("NM_FRAMES"); dir != "" {
			name := "keyring-light.png"
			if dark {
				name = "keyring-dark.png"
			}
			f, err := os.Create(filepath.Join(dir, name))
			if err != nil {
				t.Fatal(err)
			}
			err = png.Encode(f, tt.Image())
			closeErr := f.Close()
			if err != nil {
				t.Fatal(err)
			}
			if closeErr != nil {
				t.Fatal(closeErr)
			}
		}
	}
}
