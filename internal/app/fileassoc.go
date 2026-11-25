/*
在应用更新后补注册新增的文件关联，补齐安装程序未执行的配置。
*/

package app

import "log"

// repairFileAssociations adds associations introduced after an installed app
// was set up. Automatic updates replace its executable without running the
// installer, which is otherwise what registers them.
func repairFileAssociations() {
	go func() {
		if err := registerFileAssociations(); err != nil {
			log.Printf("file associations: %v", err)
		}
	}()
}
