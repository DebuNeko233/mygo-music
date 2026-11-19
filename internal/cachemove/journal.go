package cachemove

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

const stagePrefix = ".song-cache-move-"
const stageMarker = ".mygo-cache-move.json"

type stageRecord struct {
	Version   int    `json:"version"`
	Target    string `json:"target"`
	Stage     string `json:"stage"`
	ID        string `json:"id"`
	Committed bool   `json:"committed,omitempty"` // recorded before moving away from a disconnected target
}

// recordStage persists ownership before any potentially large copy starts.
// The fixed journal lives outside the cache root, so choosing a different
// target after a crash still discovers the old hidden staging directory.
func recordStage(journal, target, stage string) (stageRecord, error) {
	var err error
	if target, err = resolve(target); err != nil {
		return stageRecord{}, err
	}
	if stage, err = resolve(stage); err != nil {
		return stageRecord{}, err
	}
	if filepath.Dir(stage) != target {
		return stageRecord{}, fmt.Errorf("暂存目录不在目标缓存内")
	}
	var nonce [16]byte
	if _, err := rand.Read(nonce[:]); err != nil {
		return stageRecord{}, err
	}
	record := stageRecord{Version: 2, Target: target, Stage: stage, ID: hex.EncodeToString(nonce[:])}
	records, err := readJournal(journal)
	if err != nil {
		return stageRecord{}, err
	}
	b, err := json.Marshal(record)
	if err != nil {
		return stageRecord{}, err
	}
	if err = writeRecord(filepath.Join(stage, stageMarker), b); err != nil {
		return stageRecord{}, err
	}
	return record, saveJournal(journal, append(records, record))
}

func writeRecord(path string, b []byte) (err error) {
	if err = os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	f, err := os.CreateTemp(filepath.Dir(path), ".cache-move-record-")
	if err != nil {
		return err
	}
	defer os.Remove(f.Name())
	_, err = f.Write(b)
	if err == nil {
		err = f.Sync()
	}
	err = errors.Join(err, f.Close())
	if err != nil {
		return err
	}
	return os.Rename(f.Name(), path)
}

// Recover uses the saved layout to distinguish a committed publication from
// a copy interrupted before settings were saved. Only owned copies are removed.
// Call before starting another move, under the app's single-migration guard.
func Recover(journal string, active Layout) error {
	if journal == "" {
		return nil
	}
	records, err := readJournal(journal)
	if err != nil {
		return err
	}
	var remaining []stageRecord
	for _, record := range records {
		if _, err := os.Stat(record.Target); errors.Is(err, os.ErrNotExist) {
			// A removable disk may be disconnected. Keep its record while
			// allowing a new move to proceed. Remember a committed move
			// before settings can change to a different drive.
			if record.Version == 2 && !record.Committed {
				record.Committed, err = isPublishedLayout(record.Target, active)
				if err != nil {
					return err
				}
			}
			remaining = append(remaining, record)
			continue
		}
		if err := recoverStage(record, active); err != nil {
			return err
		}
	}
	return saveJournal(journal, remaining)
}

func isPublishedLayout(target string, active Layout) (bool, error) {
	for _, group := range active.groups() {
		if group.source == "" {
			return false, nil
		}
		same, err := SameDirectory(group.source, filepath.Join(target, group.name))
		if err != nil || !same {
			return false, err
		}
	}
	return true, nil
}

func readJournal(journal string) ([]stageRecord, error) {
	b, err := os.ReadFile(journal)
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var records []stageRecord
	if json.Unmarshal(b, &records) != nil {
		return nil, fmt.Errorf("缓存迁移记录无效")
	}
	for _, record := range records {
		_, idErr := hex.DecodeString(record.ID)
		if (record.Version != 1 && record.Version != 2) || !filepath.IsAbs(record.Target) || !filepath.IsAbs(record.Stage) || filepath.Dir(record.Stage) != record.Target || !strings.HasPrefix(filepath.Base(record.Stage), stagePrefix) || len(record.ID) != 32 || idErr != nil {
			return nil, fmt.Errorf("缓存迁移记录无效")
		}
	}
	return records, nil
}

func saveJournal(journal string, records []stageRecord) error {
	if len(records) == 0 {
		err := os.Remove(journal)
		if errors.Is(err, os.ErrNotExist) {
			return nil
		}
		return err
	}
	b, err := json.Marshal(records)
	if err != nil {
		return err
	}
	return writeRecord(journal, b)
}

