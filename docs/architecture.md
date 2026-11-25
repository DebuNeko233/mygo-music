# 项目架构

`main.go` 启动 `internal/app`。App 负责组装服务和功能模型、连接桌面视图、协调跨功能操作，以及管理启动、窗口和退出。

| 包 | 职责 |
| --- | --- |
| `internal/app` | 依赖接线、跨功能命令、账号生命周期、UI 线程分发和资源释放 |
| `internal/desktop/artwork`、`shortcuts`、`lifetime` | 图片解码/缓存/封面匹配、快捷键定义与绑定规则、桌面订阅释放 |
| `internal/sessionstore` | 播放队列快照、进度文件和后台写入 |
| `internal/desktop/view` | 布局、控件、焦点、滚动、动画和图形状态 |
| `internal/viewmodel/<feature>` | 功能数据、命令、加载、错误、重试和请求代次 |
| `internal/library`、`internal/localcatalog` | 共享音乐库数据与本地曲库操作 |
| `internal/player`、`netease`、`download`、`lyrics`、`speech` | 播放、API、下载、歌词与转写能力 |
| `internal/navigation`、`preferences`、`userdata` 及缓存包 | 导航历史、设置持久化、存储和缓存 |

简单视图平放在 `desktop/view`，包含多个源码文件的功能使用子包。按职责和依赖边界拆包，不要求目录对称。一个职责只有少量代码时，合并到已有文件；只有独立能力需要复用、隔离依赖或单独测试时才建包。

App 中搜索协调集中在 `search.go`，播放入口和来源在 `play_commands.go`，收藏与点赞在 `library_commands.go`。直接调用已归属模块的能力，避免仅换名转发的包装函数。

## 依赖方向

- `app` 可以依赖视图、ViewModel 和服务，其他包不能反向依赖 `app`。需要应用能力时，由 App 注入必要的回调或小接口。
- 视图可以读取 ViewModel，并使用底层能力。ViewModel 不导入 MyGo 或桌面视图包。
- 功能 ViewModel 可以使用 `viewmodel` 根包中的公共定义、自身实现子包和底层服务，不导入其他功能的 ViewModel。跨功能协调放在 `app`。
- `desktop/artwork`、`shortcuts`、`lifetime` 可以依赖 MyGo，但不依赖视图或 ViewModel。界面配色、展示文案和格式化留在视图包。
- 底层服务不依赖桌面视图或 ViewModel。已有具体服务和数据类型可以继续使用；只有调用方需要更窄的能力或第二种实现时，才提取接口。

`go test ./internal/architecture` 检查所有平台的生产代码导入，包括当前系统构建标签排除的文件。测试文件不受此限制，便于集成测试组合多个层次。现有 CI 的 internal 包测试会自动运行此检查。

## 视图接线和播放命令

`newApp` 在渲染前调用一次 `bindDesktopViews`。接线分为 `bindings_shared.go`、`bindings_browse.go`、`bindings_library.go`、`bindings_settings.go` 和 `bindings_playback.go`。

共享页面的渲染回调绑定到 `desktopViews` 中地址稳定的指针接收者，所有接线完成后才执行。不要在逐帧构建时重复组装，也不要把这些接收者改成复制视图值。

`play_commands.go` 保留不同播放入口的语义：

- 专辑和歌单的播放操作关闭随机模式，从首个可播放曲目开始。
- 歌手页设置所选随机模式，但仍从首个可播放曲目开始，保持现有控件行为。
- 每日推荐和曲目列表点选歌曲时，保留当前随机模式并从指定索引开始。网盘曲目列表还通过 `playTrackQueue` 把队列关联到当前加载任务。
- 整个列表的随机播放由播放器选择起始曲目。最近播放保留原有不附加页面来源的路径。

页面来源在命令执行时读取，不在接线时捕获。Radio 通过 App 注入的查询读取音乐库，不持有 Library ViewModel。每次心动模式补歌都在所属线程上读取最新的“我喜欢”歌单标识，再启动 I/O。

## 状态与生命周期

命令和发布的功能状态由同一个线程管理。视图只读借用已发布的集合；修改集合通过功能命令完成。表单可以绑定持久的输入状态，但控件、句柄、滚动位置和动画资源属于视图。

