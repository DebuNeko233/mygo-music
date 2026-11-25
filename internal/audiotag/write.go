/*
在不转码音频内容的前提下写入元数据，并限制标签处理的并发内存开销。
*/

// Package audiotag writes metadata without transcoding the audio payload.
package audiotag

import (
	"bytes"
	"fmt"
	"image"
	_ "image/gif"
	_ "image/jpeg"
	_ "image/png"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"

	"go.senan.xyz/taglib"
	_ "golang.org/x/image/webp"
)

// Serializing the short tagging stage bounds the WASM working set; playback
// and network transfers do not acquire this lock.
var writeMu sync.Mutex

// Write preserves unrelated tags. For private uploads missingOnly also keeps
// existing credits, lyrics and artwork rather than trusting a catalogue match.
// Only a private staging copy may be passed here.
func Write(path string, fields map[string][]string, cover []byte, missingOnly bool) error {
	writeMu.Lock()
	defer writeMu.Unlock()
	old, err := taglib.ReadTags(path)
	if err != nil {
		return fmt.Errorf("读取原文件标签：%w", err)
	}
	tags := make(map[string][]string)
	for k, values := range fields {
		values = slices.DeleteFunc(slices.Clone(values), func(s string) bool { return s == "" })
		if len(values) > 0 && (!missingOnly || len(old[k]) == 0) {
			tags[k] = values
		}
	}
	adts := strings.EqualFold(filepath.Ext(path), ".aac")
	if adts {
		// Keep all accessible original metadata in ID3v2 before retiring v1.
		for k, values := range old {
			if _, changed := tags[k]; !changed {
				tags[k] = values
			}
		}
	}
	if missingOnly {
		if pic, e := taglib.ReadImage(path); e == nil && len(pic) > 0 {
			cover = nil
		}
	}
	if len(cover) > 0 {
		if len(cover) > 32<<20 {
			return fmt.Errorf("专辑封面过大")
		}
		cfg, _, e := image.DecodeConfig(bytes.NewReader(cover))
		if e != nil || cfg.Width <= 0 || cfg.Height <= 0 || int64(cfg.Width)*int64(cfg.Height) > 100_000_000 {
			return fmt.Errorf("专辑封面不是有效图片")
		}
	}
	if len(tags) > 0 {
		if err := taglib.WriteTags(path, tags, 0); err != nil {
			return fmt.Errorf("写入标签：%w", err)
		}
	}
	if len(cover) > 0 {
		if err := taglib.WriteImage(path, cover); err != nil {
			return fmt.Errorf("写入封面：%w", err)
		}
	}
	if adts {
		if err := stripADTSv1(path); err != nil {
			return fmt.Errorf("整理 AAC 标签：%w", err)
		}
	}
	got, err := taglib.ReadTags(path)
	if err != nil {
		return fmt.Errorf("验证标签：%w", err)
	}
	for k, want := range tags {
		if !slices.Equal(got[k], want) {
			return fmt.Errorf("标签 %s 写入后不一致", k)
		}
	}
	if len(cover) > 0 {
		got, err := taglib.ReadImage(path)
		if err != nil || !bytes.Equal(got, cover) {
			return fmt.Errorf("封面写入后不一致")
		}
	}
	return nil
}

// FFmpeg's ADTS reader prefers a trailing ID3v1 tag over the full leading
// ID3v2 tag. TagLib writes both, but v1 cannot represent Unicode or lyrics.
// Strip only an exact v1 tail when a leading v2 tag holds the full metadata.
func stripADTSv1(path string) error {
	f, err := os.OpenFile(path, os.O_RDWR, 0)
	if err != nil {
		return err
	}
	defer f.Close()
	info, err := f.Stat()
	if err != nil {
		return err
	}
	if info.Size() < 138 {
		return nil
	}
	var head, tail [3]byte
	if _, err := f.ReadAt(head[:], 0); err != nil {
		return err
	}
	if _, err := f.ReadAt(tail[:], info.Size()-128); err != nil {
		return err
	}
	if string(head[:]) == "ID3" && string(tail[:]) == "TAG" {
		return f.Truncate(info.Size() - 128)
	}
	return nil
}