func recoverStage(record stageRecord, active Layout) error {
	// Resolve again: a parent replaced with a symlink must not redirect cleanup.
	target, err := resolve(record.Target)
	if err != nil {
		return err
	}
	if target != record.Target {
		return fmt.Errorf("缓存迁移路径已更改")
	}
	info, err := os.Lstat(record.Stage)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	if !info.IsDir() {
		return fmt.Errorf("缓存暂存路径不是独立目录")
	}
	marker := filepath.Join(record.Stage, stageMarker)
	info, err = os.Lstat(marker)
	if err != nil {
		return err
	}
	if !info.Mode().IsRegular() {
		return fmt.Errorf("缓存暂存记录不是普通文件")
	}
	owned, err := os.ReadFile(marker)
	if err != nil {
		return err
	}
	var check stageRecord
	markerRecord := record
	markerRecord.Committed = false
	if json.Unmarshal(owned, &check) != nil || check != markerRecord {
		return fmt.Errorf("缓存暂存记录不匹配")
	}
	if record.Version == 2 {
		for _, group := range active.groups() {
			if err := record.recoverPublication(group.name, group.source); err != nil {
				return err
			}
		}
	}
	if err = os.RemoveAll(record.Stage); err != nil {
		return err
	}
	return nil
}

// The manifest travels with a staged child in the same rename as its data.
// Its random name ties the publication to this transaction, even if the
// process exits immediately after the rename.
func (r stageRecord) publicationMarker() string { return stagePrefix + r.ID + ".json" }

func (r stageRecord) markPublication(name string) error {
	dir := filepath.Join(r.Stage, name)
	entries, err := os.ReadDir(dir)
	if err != nil {
		return err
	}
	names := make([]string, 0, len(entries))
	for _, entry := range entries {
		names = append(names, entry.Name())
	}
	b, err := json.Marshal(names)
	if err != nil {
		return err
	}
	return writeRecord(filepath.Join(dir, r.publicationMarker()), b)
}

func (r stageRecord) recoverPublication(name, active string) error {
	dir := filepath.Join(r.Target, name)
	info, err := os.Lstat(dir)
	if errors.Is(err, os.ErrNotExist) || err == nil && !info.IsDir() {
		return nil
	}
	if err != nil {
		return err
	}
	root, err := os.OpenRoot(dir)
	if err != nil {
		return err
	}
	defer root.Close()
	marker := r.publicationMarker()
	info, err = root.Lstat(marker)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	if !info.Mode().IsRegular() {
		return fmt.Errorf("缓存迁移发布记录不是普通文件")
	}
	b, err := root.ReadFile(marker)
	if err != nil {
		return err
	}
	var names []string
	if err := json.Unmarshal(b, &names); err != nil {
		return fmt.Errorf("缓存迁移发布记录无效：%w", err)
	}
	committed := r.Committed
	if !committed && active != "" {
		committed, err = SameDirectory(active, dir)
		if err != nil {
			return err
		}
	}
	if !committed {
		for _, file := range names {
			if !filepath.IsLocal(file) || filepath.Base(file) != file || file == marker {
				return fmt.Errorf("缓存迁移发布记录包含无效路径")
			}
			info, err := root.Lstat(file)
			if errors.Is(err, os.ErrNotExist) {
				continue
			}
			if err != nil {
				return err
			}
			if !info.Mode().IsRegular() {
				return fmt.Errorf("缓存迁移文件已更改：%s", file)
			}
			if err := root.Remove(file); err != nil {
				return err
			}
		}
	}
	// An empty child is a valid retry target. Leave the directory itself
	// in place, which also preserves any files added after publication.
	return root.Remove(marker)
}

// Do not discard recovery information if cleanup failed or another stage owns
// the record; a later launch can safely retry the remaining cleanup.
func forgetJournal(journal, stage string) {
	if journal == "" {
		return
	}
	if _, err := os.Lstat(stage); !errors.Is(err, os.ErrNotExist) {
		return
	}
	records, err := readJournal(journal)
	if err != nil {
		return
	}
	remaining := records[:0]
	for _, record := range records {
		if record.Stage != stage {
			remaining = append(remaining, record)
		}
	}
	saveJournal(journal, remaining)
}