后台任务捕获请求输入，完成 I/O 后通过注入的分发器发布结果。账号变化、重置和退出必须取消或拒绝过期结果。Radio 借用的缓存歌单在请求期间保持只读；集合修改应发布替代值，不能修改后台任务正在读取的快照。

原生窗口和资源由 App 中的所有者释放。视图不能保留单次构建的 `ui.Element`，也不能把 `ui.Context` 交给后台任务。移动异步代码时保留现有 `postResource` 的所有权转移和丢弃释放路径。

图片缓存的主线程分发和结果丢弃释放由 App 注入；缓存内部管理并发槽位、内存预算、失效和重试，App 保留曲目来源选择与预取策略。会话存储保留原来的 `session.json` 与 `session-progress.json` 格式，只有 token 匹配的进度文件才能覆盖快照。App 决定保存时机、恢复播放器和退出等待，`sessionstore` 负责快照复制及串行写盘。

## 测试归属

图片缓存、解码、快捷键和会话文件行为放在各自能力包旁；纯功能行为放在对应 ViewModel 旁；能独立构建的交互放在视图旁；跨功能行为保留在 App。登录退出、网盘队列补全、窗口关闭和账号作用域回调需要应用集成测试，不为匹配目录而移动这些测试。

迁移测试按函数判断归属，不按文件整批搬走。字体选择、背景、封面手势、歌词绘制和字幕列表测试直接构建 playback 视图；设置保存、主窗口布局、跨页面切换和预取接线仍由 App 集成测试覆盖。各包的 `helpers_test.go` 只创建所需状态和明确的回调，不复制整个 App 接线。

`internal/testutil` 只保存跨包共用的拖拽、动画稳定和帧数统计工具，禁止生产代码导入。包内专用夹具留在原包，图片和音频样本放在所属包的 `testdata/`。

修改接线或命令后，先运行相关行为测试，再运行 `CGO_ENABLED=0 go test ./...` 和 `go tool mygo vet .`。新 UI 测试按 `AGENTS.md` 连续运行五次；修改共享状态时运行 race 检查。涉及视觉或平台行为的变更还需对应的截图或原生验证。

## App 文件归属清单

本轮逐项检查生产文件，按职责迁出或合并，未新增包层级。App 由 63 个生产 Go 文件、5582 行整理为 55 个文件、5180 行（含注释和平台文件）。文件数量不是验收条件：涉及原生资源、账号生命周期或多个功能的协调应继续留在 App。下面覆盖当前全部生产文件；测试文件另按所测行为归属。

