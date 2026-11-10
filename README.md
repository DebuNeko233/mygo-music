# MyGO Rev.

用 Go 和 [MyGo](https://github.com/egoist/mygo) 写的第三方音乐桌面客户端，原生界面，无 WebView。

本仓库不考虑加入其它平台作为音源。

## 安装

Windows 上为当前用户安装，再次运行即升级，会先退出正在运行的版本：

```powershell
irm https://github.com/zhangnjsd/mygo-music/releases/latest/download/install.ps1 | iex
```

Linux 上装在 ~/.local，不需要 root：

```sh
curl -fsSL https://github.com/zhangnjsd/mygo-music/releases/latest/download/install.sh | sh
```

macOS 上从 [最新版本页面](https://github.com/zhangnjsd/mygo-music/releases/latest) 下载 DMG：Apple 芯片选择 `arm64`，Intel 芯片选择 `amd64`。打开 DMG，将应用拖入「应用程序」。应用未经过 Apple 公证；若系统阻止打开，请在「系统设置 → 隐私与安全性」中允许运行。

## 本分支新增功能

- 主题颜色与播放页、主页面背景图片自定义
- 无边框桌面歌词：字体、颜色与行数设置，拖动调整位置和大小，悬停显示控制，锁定后仅解锁按钮接收鼠标操作
- 桌面歌词透明窗口，无系统边框和阴影
- 自定义应用内与全局快捷键，支持播放、切歌、桌面歌词和音量调节
- 音量控制与系统音量同步
- 离线浏览已缓存的页面、图片和歌曲信息，网络恢复后重试加载
- 在歌单中定位正在播放的歌曲
- 收藏专辑，在侧边栏浏览已收藏的专辑
- 跨设备同步最近播放，查看最近播放的歌曲、歌单、专辑、播客与节目，离线时显示本机缓存
- 在应用内打开网易云分享链接与 `orpheus://` 链接，支持歌曲、歌单、专辑、歌手、用户、播客与节目

## 上游功能

- 扫码登录，登录态保存在系统凭据存储中
- 每日推荐、推荐歌单、排行榜、我的歌单
- 搜索单曲、歌手、专辑、用户与播客，输入时显示搜索建议，保留搜索历史
- 歌手页的热门与最新歌曲、专辑与动态，关注歌手；专辑页与用户页
- 播客：浏览与收听节目，订阅播客；在本机把节目转写成字幕（Qwen3-ASR），像歌词一样随播放滚动
- 私人 FM（推荐 / 熟悉 / 探索）与心动模式
- 边下边播，支持标准 / 较高 / 极高 / 无损（FLAC）/ Hi-Res 音质
- 听过的歌曲缓存在本机，再次播放无需等待
- 音量均衡，暂停与继续时淡出淡入，拖动进度无杂音
- 退出后再打开，从上次的播放队列与进度继续
- 逐字歌词与翻译，正在播放页的「律动」视图与歌曲评论
- 刘海播放器：在屏幕顶部刘海处显示迷你播放器，悬停展开（在设置中开启）
- 喜欢歌曲、播放队列、随机与循环播放
- 浅色 / 深色 / 跟随系统

## 构建

需要 Go 1.27+。

```bash
go tool mygo dev     # 开发模式运行
go tool mygo build   # 打包到 build/
go test ./...        # 运行测试
```

## 致谢

- [MyGo](https://github.com/egoist/mygo)：原生 UI 与应用框架
- [NeteaseCloudMusicApi](https://github.com/Binaryify/NeteaseCloudMusicApi)：eapi 加密与登录流程的参考
- [folia-major](https://github.com/chthollyphile/folia-major)：歌词动画效果的参考
- [sherpa-onnx](https://github.com/k2-fsa/sherpa-onnx)、[Qwen3-ASR](https://github.com/QwenLM/Qwen3-ASR) 与 [Silero VAD](https://github.com/snakers4/silero-vad)：本机语音识别

本项目与官方无关，音乐版权归各权利人所有。

## 许可证

[AGPL-3.0](LICENSE)
