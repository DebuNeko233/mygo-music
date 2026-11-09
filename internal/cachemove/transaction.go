package cachemove

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
)

// Options describes one relocation; callbacks run on its worker.
type Options struct {
	Context  context.Context
	Journal  string // fixed user-data path, allowing cleanup even if the target changes
	Progress func(copied, total int64)
}

// Stage inside the destination, including when it is a filesystem root.
// Publish children rather than renaming that root; rollback owns only the
// children it published, and never overwrites an occupied destination.
func transferWithOptions(target string, files []cacheFile, publications []string, validate func(string) error, save func() error, copy func(string, string) error, options Options) (warning error, err error) {
	ctx := options.Context
	if ctx == nil {
		ctx = context.Background()
	}
	if err = ctx.Err(); err != nil {
		return nil, err
	}
	if err = validate(""); err != nil {
		return nil, err
	}
	_, statErr := os.Stat(target)
	created := errors.Is(statErr, os.ErrNotExist)
	if err = os.MkdirAll(target, 0o700); err != nil {
		return nil, err
	}
	// Removing an empty directory on failure is safe; an existing mount root,
	// or any unrelated file created meanwhile, is never removed.
	defer func() {
		if err != nil && created {
			os.Remove(target)
		}
	}()
	stage, err := os.MkdirTemp(target, stagePrefix)
	if err != nil {
		return nil, err
	}
	defer func() { os.RemoveAll(stage); forgetJournal(options.Journal, stage) }()
	if err = recordStage(options.Journal, target, stage); err != nil {
		return nil, err
	}
	for _, name := range publications {
		if err = os.MkdirAll(filepath.Join(stage, name), 0o700); err != nil {
			return nil, err
		}
	}
	var copied, total int64
	for _, file := range files {
		if !file.reused {
			info, statErr := os.Stat(file.source)
			if statErr != nil {
				return nil, statErr
			}
			total += info.Size()
		}
	}
	notify := func(n int64) {
		copied += n
		if options.Progress != nil {
			options.Progress(copied, total)
		}
	}
	notify(0)
	for _, file := range files {
		if err = ctx.Err(); err != nil {
			return nil, err
		}
		if file.reused {
			continue
		}
		to := filepath.Join(stage, file.name)
		if err = os.MkdirAll(filepath.Dir(to), 0o700); err != nil {
			return nil, err
		}
		if copy != nil {
			err = copy(file.source, to)
			if err == nil {
				info, statErr := os.Stat(to)
				err = statErr
				if err == nil {
					notify(info.Size())
				}
			}
		} else {
			err = copyProgress(ctx, file.source, to, notify)
		}
		if err != nil {
			return nil, err
		}
	}
	if err = ctx.Err(); err != nil {
		return nil, err
	}
	if err = validate(stage); err != nil {
		return nil, err
	}
	// Flat song-only moves and whole layouts use the same child publication,
	// so a target mount point is never removed or renamed.
	if len(publications) == 1 && publications[0] == "" {
		publications = nil
		entries, readErr := os.ReadDir(stage)
		if readErr != nil {
			return nil, readErr
		}
		for _, entry := range entries {
			if entry.Name() != stageMarker {
				publications = append(publications, entry.Name())
			}
		}
	}
	var published []string
	rollback := func() {
		owned := func(name string) bool {
			for _, root := range published {
				if name == root || filepath.Dir(name) == root {
					return true
				}
			}
			return false
		}
		for _, file := range files {
			if !file.reused && owned(file.name) {
				os.Remove(filepath.Join(target, file.name))
			}
		}
		for _, name := range published {
			os.Remove(filepath.Join(target, name))
		}
	}
	for _, name := range publications {
		if err = ctx.Err(); err != nil {
			rollback()
			return nil, err
		}
		to := filepath.Join(target, name)
		info, statErr := os.Lstat(to)
		if statErr == nil && !info.IsDir() {
			rollback()
			return nil, fmt.Errorf("%w：%s", ErrConflict, to)
		}
		if statErr != nil && !errors.Is(statErr, os.ErrNotExist) {
			rollback()
			return nil, statErr
		}
		if statErr == nil {
			err = os.Remove(to)
		} else {
			err = nil
		}
		if err != nil {
			rollback()
			return nil, err
		}
		if err = os.Rename(filepath.Join(stage, name), to); err != nil {
			rollback()
			return nil, err
		}
		published = append(published, name)
	}
	if err = ctx.Err(); err != nil {
		rollback()
		return nil, err
	}
	if err = save(); err != nil {
		rollback()
		return nil, err
	}
	// save also rebinds read-only stores before source files are retired.
	// Cancellation after this commit cannot undo settings or remove the target.
	for _, file := range files {
		if removeErr := os.Remove(file.source); removeErr != nil {
			warning = errors.Join(warning, removeErr)
		}
	}
	return warning, nil
}
