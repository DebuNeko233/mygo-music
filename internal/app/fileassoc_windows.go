//go:build windows

/*
向 Windows 注册当前用户的音频文件类型和打开方式。
*/

package app

import (
	"errors"
	"fmt"
	"os"

	"golang.org/x/sys/windows"
	"golang.org/x/sys/windows/registry"
)

const (
	assocNotify = 0x08000000 // SHCNE_ASSOCCHANGED
	assocFlush  = 0x1000     // SHCNF_FLUSH
)

var (
	shell32            = windows.NewLazySystemDLL("shell32.dll")
	procSHChangeNotify = shell32.NewProc("SHChangeNotify")
)

// fileAssociation is one extension declared by mygo.json. Keep this list in
// sync with it: installed apps get the registration here after an update too.
type fileAssociation struct {
	ext, name string
}

var fileAssociations = []fileAssociation{
	{ext: "mp3", name: "MP3 音频"},
	{ext: "flac", name: "FLAC 音频"},
	{ext: "wav", name: "WAV 音频"},
	{ext: "ogg", name: "Ogg Vorbis 音频"},
	{ext: "m4a", name: "M4A 音频"},
	{ext: "aac", name: "AAC 音频"},
	{ext: "3gp", name: "3GPP 音频"},
	{ext: "3g2", name: "3GPP2 音频"},
}

// registerFileAssociations publishes the same per-user ProgIDs and open-with
// candidates as the NSIS installer. It does not replace the user's chosen
// default application in UserChoice.
func registerFileAssociations() error {
	exe, err := os.Executable()
	if err != nil {
		return err
	}
	command := fmt.Sprintf(`"%s" "%%1"`, exe)
	var errs []error
	for _, file := range fileAssociations {
		progID := "dev.egoist.cloudmusic." + file.ext
		class := `Software\Classes\` + progID
		for _, entry := range []struct{ key, value string }{
			{class, file.name},
			{class + `\DefaultIcon`, exe + ",0"},
			{class + `\shell\open\command`, command},
		} {
			key, _, err := registry.CreateKey(registry.CURRENT_USER, entry.key, registry.SET_VALUE)
			if err == nil {
				err = key.SetStringValue("", entry.value)
				err = errors.Join(err, key.Close())
			}
			if err != nil {
				errs = append(errs, fmt.Errorf("%s: %w", entry.key, err))
			}
		}
		key, _, err := registry.CreateKey(registry.CURRENT_USER, `Software\Classes\.`+file.ext+`\OpenWithProgids`, registry.SET_VALUE)
		if err == nil {
			err = key.SetStringValue(progID, "")
			err = errors.Join(err, key.Close())
		}
		if err != nil {
			errs = append(errs, fmt.Errorf("%s: %w", file.ext, err))
		}
	}
	if len(errs) != 0 {
		return fmt.Errorf("register local audio files: %w", errors.Join(errs...))
	}
	procSHChangeNotify.Call(assocNotify, assocFlush, 0, 0)
	return nil
}
