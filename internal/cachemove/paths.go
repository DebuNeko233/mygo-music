// Package cachemove moves a song cache to another directory: it looks at the
// two directories, and copies the songs that are whole, once nothing reads or
// writes either cache any more.
package cachemove

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// SameDirectory reports whether two paths are of the same directory, as the
// system has them: compared as names, then as what they name, which tells
// a link from its target, and on macOS or Windows, one case from another.
func SameDirectory(source, target string) (bool, error) {
	source, err := resolve(source)
	if err != nil {
		return false, err
	}
	target, err = resolve(target)
	if err != nil {
		return false, err
	}
	if rel, err := filepath.Rel(source, target); err == nil && rel == "." {
		return true, nil
	}
	src, err := os.Stat(source)
	if errors.Is(err, os.ErrNotExist) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	dst, err := os.Stat(target)
	if errors.Is(err, os.ErrNotExist) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	return os.SameFile(src, dst), nil
}

// Paths returns the two directories as the system has them, following the
// links of both and of their parents, and refuses ones that are the same or
// inside each other: what moves would then be what it moves into.
func Paths(source, target string) (string, string, error) {
	source, err := resolve(source)
	if err != nil {
		return "", "", err
	}
	target, err = resolve(target)
	if err != nil {
		return "", "", err
	}
	for _, pair := range [][2]string{{source, target}, {target, source}} {
		rel, err := filepath.Rel(pair[0], pair[1])
		if err == nil && (rel == "." || (rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator)))) {
			return "", "", fmt.Errorf("新旧缓存目录不能相同或互相包含")
		}
	}
	return source, target, nil
}

// resolve returns the directory a path names, as the system has it, whether
// it is there yet or not, and says why it cannot be a cache.
func resolve(path string) (string, error) {
	if !filepath.IsAbs(path) {
		return "", fmt.Errorf("缓存目录必须是绝对路径")
	}
	path = filepath.Clean(path)
	resolved, err := filepath.EvalSymlinks(path)
	if err == nil {
		info, err := os.Stat(resolved)
		if err != nil {
			return "", err
		}
		if !info.IsDir() {
			return "", fmt.Errorf("缓存路径不是目录：%s", resolved)
		}
		return resolved, nil
	}
	if !errors.Is(err, os.ErrNotExist) {
		return "", err
	}
	// Not there yet: where it would be, in a parent as the system has it.
	parent := filepath.Dir(path)
	if parent == path {
		return "", err
	}
	parent, err = resolve(parent)
	if err != nil {
		return "", err
	}
	return filepath.Join(parent, filepath.Base(path)), nil
}

// Validate refuses a target that is not a directory of its own, empty: what
// is in it is the listener's, and is never merged with, written over, or
// removed, migrated or not.
func Validate(source, target string) error {
	_, target, err := Paths(source, target)
	if err != nil {
		return err
	}
	entries, err := os.ReadDir(target)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	if len(entries) != 0 {
		return fmt.Errorf("请选择空目录，目标目录中已有文件")
	}
	return nil
}
