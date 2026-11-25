# 功能与代码索引

功能手册同时说明用户入口、代码所有者、必须保留的行为和验证位置。产品级说明仍可放根 README；这里服务于实现和审查。新增功能先读[扩展流程](../adding-features.md)，新增文档按[文档维护](../documentation.md)。

| 功能 | 手册 | 主要代码 |
| --- | --- | --- |
| 扫码登录、凭据、离线身份、退出 | [浏览与资料库](browsing-library.md) | `netease/auth`、`viewmodel/account`、App account |
| 首页、每日推荐、排行榜 | [浏览与资料库](browsing-library.md) | `viewmodel/home`、view home/daily/cards |
| 搜索、联想、历史、分享链接 | [浏览与资料库](browsing-library.md) | `viewmodel/search`、`viewmodel/links`、App search/music_links |
| 歌单、喜欢、专辑收藏、侧栏集合 | [浏览与资料库](browsing-library.md) | `library`、`viewmodel/library`、view library |
| 歌手、专辑、用户详情 | [浏览与资料库](browsing-library.md) | 对应 ViewModel、App details、功能视图 |
| 音乐网盘、最近播放、续播 | [浏览与资料库](browsing-library.md) | cloud/recent/playback 模型与 App 适配 |
| 播客、节目、私人 FM、心动、评论 | [浏览与资料库](browsing-library.md) | podcast/radio/comments 模型与 netease |
| 队列、模式、音质、预取、跳转 | [播放与音频](playback.md) | `player`、App play_commands |
| 响度均衡、音量、频谱、播放记录、恢复 | [播放与音频](playback.md) | player、mixvol、sessionstore、App play_report/session |
| 当前歌曲定位、虚拟列表 | [播放与音频](playback.md) | view tracklist、App cloud/local 协调 |
| 歌词来源、翻译、署名、逐字与舞台 | [歌词与字幕](lyrics-captions.md) | lyrics、view playback |
| 桌面歌词、字体、频谱背景、间奏热评 | [歌词与字幕](lyrics-captions.md) | view playback、App settings |
| 播客转写、模型下载和字幕保存 | [歌词与字幕](lyrics-captions.md) | speech、captionstore、viewmodel/captions |
| 本地扫描、重定位、临时打开、歌词匹配 | [本地音乐](local-music.md) | localcatalog、localmedia、viewmodel/local |
| 本地与网盘封面匹配 | [本地音乐](local-music.md) | desktop/artwork、App track_artwork |
| 歌曲导出、标签、下载任务 | [下载与缓存](download-cache.md) | download、audiotag、viewmodel/downloads |
| 音频/图片/页面缓存、离线、目录迁移 | [下载与缓存](download-cache.md) | player/cache、diskcache、cachemove、pagecache |
| 主题、背景、设置搜索、侧栏开关 | [桌面与设置](desktop-settings.md) | preferences、view settings/shell、App appearance |
| 快捷键、托盘、原生菜单、刘海、更新 | [桌面与设置](desktop-settings.md) | desktop/shortcuts、App、view playback |

路径缩写均相对 `internal/`，具体源码与测试链接在各手册中。不要据文件名将整项功能误认为只有一个所有者：例如播客 API、订阅状态、字幕引擎与界面分别位于不同层。

## 功能文档共同格式

每项至少回答：入口和范围、数据/命令的所有者、正常与异常流程、持久化和兼容、平台差异、已有回归、以后扩展要同步的位置。纯粹的控件列表不构成功能手册；一次性实现进度不进入这里。