| 文件（相对 `internal/app`） | 处理 | 留在 App 的职责与迁出边界 |
| --- | --- | --- |
| `app.go` | 保留 | 组合根状态：持有各功能模型、视图和应用资源；不承载功能算法。 |
| `runtime.go` | 合并后保留 | 应用构造、原生事件注册、启动时文件/链接意图与会话恢复的协调；吸收 startup.go。 |
| `shutdown.go` | 保留 | 停任务、关窗口、释放资源、完成持久化的应用退出顺序。 |
| `dispatch.go` | 保留 | 向 UI 线程发布结果，以及退出时拒绝更新并释放资源。 |
| `windows.go` | 合并后保留 | 主窗口和辅助播放器的创建、激活、关闭及同步；吸收 window_sync.go。 |
| `settings_window.go` | 保留 | 设置窗口生命周期，将设置变动应用到主窗口、刘海和歌词窗口。 |
| `view.go` | 保留 | 主窗口组合、路由分发、页面进入/离开时加载与清理；实际页面布局在视图包。 |
| `view_state.go`、`view_bindings.go` | 保留 | App 所有的主/设置窗口状态与地址稳定的视图集合、接线入口。 |
| `bindings_shared.go` | 合并后保留 | 公共页面、曲目列表、图片和点赞组件的接线；剪贴板平台适配器由 clipboard.go 并入。 |
| `bindings_browse.go`、`bindings_library.go`、`bindings_playback.go`、`bindings_settings.go` | 保留 | 按功能组接线；回调读取当前账号/设置，而不是在绑定时冻结它们。 |
| `account.go` | 保留 | 账号切换时重置其他功能、重新绑定歌词来源、停止下载与上报、恢复离线身份。 |
| `appearance.go` | 保留 | 跨首页/播放页的背景资源生命周期、异步读取的过期检查和应用主题；图片解码已在 artwork。 |
| `settings.go` | 部分迁出后保留 | 设置路径、原生外观适配和 preferences 类型转换；绑定规则归 shortcuts，展示选项归 settings 视图。 |
| `hotkeys.go` | 部分迁出后保留 | 动作 ID 到应用命令的映射、原生注册释放、窗口焦点策略、保存和刷新；定义、查找、冲突和恢复规则归 shortcuts。 |
| `menu.go`、`tray.go` | 保留 | 原生菜单、托盘、Dock/激活策略以及系统图标选择；组合多个功能的命令。 |
| `updates.go` | 保留 | 把原生更新器接入窗口、设置和应用退出。 |
| `fileassoc.go`、`fileassoc_other.go`、`fileassoc_windows.go` | 保留 | 应用启动时修复安装路径与操作系统文件关联；平台构建标签保持分文件。 |
| `cache.go` | 部分迁出后保留 | 创建各缓存、账号 scope/epoch、接入播放器、清理和统计结果发布；歌词读写/过期刷新归 lyrics，容量展示归 settings 视图。 |
| `cache_directory.go` | 保留 | 目录选择、迁移恢复、设置提交、播放器/缓存切换与退出协调；搬运事务在 cachemove 和 settings ViewModel。 |
| `playlist_cache.go` | 合并后保留 | 账号缓存写入预约、异步写入和读取等待、按可见页面裁剪；从 offline.go 接收持久化入口，快照复制与脏标记聚合归 Library。 |
| `recovery.go` | 收拢后保留 | 原 offline.go 中的可见页面恢复、网络/账号/播放器/图片联合重试与定时器生命周期。缓存创建和快照算法已移出。 |
| `network.go` | 保留 | 系统连通性通知驱动 API、播放器、封面和页面恢复。 |
| `track_artwork.go` | 部分迁出后保留 | 当前曲目、账号缓存键、嵌入封面选择、快速切歌和过期结果发布；标签归一化、文件名标签和候选匹配归 artwork。 |
| `playback_artwork.go` | 保留 | 从当前播放、本地曲库、下载缓存和图片缓存选择封面，协调不同窗口尺寸和预取。 |
| `play_commands.go` | 合并后保留 | 页面来源与播放队列、网盘队列追踪，以及播放器/系统混音器接线；吸收 volume.go。 |
| `play_report.go` | 保留 | 实际播放事件与当前登录会话的关联、每次登录有序发送、退出时收尾；协议请求在 netease。 |
| `session.go` | 保留 | 会话保存时机、恢复播放器、窗口设置与启动意图；文件格式、快照和写入排序在 sessionstore。 |
| `pickup.go` | 部分迁出后保留 | 登录后请求最近播放、确认空队列、恢复播放器和展示提示；最近一段收听记录的筛选归 Playback ViewModel。 |
| `library_commands.go`、`library_refresh.go` | 保留 | 登录保护、账号代次、可见歌单和播放/导航回调注入；增删改、刷新及数据状态归 Library ViewModel。 |
| `search.go` | 部分迁出后保留 | 搜索与链接打开、当前页曲目来源、导航和播放的协调及设置保存；历史去重归 Search ViewModel。 |
| `details.go` | 合并后保留 | 歌手/专辑/用户页面的账号作用域接线与打开主窗口；吸收 artist_navigation.go。 |
| `home.go` | 合并后保留 | 首页服务接线和账号状态下的页面组合；合并 home_cache.go 与 view_home.go。 |
| `cloud.go` | 合并后保留 | 账号绑定的网盘服务、播放队列联动、当前歌曲定位与页面滚动；吸收 cloud_locate.go。 |
| `music_links.go` | 保留 | 音乐链接的系统协议注册、启动意图、账号作用域与播放/导航协调；解析由 netease 和 Links ViewModel 完成。 |
| `recent.go`、`podcast.go`、`radio.go`、`comments.go` | 保留 | 各功能与账号、播放器、导航/滚动状态的适配；功能状态与业务规则由对应 ViewModel 管理。 |
| `local_music.go` | 保留 | 本地曲库初始化、播放器的本地文件解析和功能服务接线。 |
| `local_open.go` | 保留 | 系统打开文件事件/文件菜单与启动、资源释放和临时播放的协调。 |
| `local_lyrics.go` | 保留 | 当前歌曲、转写/歌词状态、搜索对话框和本地歌词保存/重载的接线。 |
| `local_window.go` | 保留 | 本地目录窗口、原生文件选择以及本地曲目与播放队列菜单的组合。 |
| `download.go` | 合并后保留 | 从应用设置组装下载选项、原生目录选择、账号退出与下载服务接线；吸收 download_dialog.go，显示选项归 settings 视图。 |
| `captions.go` | 保留 | 播放器、模型安装、转写服务、应用缓存路径和设置窗口之间的适配。 |
| `debug_shots.go`、`cputime_unix.go`、`cputime_windows.go` | 保留 | 通过 NM_SHOTS/NM_SCENE 驱动整个应用场景的开发截图与性能采样；依赖完整应用，不放入单个功能。 |

