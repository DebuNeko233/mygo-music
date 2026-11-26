# Nyago Music

用 Go 和 [MyGo](https://github.com/egoist/mygo) 写的第三方音乐桌面客户端，原生界面，无 WebView。

本项目由 [DebuNeko233/nyago-music](https://github.com/DebuNeko233/nyago-music) 独立维护。业务音源限定为单一音源，另支持本地音乐。

<img width="1289" height="928" alt="image" src="https://github.com/user-attachments/assets/a7a738a2-fa40-4eb5-8528-d168028d96c0" />

## 安装
支持 Windows、Linux、macOS的x86和arm架构
**前往 [最新 Release](https://github.com/DebuNeko233/nyago-music/releases/latest) 按照指引下载。**

## 数据与清理

`v0.3.1` 起直接使用 Nyago Music 专用目录，不读取、复制、搬迁或删除旧版目录。首次启动使用默认设置和空缓存，旧设置、曲库索引、播放进度和缓存保留在原处。

macOS 默认目录为：

- 数据与字幕模型：`~/Library/Application Support/Nyago Music`
- 缓存与字幕：`~/Library/Caches/Nyago Music`
- 日志（如有）：`~/Library/Logs/Nyago Music`
- 下载歌曲：`~/Music/Nyago Music`

Windows/Linux 位置和开发版隔离规则见[数据目录](docs/data-lifecycle.md#应用数据目录)。自选缓存、下载目录继续由设置管理。

## 桌面体验与扩展功能

- 自定义**主题颜色**与播放页、主页面的**背景图片**，随意设置背景图片透明度
- 无边框**桌面歌词**，自定义桌面歌词样式
- **自定义快捷键**，应用内与全局都支持
- 支持**离线浏览**浏览过的的页面、图片和歌曲信息，无网也可用
- **收藏专辑**，在侧边栏浏览已收藏的专辑
- 跨设备**同步最近播放**，查看最近播放的歌曲、歌单、专辑、播客与节目，离线时显示本机缓存
- **在应用内打开分享链接**与 `orpheus://` 链接，支持歌曲、歌单、专辑、歌手、用户、播客与节目
- **搜索框支持打开分享链接**，并显示“搜索/链接”提示；搜索建议可查找并播放当前歌单中的歌曲
- **支持歌单搜索与封面卡片展示**，显示歌曲数和创建者
- 律动视图支持多种**歌词动画效果**、**火花效果**与**连续切歌优化**
- 播客字幕支持**逐句跳转**与**逐字高亮**，并支持 Silero VAD
- 播客页支持订阅、推荐与热门浏览、节目播放、点赞和评论，个人主页显示订阅的播客与关注的歌手

## 音乐功能

- 支持播放本地音乐
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
