/*
验证快捷键名称与按键组合能够往返转换。
*/

package shortcuts

import (
	"testing"

	"github.com/egoist/mygo/ui"
)

// Accelerators go back and forth between the keys the window binds and the
// names the settings keep; the ones only the system sees name no key.
func TestAcceleratorsGoBackAndForth(t *testing.T) {
	for _, c := range []struct {
		mods ui.Modifiers
		key  ui.Key
		acc  string
	}{
		{0, ui.KeySpace, "space"},
		{ui.Cmd, ui.KeyL, "CmdOrCtrl+l"},
		{ui.Cmd | ui.Alt, ui.KeyUp, "CmdOrCtrl+Alt+up"},
		{ui.Cmd | ui.Shift, ui.KeyEqual, "CmdOrCtrl+Shift+="},
		{ui.Alt, ui.KeyMinus, "Alt+-"},
		{ui.Shift, ui.KeyF5, "Shift+f5"},
	} {
		acc, ok := Format(c.mods, c.key)
		if !ok || acc != c.acc {
			t.Errorf("%v and %v gave %q, want %q", c.mods, c.key, acc, c.acc)
			continue
		}
		mods, key, ok := Parse(acc)
		if !ok || mods != c.mods || key != c.key {
			t.Errorf("%q went back to %v and %v, want %v and %v", acc, mods, key, c.mods, c.key)
		}
	}
	if _, _, ok := Parse(""); ok {
		t.Errorf("an empty accelerator parsed")
	}
	if _, _, ok := Parse("MediaPlayPause"); ok {
		t.Errorf("a media key parsed as a key of the window")
	}
	if _, ok := Format(ui.Shift, ui.KeyUnknown); ok {
		t.Errorf("a modifier on its own made an accelerator")
	}
}
