/*
检查所有平台的包依赖，约束功能、界面、应用协调和通用工具四层的依赖方向。
*/

package architecture

import (
	"go/parser"
	"go/token"
	"io/fs"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"testing"
)

// Business features and shared tools stay independent of presentation; view
// models remain usable without widgets, and only app coordinates the layers.
func TestPackageBoundaries(t *testing.T) {
	const prefix = "netease-music-mygo/internal/"
	root := filepath.Join("..", "..")
	err := filepath.WalkDir(root, func(path string, entry fs.DirEntry, err error) error {
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
		rel, err := filepath.Rel(root, filepath.Dir(path))
		if err != nil {
			return err
		}
		pkg := filepath.ToSlash(rel)
		layer := strings.Split(pkg, "/")[0]
		if !slices.Contains([]string{"app", "features", "ui", "util"}, layer) {
			t.Errorf("%s is outside app, features, ui and util", path)
		}
		vm := inPackage(pkg, "ui/viewmodel")
		if layer == "ui" && !vm && !inPackage(pkg, "ui/view") {
			t.Errorf("%s must belong to ui/view or ui/viewmodel", path)
		}
		file, err := parser.ParseFile(token.NewFileSet(), path, nil, parser.ImportsOnly)
		if err != nil {
			return err
		}
		for _, spec := range file.Imports {
			dep, err := strconv.Unquote(spec.Path.Value)
			if err != nil {
				return err
			}
			if (vm || layer == "util") && inPackage(dep, "github.com/egoist/mygo") {
				t.Errorf("%s imports UI framework %s; view models and generic tools must stay independent", path, dep)
			}
			if layer == "features" && inPackage(dep, "github.com/egoist/mygo/ui") {
				t.Errorf("%s imports widgets %s; features must stay independent of presentation", path, dep)
			}
			if !strings.HasPrefix(dep, prefix) {
				continue
			}
			target := strings.TrimPrefix(dep, prefix)
			if slices.Contains(strings.Split(target, "/"), "testutil") {
				t.Errorf("%s imports test helper %s; keep fixtures out of production", path, dep)
			}
			if layer != "app" && inPackage(target, "app") {
				t.Errorf("%s imports %s; app owns composition", path, dep)
			}
			if layer == "util" && !inPackage(target, "util") {
				t.Errorf("%s imports %s; generic tools must not depend on features or presentation", path, dep)
			}
			if layer == "features" && inPackage(target, "ui") {
				t.Errorf("%s imports %s; features must not depend on presentation", path, dep)
			}
			if vm && inPackage(target, "ui/view") {
				t.Errorf("%s imports %s; view models must not depend on views", path, dep)
			}
			if vm && strings.HasPrefix(target, "ui/viewmodel/") && !inPackage(target, pkg) {
				t.Errorf("%s imports another feature model %s; compose in app or share a lower-level capability", path, dep)
			}
			if (inPackage(pkg, "ui/view/artwork") || inPackage(pkg, "ui/view/shortcuts")) && inPackage(target, "ui") && !inPackage(target, pkg) {
				t.Errorf("%s imports %s; UI resource capabilities must stay independent of views and feature models", path, dep)
			}
			if (inPackage(pkg, "features/cache/disk") || inPackage(pkg, "features/cache/page")) && inPackage(target, "features/cache") && !inPackage(target, pkg) {
				t.Errorf("%s imports %s; base caches must stay independent of cache orchestration", path, dep)
			}
			if inPackage(pkg, "features/download/tagging") && ((inPackage(target, "features/download") && !inPackage(target, pkg)) || inPackage(target, "features/player")) {
				t.Errorf("%s imports %s; tagging must stay independent of download scheduling and playback", path, dep)
			}
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}
func inPackage(path, pkg string) bool { return path == pkg || strings.HasPrefix(path, pkg+"/") }
