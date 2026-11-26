/*
验证快捷键默认值补全、冲突拒绝和恢复操作。
*/

package shortcuts

import (
	"testing"

	"netease-music-mygo/internal/features/preferences"
)

// The keys the settings hold as empty, or as something the set cannot take,
// give way to the action's own.
func TestTheShortcutSettingsFillInTheDefaults(t *testing.T) {
	filled := Fill(preferences.ShortcutKeys{})
	if len(filled.InApp) != len(Actions()) || len(filled.Global) != len(Actions()) {
		t.Fatalf("filled %d in-app and %d global keys, want %d each",
			len(filled.InApp), len(filled.Global), len(Actions()))
	}
	next := Action{}
	for _, h := range Actions() {
		if h.Name == "next" {
			next = h
		}
	}

	kept := Fill(preferences.ShortcutKeys{
		InApp:  map[string]string{"playPause": "", "next": "Bogus+Q"},
		Global: map[string]string{"next": "J", "prev": ""},
	})
	if kept.InApp["playPause"] != "" {
		t.Errorf("a key the listener took away came back as %q", kept.InApp["playPause"])
	}
	if kept.InApp["next"] != next.InApp {
		t.Errorf("a key the app cannot take was kept: %q", kept.InApp["next"])
	}
	if kept.Global["next"] != next.Global {
		t.Errorf("a global key without a modifier was kept: %q", kept.Global["next"])
	}
	if kept.Global["prev"] != "" {
		t.Errorf("a global key the listener took away came back as %q", kept.Global["prev"])
	}
}

// Equivalent accelerator spellings conflict within one scope, while a disabled
// binding stays disabled until restoring it and leaves the other scope intact.
func TestBindingConflictAndRestore(t *testing.T) {
	keys := preferences.ShortcutKeys{}
	Set(&keys, InApp, "next", "Ctrl+Alt+j")
	if _, taken := Taken(keys, InApp, "prev", "Control+Option+J"); !taken {
		t.Fatal("equivalent binding was accepted twice")
	}
	if _, taken := Taken(keys, InApp, "next", "Control+Option+J"); taken {
		t.Fatal("own binding conflicts with itself")
	}
	if _, taken := Taken(keys, Global, "prev", "Control+Option+J"); taken {
		t.Fatal("in-app binding occupied the global scope")
	}
	Set(&keys, Global, "next", "")
	if Binding(keys, Global, "next") != "" || !Changed(keys, Global, "next") {
		t.Fatal("explicit disable restored the default")
	}
	Restore(&keys, InApp, "next")
	if Changed(keys, InApp, "next") || Binding(keys, InApp, "next") == "Ctrl+Alt+j" || Binding(keys, Global, "next") != "" {
		t.Fatal("restore affected the wrong scope")
	}
	Restore(&keys, Global, "next")
	if Changed(keys, Global, "next") || Binding(keys, Global, "next") == "" {
		t.Fatal("default global binding was not restored")
	}
}
