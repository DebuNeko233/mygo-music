package architecture

import (
	"go/parser"
	"go/token"
	"io/fs"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

// Feature logic stays usable without the desktop, and services cannot depend
// on their callers. Parse every platform's production files, not just this host's.
func TestPackageBoundaries(t *testing.T) {
	const prefix = "netease-music-mygo/internal/"
	err := filepath.WalkDir("..", func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.IsDir() {
			if entry.Name() == "testdata" {
				return filepath.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
			return nil
		}
		rel, err := filepath.Rel("..", filepath.Dir(path))
		if err != nil {
			return err
		}
		pkg := filepath.ToSlash(rel)
		layer := strings.Split(pkg, "/")[0]
		file, err := parser.ParseFile(token.NewFileSet(), path, nil, parser.ImportsOnly)
		if err != nil {
			return err
		}
		for _, spec := range file.Imports {
			dep, err := strconv.Unquote(spec.Path.Value)
			if err != nil {
				return err
			}
			if layer == "viewmodel" && inPackage(dep, "github.com/egoist/mygo") {
				t.Errorf("%s imports UI framework %s; keep widgets and windows in desktop/view", path, dep)
			}
			if !strings.HasPrefix(dep, prefix) {
				continue
			}
			target := strings.TrimPrefix(dep, prefix)
			if inPackage(target, "testutil") || (strings.HasSuffix(target, "/internal/testutil") || strings.Contains(target, "/internal/testutil/")) {
				t.Errorf("%s imports test helper %s; keep test fixtures out of production", path, dep)
			}
			if layer != "app" && inPackage(target, "app") {
				t.Errorf("%s imports %s; app owns composition", path, dep)
			}
			if layer != "app" && layer != "desktop" && inPackage(target, "desktop") {
				t.Errorf("%s imports %s; lower layers must not depend on desktop views", path, dep)
			}
			if layer != "app" && layer != "desktop" && layer != "viewmodel" && inPackage(target, "viewmodel") {
				t.Errorf("%s imports %s; services must not depend on feature presentation", path, dep)
			}
			if layer == "desktop" && !inPackage(pkg, "desktop/view") &&
				(inPackage(target, "desktop/view") || inPackage(target, "viewmodel")) {
				t.Errorf("%s imports %s; desktop capabilities must stay independent of views and feature presentation", path, dep)
			}
			if layer == "viewmodel" && strings.HasPrefix(target, "viewmodel/") &&
				!inPackage(target, pkg) {
				t.Errorf("%s imports another feature %s; compose features in app or share a lower-level capability", path, dep)
			}
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}

func inPackage(path, pkg string) bool {
	return path == pkg || strings.HasPrefix(path, pkg+"/")
}
