# 播放、队列与音频

本章约束音乐播放链路。下载成文件见[下载与缓存](download-cache.md)，歌词和字幕见[歌词与字幕](lyrics-captions.md)。播放栏、正在播放页、队列、页面播放按钮、系统媒体控制都应汇入同一组播放命令。

## 代码归属

| 所有者 | 职责 |
| --- | --- |
| [player](../../internal/features/player) | 队列、模式、播放状态、解码、流式读取、预取、音频缓存和临时播放 |
| [App 播放命令](../../internal/app/play_commands.go) | 页面播放意图、数据源身份、跨功能协调 |
| [Playback ViewModel](../../internal/ui/viewmodel/playback) | 播放相关的界面状态与操作语义 |
| [播放视图](../../internal/ui/view/playback) | 控件、手势、进度、封面、动画、正在播放及舞台布局 |
| [tracklist](../../internal/ui/view/tracklist) | 通用歌曲行、虚拟列表和当前歌曲定位 |
| [player/session](../../internal/features/player/session) | 会话与进度快照的存储格式和串行写入 |
| [App 上报](../../internal/app/play_report.go) | 捕获实际播放会话、账号隔离、网易云播放记录上报 |
| [mixvol](../../internal/features/mixvol) | 平台应用音量能力 |

新增播放入口先复用已有命令。不能在视图复制一套队列、URL 获取或播放器状态机，也不能把 App 页面对象传进 player。

## macOS Dock 菜单

Dock 图标右键菜单提供重复播放（关闭、列表循环、单曲循环）、随机播放（关闭、开启）、播放/暂停、下一首和上一首。菜单由 App 在播放器初始化后注册，独立于窗口和托盘菜单；退出时解除注册。系统自带的窗口、选项和退出项目继续由 macOS 提供。

菜单直接读取 Player 的队列、播放意图和模式，通过 Changed 事件更新有变化的原生属性，不轮询或按进度重建菜单。加载中仍可暂停/恢复；空队列禁用播放和切歌。手动上一首/下一首沿用现有队列循环、电台续播和临时文件结束语义，包括单曲队列。仅菜单栏模式且无窗口时 Dock 图标按原设置隐藏。

显式模式选择使用 `SetRepeat` / `SetShuffle`，重复选择当前模式不重排队列或重新开始播放；界面上的循环/随机按钮也通过这些命令通知状态变化。回归见 [Dock 接线测试](../../internal/app/dock_menu_test.go)和 [模式测试](../../internal/features/player/modes_test.go)。

## Windows 任务栏与 Linux 启动器

Windows 任务栏右键 Jump List 和支持 Desktop Actions 的 Linux 启动器按现有 macOS Dock 的功能顺序排列：循环模式、随机播放、播放/暂停、下一首、上一首。随机模式单击切换开关，循环模式按关闭、列表循环、单曲循环轮换。Windows 的模式任务文字显示当前状态，例如“随机播放：开启”“循环：单曲循环”；Linux 保留“切换随机播放”“切换循环模式”固定按钮。两平台均不添加勾选或子菜单，原有托盘菜单继续可用。

任务使用 `--playback-action=<action>` 启动本应用。已运行时通过单实例事件执行，控制播放不主动弹出主窗口；未运行时正常启动应用，等账号和播放会话恢复后按点击顺序执行，每次点击只执行一次。连续控制命令不会合并掉切换次数；与文件/音乐链接共享启动意图的顺序，较新的文件/链接替代旧命令批次，新的命令也可替代旧文件/链接。空队列的播放和切歌仍为空操作。任务只接受一个已知动作参数，不解释任意命令或参数组合。

Windows 在启动时注册 Jump List，会话恢复、界面和系统控制引起的模式变化都通过 Player Changed 通知更新文字。App 只保存上次尝试提交的展示文字，不复制播放模式；文字不变的进度、切歌、播放/暂停通知不写任务列表。提交失败记录日志，同一文字不按进度重试，下次模式变化重新提交完整列表。正常退出时恢复通用命令文字，避免系统保留过期运行状态；异常终止或退出提交失败时可能保留最后一次文字，下次启动重新同步。Explorer 展示刷新需实机确认，不保证已展开的菜单立即变化。

Linux 的动作由 `mygo.json` 的 `linux.actions` 在打包时生成，随桌面入口进入 DEB、RPM、PKGBUILD、归档安装和更新，使用实际安装目录里的可执行文件，保留 MIME 与 URL 关联；应用运行时不另建用户级启动器或改写动作文字。具体动作展示取决于桌面环境。