## 规则提取后的接口约束

- `artwork.FindTrack` 接收曲目和搜索函数，负责标签与时长匹配；`FilenameTags` 仅补充缺失标签。它们不读取播放器、账号或网络状态。App 仍选择缓存 scope，并在曲目变化后拒绝过期封面发布。
- `shortcuts` 保存动作定义和两套绑定规则。App 的动作映射只负责执行，绑定变更后仍由 App 保存设置、重新注册全局快捷键和刷新窗口。设置视图直接持有自己的音质、文件名与容量展示选项。
- `lyrics.Cache` 捕获 Store、Scope 和 Fetch，负责读取、七天过期刷新及写入。`Load` 在工作线程运行；`Publish` 由 App 分发回 UI 线程并检查账号 scope 与缓存 epoch。播放器继续负责歌曲请求有效性。
- `Library.TakeSnapshots` 在所属线程聚合脏标记，等全部编辑成功或回滚后才返回复制的数据。App 在同一线程预约写入，再将编码交给工作线程；歌单读取仍等待最近一次已接受的快照写入。账号有效性和缓存 epoch 不下放给纯快照构造。
- `Playback.LastStretch` 选择可续播的一段收听记录；`Search.RememberQuery` 去重并限制历史长度。App 负责请求、播放和设置持久化。

本轮将封面匹配/裁剪、网盘去重、播客去重、字体解析、时间格式、续播筛选、设置持久化和歌词磁盘读取的测试迁到各自所有者。快照隔离、重叠编辑、歌词后台刷新和快捷键冲突/恢复在能力包直接测试；账号切换与缓存清理拒绝歌词回调、全局快捷键注册、跨页面搜索和播放仍在 App 测试。迁移保留原有断言；混合测试中的显示格式断言拆到视图测试，不让 ViewModel 测试依赖桌面代码。

## 上游 0.2.16 同步

同步 `zhangnjsd/mygo-music` 的 `8b41312649a6963ddfe94dd8b5f8d69af7209caf`，共同基线为 `163f1dccfed37893d63c3044a7e305eeb74295f7`。合并提交保留上游父提交，后续同步可以继续沿用 Git 的共同基线；上游已撤回的歌词拖动改动不重复引入。

| 上游改动 | 当前架构中的位置 |
| --- | --- |
| 缺少钥匙串的错误识别与帮助内容 | `netease/auth.go`、`netease/keyring_help.md` |
| 登录持久化失败状态与账号代次检查 | `viewmodel/account` |
| 登录设置帮助、复制与重试按钮 | `desktop/view/account.go`；App 仅注入剪贴板与通知回调 |
| Wayland 绑定反馈与 Hyprland 空触发器处理 | `app/hotkeys.go`；定义与绑定规则仍属于 `desktop/shortcuts` |
| Linux 歌词窗口隐藏菜单、持续提交解锁点击区域 | `desktop/view/playback/desklyrics.go` |
| 官方 MyGo 的歌词图层接口 | `desktop/view/playback/lyric_edges.go` 使用 `Draw` |

依赖与上游一致，使用官方 `github.com/egoist/mygo v0.10.0`，取消此前的 fork replace。快捷键注册附带代次：重新绑定、注销或退出后，旧 portal 结果不能覆盖当前错误状态。新增错误分类和登录状态测试分别放在 API 与 Account ViewModel 旁；界面交互放在视图旁，剪贴板接线和系统快捷键注册保留 App 集成测试。
