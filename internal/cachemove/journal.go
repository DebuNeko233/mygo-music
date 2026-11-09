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
	Version int    `json:"version"`
	Target  string `json:"target"`
	Stage   string `json:"stage"`
	ID      string `json:"id"`
}

// recordStage persists ownership before any potentially large copy starts.
// The fixed journal lives outside the cache root, so choosing a different
// target after a crash still discovers the old hidden staging directory.
func recordStage(journal, target, stage string) error {
	if journal == "" {
		return nil
	}
	var err error
	if target, err = resolve(target); err != nil {
		return err
	}
	if stage, err = resolve(stage); err != nil {
		return err
	}
	if filepath.Dir(stage) != target {
		return fmt.Errorf("暂存目录不在目标缓存内")
	}
	var nonce [16]byte
	if _, err := rand.Read(nonce[:]); err != nil {
		return err
	}
	record := stageRecord{1, target, stage, hex.EncodeToString(nonce[:])}
	records, err := readJournal(journal)
	if err != nil {
		return err
	}
	b, err := json.Marshal(record)
	if err != nil {
		return err
	}
	if err = writeRecord(filepath.Join(stage, stageMarker), b); err != nil {
		return err
	}
	return saveJournal(journal, append(records, record))
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

// Recover removes only a journaled stage with its matching ownership marker.
// It never guesses from a directory prefix or deletes source/published caches.
// Call before starting another move, under the app's single-migration guard.
func Recover(journal string) error {
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
			// allowing a new move to another target to proceed.
			remaining = append(remaining, record)
			continue
		}
		if err := recoverStage(record); err != nil {
			return err
		}
	}
	return saveJournal(journal, remaining)
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
		if record.Version != 1 || !filepath.IsAbs(record.Target) || !filepath.IsAbs(record.Stage) || filepath.Dir(record.Stage) != record.Target || !strings.HasPrefix(filepath.Base(record.Stage), stagePrefix) || len(record.ID) != 32 {
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

func recoverStage(record stageRecord) error {
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
	if json.Unmarshal(owned, &check) != nil || check != record {
		return fmt.Errorf("缓存暂存记录不匹配")
	}
	if err = os.RemoveAll(record.Stage); err != nil {
		return err
	}
	return nil
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