回归见 [启动器命令、恢复顺序与打包配置接线](../../internal/app/launcher_tasks_test.go)、[Windows 模式文字、去重与退出](../../internal/app/launcher_menu_test.go)和 [Linux 安装包验证](../../scripts/check_linux_packages.sh)。Windows Explorer 和 Linux 各桌面环境的原生交互需分别实机验证，交叉编译不能替代。

## 页面播放语义

“播放这个页面”“从第 N 首播放”“随机播放整个列表”“下一首播放”和“临时打开文件”不是相同操作。调用前明确是否替换队列、是否保留随机模式、起始位置及数据源归属。

- 页面普通播放从第一个可播放条目开始；不可播放条目不能把起点变成无效索引。
- 明确点选一首时保留选中的起点；不能因为已有随机模式而换成另一首。
- 整表随机播放允许随机起点，与“选中某首后按随机模式继续”分开。
- 歌手页按页面命令选择模式；最近播放不应意外绑定另一个页面的数据源。
- 网盘等有后续分页的队列必须保留其来源身份。继续加载只能补充仍属于该来源的队列。
- 临时本地播放遵循[本地音乐](local-music.md)的队列保留规则。

队列身份不能只用歌曲 ID：重复歌曲是多个条目，本地文件可能没有远端 ID。删除、重排、文件重定位后必须保持当前项、历史和随机遍历关系一致。

回归优先看 [play_commands_test.go](../../internal/app/play_commands_test.go)、[shuffle_backport_test.go](../../internal/features/player/shuffle_backport_test.go)、[queue_completion_test.go](../../internal/features/player/queue_completion_test.go)、[navigation_direction_test.go](../../internal/features/player/navigation_direction_test.go)。

## 解码和流式读取

网络、本地文件、内存缓存和磁盘缓存共用格式与解码策略。按实际字节识别格式，不能相信 URL 后缀或文件扩展名；同一格式在四种来源下都要能正确读、跳转和结束。

选曲状态在异步打开音频前通知所有者。通知回调若关闭播放器或替换选择，旧加载须立即释放其预备解码器和预取资源，不能继续启动后台打开；相关生命周期回归见 [player/lifecycle_test.go](../../internal/features/player/lifecycle_test.go)。

当前格式路径包括 MP3、FLAC、WAV、Ogg Vorbis、AAC 与 MP4 家族；HE-AAC 使用相应后备解码路径。修改格式识别、标签或容器支持时，联合检查 `formats.go`、`aac.go`、`mp4.go`、`flac.go`，不能只修一个入口。

流式读取的约束：

1. 下载区间保持原始文件偏移。未下载的洞应等待数据或返回明确错误，不能当作零字节音频。
2. Seek 更新读取优先级；新请求不能无限等待已过期的远端区间。
3. 未知长度与完整下载是不同状态。不能因暂时读不到数据就发布完整缓存。
4. 磁盘写入失败时按现有内存后备策略处理；关闭或落盘失败不能产生“可复用完整文件”。
5. 解码器、底层 reader、临时文件各有明确所有者。换歌、预加载替换、失败和取消均释放。
6. MP4 工厂的串行保护不能覆盖等待分片数据的整个生命周期，否则一个慢源会阻塞其他播放。

相关回归集中在 player 的 `unified_formats_test.go`、`tagged_formats_test.go`、`local_formats_test.go`、`local_aac_test.go`、`local_mp4_test.go`、`download_reader_test.go`、`streaming_boundaries_test.go` 和 `lifecycle_test.go`。保持小型格式夹具，不把用户音乐提交进仓库。

## 缓冲、失败和离线

界面时间来自已确认的播放位置；暂停、缓冲、拖动预览和实际 Seek 必须区分。进度条松开才执行的操作不能在每次绘制时反复提交。音频请求尚未完成时，旧歌曲的结果不能替换新选择。

每次音频加载的取消上下文由 Player 持有，覆盖 URL 查询、下载、解码初始化和结果投递后的播放阶段。切歌、关闭和缓存迁移同时取消旧加载，不能只递增代次后让旧请求继续下载；已加载音频先取消底层下载以唤醒阻塞读取，再关闭音频输出，由解码线程完成释放。取消不会将完整缓存标为损坏，未完成文件仍按临时下载清理。

