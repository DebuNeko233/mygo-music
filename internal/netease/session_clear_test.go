package netease

import (
	"errors"
	"net/http"
	"net/http/cookiejar"
	"os"
	"path/filepath"
	"testing"

	"github.com/egoist/mygo"
)

// Clearing a session drops current cookies and reports failed disk deletion, while a missing file is harmless.
func TestClearSessionReportsDeletionFailure(t *testing.T) {
	t.Setenv("MYGO_ENV", "development")
	dir := t.TempDir()
	oldDir, err := mygo.App.Path(mygo.PathUserData)
	if err != nil {
		t.Fatal(err)
	}
	mygo.App.SetPath(mygo.PathUserData, dir)
	t.Cleanup(func() { mygo.App.SetPath(mygo.PathUserData, oldDir) })
	oldJar := httpClient.Jar
	httpClient.Jar, _ = cookiejar.New(nil)
	t.Cleanup(func() { httpClient.Jar = oldJar })
	httpClient.Jar.SetCookies(siteURL, []*http.Cookie{{Name: "MUSIC_U", Value: "old-session"}})
	path := filepath.Join(dir, "session")
	if err := os.Mkdir(path, 0o700); err != nil {
		t.Fatal(err)
	}
	child := filepath.Join(path, "occupied")
	if err := os.WriteFile(child, []byte("fixture"), 0o600); err != nil {
		t.Fatal(err)
	}
	var deletion *os.PathError
	if err := ClearSession(); !errors.As(err, &deletion) {
		t.Fatalf("failed session deletion returned %v", err)
	}
	if SessionIdentity() != "" {
		t.Fatal("failed disk deletion kept current session cookies")
	}
	if err := os.Remove(child); err != nil {
		t.Fatal(err)
	}
	if err := ClearSession(); err != nil {
		t.Fatal(err)
	}
	if err := ClearSession(); err != nil {
		t.Fatalf("an absent session is already cleared: %v", err)
	}
}
