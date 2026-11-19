//go:build windows

package app

import (
	"fmt"
	"os"
	"strings"
	"testing"

	"golang.org/x/sys/windows/registry"
)

// TestRegisterFileAssociations checks that an app upgraded without the
// installer publishes both audio types in the current user's open-with list.
func TestRegisterFileAssociations(t *testing.T) {
	const progID = "dev.egoist.cloudmusic."
	var keys []string
	for _, ext := range []string{"mp3", "flac"} {
		keys = append(keys, `Software\Classes\`+progID+ext)
	}
	for _, key := range keys {
		registry.DeleteKey(registry.CURRENT_USER, key+`\shell\open`)
		registry.DeleteKey(registry.CURRENT_USER, key+`\shell`)
		registry.DeleteKey(registry.CURRENT_USER, key+`\DefaultIcon`)
		registry.DeleteKey(registry.CURRENT_USER, key)
	}
	for _, ext := range []string{"mp3", "flac"} {
		key, _, err := registry.CreateKey(registry.CURRENT_USER, `Software\Classes\.`+ext+`\OpenWithProgids`, registry.ALL_ACCESS)
		if err != nil {
			t.Fatal(err)
		}
		if err := key.DeleteValue(progID + ext); err != nil && err != registry.ErrNotExist {
			key.Close()
			t.Fatal(err)
		}
		if err := key.Close(); err != nil {
			t.Fatal(err)
		}
	}
	t.Cleanup(func() {
		for _, key := range keys {
			registry.DeleteKey(registry.CURRENT_USER, key+`\shell\open\command`)
			registry.DeleteKey(registry.CURRENT_USER, key+`\shell\open`)
			registry.DeleteKey(registry.CURRENT_USER, key+`\shell`)
			registry.DeleteKey(registry.CURRENT_USER, key+`\DefaultIcon`)
			registry.DeleteKey(registry.CURRENT_USER, key)
		}
		for _, ext := range []string{"mp3", "flac"} {
			key, err := registry.OpenKey(registry.CURRENT_USER, `Software\Classes\.`+ext+`\OpenWithProgids`, registry.SET_VALUE)
			if err == nil {
				key.DeleteValue(progID + ext)
				key.Close()
			}
		}
	})
	if err := registerFileAssociations(); err != nil {
		t.Fatal(err)
	}
	exe, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	for _, ext := range []string{"mp3", "flac"} {
		key, err := registry.OpenKey(registry.CURRENT_USER, `Software\Classes\`+progID+ext+`\shell\open\command`, registry.QUERY_VALUE)
		if err != nil {
			t.Fatalf("open command for %s: %v", ext, err)
		}
		command, _, err := key.GetStringValue("")
		key.Close()
		if err != nil {
			t.Fatal(err)
		}
		want := fmt.Sprintf(`"%s" "%%1"`, exe)
		if command != want {
			t.Fatalf("open command for %s = %q, want %q", ext, command, want)
		}
		key, err = registry.OpenKey(registry.CURRENT_USER, `Software\Classes\.`+ext+`\OpenWithProgids`, registry.QUERY_VALUE)
		if err != nil {
			t.Fatal(err)
		}
		values, err := key.ReadValueNames(0)
		key.Close()
		if err != nil {
			t.Fatal(err)
		}
		found := false
		for _, value := range values {
			if strings.EqualFold(value, progID+ext) {
				found = true
			}
		}
		if !found {
			t.Errorf("%s does not offer this app in open with", ext)
		}
	}
}