下一首预取独立持有取消上下文：停止预取可立即结束等待当前下载完成的任务，且不停止当前歌曲；当前播放接管尚在查询 URL 的预取后，后续切歌负责取消该请求。普通暂停保留当前加载，不重启 URL 查询或下载。回归见 [快速切歌、URL 取消、预取接管与缓存保留](../../internal/features/player/load_cancel_test.go)。

版权或资源不可播放与暂时网络失败应分类处理，不能全部自动跳过。恢复网络后的重试也不能覆盖用户后续暂停、切歌或换队列的意图。离线优先使用格式、音质、完整性符合条件的缓存；“有文件”不代表可以播。

手动上一曲遇到明确不可播歌曲时沿当前播放顺序向较早条目寻找，下一曲向较晚条目寻找；连续跳过和同曲重试保留本次方向，最多遍历一轮后停止。随机模式沿既有随机顺序遍历。直接点选、换队列、会话恢复及电台自动续播的不可播后备方向为下一曲方向，不继承之前的上一曲命令；界面切歌动画的方向与后备查找方向分别维护。

回归：player 的 `buffering_test.go`、`playback_failure_test.go`、`unavailable_navigation_test.go`、`offline_test.go`、`preload_test.go`，App 的 `buffering_time_test.go`、`offline_buffering_test.go`、`scrub_test.go`、`recovery_test.go`。

## 会话恢复和播放记录

[App session](../../internal/app/session.go)组装队列、模式、选中项与位置快照；[player/session](../../internal/features/player/session/store.go)维护 `session.json` 和轻量 `session-progress.json`。进度带会话身份，只有匹配的快照可合并，避免旧进度套到新队列。后台写入使用独立快照，不能持有仍会被 UI 修改的 slice。

恢复会话应恢复暂停状态下的可见进度和队列，不应因读取配置自行开始播放。旧格式兼容要用真实旧字段构造夹具，不靠修改默认值掩盖。

播放上报依据音频实际开始、结束的会话。暂停和 Seek 不建立新的播放会话；单曲循环结束后再次开始属于新会话。听歌时长排除暂停和直接跳转的时间，不能用 UI 帧次数或歌曲时长替代。异步上报捕获当时的账号凭据，退出登录后不能拿新账号发送旧记录；串行工作与关闭等待应有边界。

验证 [session_test.go](../../internal/app/session_test.go)、[store_test.go](../../internal/features/player/session/store_test.go)、[play_report_test.go](../../internal/app/play_report_test.go)。

## 当前歌曲定位

入口是歌曲列表右下角的定位按钮，位于播放栏上方。它将当前歌曲行滚到可见区域中部；不能切页、替换队列、改变暂停状态或播放位置。

显示条件：列表含当前项，且该行在实际列表视口之外。行已部分可见就不显示按钮；被播放栏等遮挡的区域不算可见。加载中、列表不含当前项或没有正在播放项时隐藏。用户切歌不会自动触发定位。

实现入口为 [tracklist/locate.go](../../internal/ui/view/tracklist/locate.go)和 [keys.go](../../internal/ui/view/tracklist/keys.go)。稳定行键需要处理重复条目；标题、列表头高度或列表内容变化后应重新计算几何。比较需要保存旧值，不能保留指向已原地修改对象的指针。几何更新可请求必要的后续帧，稳定后不能形成空闲重绘循环。

网盘分页与本地搜索的定位需要 App 协调，但不能让迟到定位请求把用户拉回已离开的页面。鼠标与键盘激活应一致。回归见 [playlist_locate_test.go](../../internal/app/playlist_locate_test.go)、[local_locate_test.go](../../internal/app/local_locate_test.go)、[cloud_test.go](../../internal/app/cloud_test.go)和对应性能测试。

## 音量、响度均衡和频谱

播放器音量、系统应用音量和按歌曲响度调整的音量均衡职责不同；这里的均衡不是多频段 EQ。Windows COM、Linux 音量命令等系统访问放在桌面适配；外部调用不得阻塞 UI。连续滚轮或触控板输入应累计到正确目标，不能被较慢的旧回调倒退覆盖。平台不支持的能力不应显示一个无效开关。

频谱由音频数据采样产生，界面只做必要的平滑和绘制。Seek、暂停、换歌后的过渡不能继续显示前一首数据。性能验证区分采样开销、绘制开销和原生 GPU 行为，不以软件 Tester 帧率替代原生性能结论。

扩展播放功能时，至少同步命令语义、恢复快照、错误分类、系统控制、相关功能文档和受影响回归；若没有涉及其中某项，在交付中说明边界即可，不必修改无关实现。
