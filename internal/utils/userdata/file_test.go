/*
验证数据提交失败时保留原文件并清理暂存文件。
*/

package userdata

import (
	"os"
	"path/filepath"
	"testing"
)

// A failed commit keeps its destination intact and removes the staged file for both existing write paths.
func TestFailedWriteKeepsDestination(t *testing.T) {
	for _, jsonFile := range []bool{false, true} {
		name := "atomic"
		if jsonFile {
			name = "json"
		}
		t.Run(name, func(t *testing.T) {
			dir := t.TempDir()
			destination := filepath.Join(dir, "destination")
			if err := os.Mkdir(destination, 0700); err != nil {
				t.Fatal(err)
			}
			sentinel := filepath.Join(destination, "preserved")
			if err := os.WriteFile(sentinel, []byte("original"), 0600); err != nil {
				t.Fatal(err)
			}
			if jsonFile {
				if WriteJSON(destination, map[string]string{"value": "new"}) {
					t.Fatal("write unexpectedly replaced a directory")
				}
			} else if err := Write(destination, []byte("new")); err == nil {
				t.Fatal("write unexpectedly replaced a directory")
			}
			if b, err := os.ReadFile(sentinel); err != nil || string(b) != "original" {
				t.Fatal("failed rename changed the destination")
			}
			entries, err := os.ReadDir(dir)
			if err != nil || len(entries) != 1 {
				t.Fatal("failed write leaked a staged file", err)
			}
		})
	}
}
