/*
通过同目录暂存和原子替换写入用户数据文件及 JSON 内容。
*/

package userdata

import (
	"encoding/json"
	"os"
	"path/filepath"
)

// Write atomically replaces a file in the app's user data.
func Write(path string, b []byte) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	// Stage beside the destination so the rename stays on the same volume.
	f, err := os.CreateTemp(filepath.Dir(path), ".data-*.part")
	if err != nil {
		return err
	}
	tmp := f.Name()
	defer os.Remove(tmp)
	if _, err := f.Write(b); err != nil {
		f.Close()
		return err
	}
	if err := f.Sync(); err != nil {
		f.Close()
		return err
	}
	if err := f.Close(); err != nil {
		return err
	}
	if err := os.Chmod(tmp, 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}

// WriteJSON stages a serialized file beside its destination. The caller serializes writes.
func WriteJSON(path string, value any) bool {
	b, err := json.Marshal(value)
	if err != nil || os.WriteFile(path+".part", b, 0o600) != nil {
		return false
	}
	if os.Rename(path+".part", path) != nil {
		_ = os.Remove(path + ".part")
		return false
	}
	return true
}
