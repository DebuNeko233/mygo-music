# 项目架构

`main.go` 启动 `internal/app`。App 负责组装服务和功能模型、连接桌面视图、协调跨功能操作，以及管理启动、窗口和退出。

| 包 | 职责 |
| --- | --- |
| `internal/app` | 依赖接线、跨功能命令、账号生命周期、UI 线程分发和资源释放 |
| `internal/desktop/artwork`、`shortcuts`、`lifetime` | 图片解码与缓存、快捷键解析、桌面订阅释放 |
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
