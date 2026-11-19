package localmedia

import (
	"bytes"
	"io"
	"os"
	"path/filepath"
	"strings"
	"unicode/utf8"

	"github.com/dhowden/tag"
	"golang.org/x/text/encoding/simplifiedchinese"
	"golang.org/x/text/encoding/unicode"
)

// Tags reads descriptive tags and embedded artwork from a supported local file.
func Tags(path string) tag.Metadata {
	if st, err := os.Stat(path); err != nil || !st.Mode().IsRegular() {
		return nil
	}
	f, err := os.Open(path)
	if err != nil {
		return nil
	}
	defer f.Close()
	m, _ := tag.ReadFrom(f)
	return m
}

// Cover returns the embedded image, when present.
func Cover(path string) []byte {
	if m := Tags(path); m != nil {
		if pic := m.Picture(); pic != nil {
			return pic.Data
		}
	}
	return nil
}

// Lyrics reads a same-name LRC alongside the audio, including common BOMs.
func Lyrics(path string) string {
	if path == "" {
		return ""
	}
	base := strings.TrimSuffix(path, filepath.Ext(path))
	for _, suffix := range []string{".lrc", ".LRC"} {
		name := base + suffix
		if st, err := os.Stat(name); err != nil || !st.Mode().IsRegular() {
			continue
		}
		f, err := os.Open(name)
		if err != nil {
			continue
		}
		const limit = 2 << 20
		b, err := io.ReadAll(io.LimitReader(f, limit+1))
		f.Close()
		if err != nil || len(b) > limit {
			continue
		}
		var text []byte
		switch {
		case bytes.HasPrefix(b, []byte{0xff, 0xfe}), bytes.HasPrefix(b, []byte{0xfe, 0xff}):
			text, err = unicode.UTF16(unicode.LittleEndian, unicode.UseBOM).NewDecoder().Bytes(b)
		case utf8.Valid(b):
			text = bytes.TrimPrefix(b, []byte{0xef, 0xbb, 0xbf})
		default:
			text, err = simplifiedchinese.GB18030.NewDecoder().Bytes(b)
		}
		if err == nil {
			return string(text)
		}
	}
	return ""
}
