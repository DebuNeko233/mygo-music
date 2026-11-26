# Nyago Music

用 Go 和 [MyGo](https://github.com/egoist/mygo) 写的第三方音乐桌面客户端，原生界面，无 WebView。

本项目由 [DebuNeko233/nyago-music](https://github.com/DebuNeko233/nyago-music) 独立维护，`main` 为正式开发主线。版本、更新和字幕库资源由本仓库发布，不再跟随原项目自动同步。业务音源限定为单一音源，另支持本地音频。

## 安装

独立版本从[本仓库 Releases](https://github.com/DebuNeko233/nyago-music/releases)下载。首个独立正式版本为 `v0.3.0`；旧预览版不包含本次主线改动。安装时请选择正式版本，字幕运行库资源发行不包含应用安装包。

从 `v0.3.1` 开始，应用显示为「Nyago Music」，使用下述独立目录；`v0.3.0` 及更早版本仍使用旧目录。安装包文件名前缀暂保留 `mygo-cloudmusic`。

原项目安装的版本不会自动切换到本项目：首次迁移需手动安装本仓库版本，此后使用本仓库的更新源和签名。应用标识与系统凭据服务暂时保留，因此可能继续使用现有钥匙串登录；文件数据已按应用名称隔离，但不保证两个应用并行运行。

Windows 用户从[最新版本页面](https://github.com/DebuNeko233/nyago-music/releases/latest)下载对应架构的 `setup-*.exe` 安装包。Linux 用户选择对应架构的 `.deb`、`.rpm` 或 `.tar.gz` 包。

macOS 也可以通过 [Nyago Music Homebrew tap](https://github.com/DebuNeko233/homebrew-nyago-music) 安装：

```sh
brew tap debuneko233/nyago-music
brew trust --cask debuneko233/nyago-music/nyago-music
brew install --cask debuneko233/nyago-music/nyago-music
```

Homebrew 7 需要信任第三方 cask；旧版 Homebrew 没有 `trust` 命令时跳过第二步。通过 Homebrew 更新时运行 `brew update`，再运行 `brew upgrade --cask --greedy nyago-music`。

macOS 手动安装可从 [最新版本页面](https://github.com/DebuNeko233/nyago-music/releases/latest) 下载 DMG：Apple 芯片选择 `arm64`，Intel 芯片选择 `amd64`。打开 DMG，将应用拖入「应用程序」。应用未经过 Apple 公证；若系统阻止打开，请在「系统设置 → 隐私与安全性」中允许运行。

## 数据与清理

`v0.3.1` 起直接使用 Nyago Music 专用目录，不读取、复制、搬迁或删除旧版目录。首次启动使用默认设置和空缓存，旧设置、曲库索引、播放进度和缓存保留在原处。

macOS 默认目录为：

- 数据与字幕模型：`~/Library/Application Support/Nyago Music`
- 缓存与字幕：`~/Library/Caches/Nyago Music`
- 日志（如有）：`~/Library/Logs/Nyago Music`
- 下载歌曲：`~/Music/Nyago Music`

Windows/Linux 位置和开发版隔离规则见[数据目录](docs/data-lifecycle.md#应用数据目录)。自选缓存、下载目录继续由设置管理。

普通 `brew uninstall --cask nyago-music` 保留数据；显式运行 `brew uninstall --cask --zap nyago-music` 才将上述前三个应用专用目录移至废纸篓。旧版目录、开发版目录、钥匙串、自选目录和下载歌曲不在清理范围内。

## 桌面体验与扩展功能

- 主题颜色与播放页、主页面背景图片自定义
- 播放页与主页面分别设置背景透明度，支持预加载背景图并在重启后保留主页面背景
- 无边框桌面歌词：字体、颜色与行数设置，拖动调整位置和大小，悬停显示控制，锁定后仅解锁按钮接收鼠标操作
- 桌面歌词透明窗口，无系统边框和阴影，支持文字描边、逐字高亮与水平对齐设置
- 自定义应用内与全局快捷键，支持播放、切歌、桌面歌词和音量调节
- 音量控制与系统音量同步
- 离线浏览已缓存的页面、图片和歌曲信息，网络恢复后重试加载
- 页面、封面与背景图缓存带内存上限，统一缓存目录并支持旧缓存迁移，封面加载与刷新更稳定
- 在歌单中定位正在播放的歌曲，支持加载完整歌单并从随机歌曲开始播放
- 收藏专辑，在侧边栏浏览已收藏的专辑
- 跨设备同步最近播放，查看最近播放的歌曲、歌单、专辑、播客与节目，离线时显示本机缓存
- 在应用内打开网易云分享链接与 `orpheus://` 链接，支持歌曲、歌单、专辑、歌手、用户、播客与节目
- 搜索框支持打开分享链接，并显示“搜索/链接”提示；搜索建议可查找并播放当前歌单中的歌曲
- 支持歌单搜索与封面卡片展示，显示歌曲数和创建者
- 记录播放曲目、播放时长与结束原因
- 支持网络状态监测，断网时显示重新连接控件，网络恢复后自动刷新
- 播放失败时区分 VIP/付费不可用与网络下载失败：支持重试、断点续播、提前预加载下一首，并避免因临时错误跳过歌曲
- 支持播放进度后台保存，重新打开应用后恢复队列与进度
- 优化 MP3/FLAC 播放、频谱与音频淡入淡出，降低播放和绘制开销
- 正在播放页的待播清单、选歌与评论标签可用快捷键切换；选歌支持拖动、甩动和触控板浏览
- 律动视图支持多种歌词动画效果、火花效果与连续切歌优化
- 歌单歌曲显示专辑封面，页面返回后复用已缓存封面并减少闪烁
- 播客字幕支持逐句跳转与逐字高亮，并支持 Silero VAD
- 播客页支持订阅、推荐与热门浏览、节目播放、点赞和评论，个人主页显示订阅的播客与关注的歌手
- 账号菜单支持“我的主页”，登录态支持网络异常重试
- macOS 支持刘海播放器、菜单栏与通知区域托盘图标；窗口打开时可通过 Dock 和 Command-Tab 切换

## 音乐功能

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

工具链版本以 `go.mod` 为准，当前要求 Go 1.27.4。

```bash
go tool mygo dev     # 开发模式运行
go tool mygo build   # 打包到 build/
go test ./...        # 运行测试
```

参与开发请从 [贡献指南](CONTRIBUTING.md) 和 [开发文档](docs/README.md) 开始。文档包含架构、功能与代码索引、新增功能流程、测试、代码规范及文档维护规则。

## 致谢

本项目基于 [zhangnjsd/mygo-music](https://github.com/zhangnjsd/mygo-music) 演进，保留历史提交、许可证与原作者贡献。独立维护不改变已有开源许可。

- [MyGo](https://github.com/egoist/mygo)：原生 UI 与应用框架
- [NeteaseCloudMusicApi](https://github.com/Binaryify/NeteaseCloudMusicApi)：eapi 加密与登录流程的参考
- [folia-major](https://github.com/chthollyphile/folia-major)：歌词动画效果的参考
- [sherpa-onnx](https://github.com/k2-fsa/sherpa-onnx)、[Qwen3-ASR](https://github.com/QwenLM/Qwen3-ASR) 与 [Silero VAD](https://github.com/snakers4/silero-vad)：本机语音识别

本项目与官方无关，音乐版权归各权利人所有。

## 许可证

[AGPL-3.0](LICENSE)
