/*
统一音频容器名称与扩展名，并探测跳过 ID3 标签后的实际音频格式。
*/

// Package audioformat names the audio formats shared by playback, import and cache maintenance.
package audioformat

import (
	"io"
	"strings"
)

// Normalize returns a safe canonical container name, or empty for an unknown format.
// M4A and the 3GPP variants share the MP4 audio container reader.
func Normalize(name string) string {
	switch strings.TrimPrefix(strings.ToLower(strings.TrimSpace(name)), ".") {
	case "mp3":
		return "mp3"
	case "flac":
		return "flac"
	case "wav", "wave":
		return "wav"
	case "ogg", "vorbis":
		return "ogg"
	case "aac", "adts":
		return "aac"
	case "m4a", "mp4", "3gp", "3g2", "alac":
		return "m4a"
	}
	return ""
}

// Canonical reports names safe to use as extensions in the song cache.
func Canonical(name string) bool { return name != "" && Normalize(name) == name }

// Extensions returns the supported file-picker/import extensions.
func Extensions() []string { return []string{"mp3", "flac", "wav", "ogg", "m4a", "aac", "3gp", "3g2"} }

// ID3Size returns the byte offset after a leading ID3v2 tag.
func ID3Size(header []byte) int64 {
	if len(header) < 10 || string(header[:3]) != "ID3" {
		return 0
	}
	return 10 + (int64(header[6]&0x7f)<<21 | int64(header[7]&0x7f)<<14 | int64(header[8]&0x7f)<<7 | int64(header[9]&0x7f))
}

// Detect identifies a container from bytes after any leading ID3 tag.
func Detect(header []byte) string {
	if len(header) >= 12 && string(header[:4]) == "RIFF" && string(header[8:12]) == "WAVE" {
		return "wav"
	}
	if len(header) >= 4 && string(header[:4]) == "OggS" {
		return "ogg"
	}
	if len(header) >= 12 && string(header[4:8]) == "ftyp" {
		return "m4a"
	}
	if len(header) >= 4 && string(header[:4]) == "fLaC" {
		return "flac"
	}
	if len(header) >= 2 && header[0] == 0xff && header[1]&0xf6 == 0xf0 {
		return "aac"
	}
	if len(header) >= 4 && header[0] == 0xff && header[1]&0xe0 == 0xe0 && header[1]&0x18 != 0x08 && header[1]&6 != 0 {
		return "mp3"
	}
	return ""
}

// Probe recognizes a seekable source using the same signatures as playback,
// skipping its leading ID3 tag. Unrecognized signatures return empty.
func Probe(source io.ReaderAt) string {
	var header [12]byte
	n, _ := source.ReadAt(header[:], 0)
	if off := ID3Size(header[:n]); off > 0 {
		n, _ = source.ReadAt(header[:], off)
	}
	return Detect(header[:n])
}
