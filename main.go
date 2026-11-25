/*
启动桌面应用，并将初始化或运行失败写入日志。
*/

package main

import (
	"log"
	"netease-music-mygo/internal/app"
)

func main() {
	if err := app.Run(); err != nil {
		log.Fatal(err)
	}
}
