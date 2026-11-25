/*
实现本地曲库目录增删、歌曲移除和文件重定位。
*/

package localcatalog

import (
	"fmt"
	"os"
	"slices"
)

func AddFolders(catalog *Catalog, present map[string]struct{}, paths []string) (bool, string) {
	var changed bool
	var invalid int
	for _, path := range paths {
		path, err := filePath(path)
		if err != nil {
			invalid++
			continue
		}
		if st, err := os.Stat(path); err != nil || !st.IsDir() {
			invalid++
			continue
		}
		if !slices.Contains(catalog.Folders, path) {
			catalog.Folders = append(catalog.Folders, path)
			changed = true
		}
	}
	if len(catalog.Folders) == 0 {
		return false, "请选择包含可播放音频的文件夹"
	}
	scanned, status := Scan(catalog, present)
	if invalid != 0 {
		status += fmt.Sprintf("，%d 个所选项目无法作为文件夹打开", invalid)
	}
	return changed || scanned, status
}

func RemoveFolder(catalog *Catalog, path string) (bool, string) {
	catalog.Folders = slices.DeleteFunc(catalog.Folders, func(folder string) bool { return folder == path })
	catalog.Songs = slices.DeleteFunc(catalog.Songs, func(song Song) bool {
		return song.Scanned && !pathInFolders(song.Path, catalog.Folders)
	})
	return true, "已移除文件夹"
}

func Relink(catalog *Catalog, present map[string]struct{}, id int64, path string) (bool, string) {
	path, err := filePath(path)
	if err != nil {
		return false, "无法打开所选文件"
	}
	if slices.ContainsFunc(catalog.Songs, func(song Song) bool { return song.Path == path && song.Track.LocalID != id }) {
		return false, "所选文件已在曲库中"
	}
	i := slices.IndexFunc(catalog.Songs, func(song Song) bool { return song.Track.LocalID == id })
	if i < 0 {
		return false, "该歌曲已从曲库移除"
	}
	song, ok := Inspect(path)
	if !ok {
		return false, "无法打开所选音频"
	}
	song.Track.LocalID = id
	song.Scanned = catalog.Songs[i].Scanned && pathInFolders(path, catalog.Folders)
	catalog.Songs[i] = song
	present[path] = struct{}{}
	return true, "已重新定位文件"
}

func RemoveSong(catalog *Catalog, id int64) (bool, string) {
	catalog.Songs = slices.DeleteFunc(catalog.Songs, func(song Song) bool { return song.Track.LocalID == id })
	return true, "已从曲库移除"
}
