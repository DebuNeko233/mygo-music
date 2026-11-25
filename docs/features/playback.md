# 播放、队列与音频

本章约束音乐播放链路。下载成文件见[下载与缓存](download-cache.md)，歌词和字幕见[歌词与字幕](lyrics-captions.md)。播放栏、正在播放页、队列、页面播放按钮、系统媒体控制都应汇入同一组播放命令。

## 代码归属

| 所有者 | 职责 |
| --- | --- |
| [player](../../internal/player) | 队列、模式、播放状态、解码、流式读取、预取、音频缓存和临时播放 |
| [App 播放命令](../../internal/app/play_commands.go) | 页面播放意图、数据源身份、跨功能协调 |
| [Playback ViewModel](../../internal/viewmodel/playback) | 播放相关的界面状态与操作语义 |
| [播放视图](../../internal/desktop/view/playback) | 控件、手势、进度、封面、动画、正在播放及舞台布局 |
| [tracklist](../../internal/desktop/view/tracklist) | 通用歌曲行、虚拟列表和当前歌曲定位 |
| [sessionstore](../../internal/sessionstore) | 会话与进度快照的存储格式和串行写入 |
| [App 上报](../../internal/app/play_report.go) | 捕获实际播放会话、账号隔离、网易云播放记录上报 |
| [mixvol](../../internal/mixvol) | 平台应用音量能力 |

新增播放入口先复用已有命令。不能在视图复制一套队列、URL 获取或播放器状态机，也不能把 App 页面对象传进 player。

## 页面播放语义

“播放这个页面”“从第 N 首播放”“随机播放整个列表”“下一首播放”和“临时打开文件”不是相同操作。调用前明确是否替换队列、是否保留随机模式、起始位置及数据源归属。

- 页面普通播放从第一个可播放条目开始；不可播放条目不能把起点变成无效索引。
- 明确点选一首时保留选中的起点；不能因为已有随机模式而换成另一首。
- 整表随机播放允许随机起点，与“选中某首后按随机模式继续”分开。
- 歌手页按页面命令选择模式；最近播放不应意外绑定另一个页面的数据源。
- 网盘等有后续分页的队列必须保留其来源身份。继续加载只能补充仍属于该来源的队列。
- 临时本地播放遵循[本地音乐](local-music.md)的队列保留规则。

队列身份不能只用歌曲 ID：重复歌曲是多个条目，本地文件可能没有远端 ID。删除、重排、文件重定位后必须保持当前项、历史和随机遍历关系一致。

回归优先看 [play_commands_test.go](../../internal/app/play_commands_test.go)、[shuffle_backport_test.go](../../internal/player/shuffle_backport_test.go)、[queue_completion_test.go](../../internal/player/queue_completion_test.go)、[navigation_direction_test.go](../../internal/player/navigation_direction_test.go)。

## 解码和流式读取

网络、本地文件、内存缓存和磁盘缓存共用格式与解码策略。按实际字节识别格式，不能相信 URL 后缀或文件扩展名；同一格式在四种来源下都要能正确读、跳转和结束。

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

版权或资源不可播放与暂时网络失败应分类处理，不能全部自动跳过。恢复网络后的重试也不能覆盖用户后续暂停、切歌或换队列的意图。离线优先使用格式、音质、完整性符合条件的缓存；“有文件”不代表可以播。

回归：player 的 `buffering_test.go`、`playback_failure_test.go`、`offline_test.go`、`preload_test.go`，App 的 `buffering_time_test.go`、`offline_buffering_test.go`、`scrub_test.go`、`recovery_test.go`。

## 会话恢复和播放记录

[App session](../../internal/app/session.go)组装队列、模式、选中项与位置快照；[sessionstore](../../internal/sessionstore/store.go)维护 `session.json` 和轻量 `session-progress.json`。进度带会话身份，只有匹配的快照可合并，避免旧进度套到新队列。后台写入使用独立快照，不能持有仍会被 UI 修改的 slice。

恢复会话应恢复暂停状态下的可见进度和队列，不应因读取配置自行开始播放。旧格式兼容要用真实旧字段构造夹具，不靠修改默认值掩盖。

播放上报依据音频实际开始、结束的会话。暂停和 Seek 不建立新的播放会话；单曲循环结束后再次开始属于新会话。听歌时长排除暂停和直接跳转的时间，不能用 UI 帧次数或歌曲时长替代。异步上报捕获当时的账号凭据，退出登录后不能拿新账号发送旧记录；串行工作与关闭等待应有边界。

验证 [session_test.go](../../internal/app/session_test.go)、[store_test.go](../../internal/sessionstore/store_test.go)、[play_report_test.go](../../internal/app/play_report_test.go)。

## 当前歌曲定位

入口是歌曲列表右下角的定位按钮，位于播放栏上方。它将当前歌曲行滚到可见区域中部；不能切页、替换队列、改变暂停状态或播放位置。

显示条件：列表含当前项，且该行在实际列表视口之外。行已部分可见就不显示按钮；被播放栏等遮挡的区域不算可见。加载中、列表不含当前项或没有正在播放项时隐藏。用户切歌不会自动触发定位。

实现入口为 [tracklist/locate.go](../../internal/desktop/view/tracklist/locate.go)和 [keys.go](../../internal/desktop/view/tracklist/keys.go)。稳定行键需要处理重复条目；标题、列表头高度或列表内容变化后应重新计算几何。比较需要保存旧值，不能保留指向已原地修改对象的指针。几何更新可请求必要的后续帧，稳定后不能形成空闲重绘循环。

网盘分页与本地搜索的定位需要 App 协调，但不能让迟到定位请求把用户拉回已离开的页面。鼠标与键盘激活应一致。回归见 [playlist_locate_test.go](../../internal/app/playlist_locate_test.go)、[local_locate_test.go](../../internal/app/local_locate_test.go)、[cloud_test.go](../../internal/app/cloud_test.go)和对应性能测试。

## 音量、响度均衡和频谱

播放器音量、系统应用音量和按歌曲响度调整的音量均衡职责不同；这里的均衡不是多频段 EQ。Windows COM、Linux 音量命令等系统访问放在桌面适配；外部调用不得阻塞 UI。连续滚轮或触控板输入应累计到正确目标，不能被较慢的旧回调倒退覆盖。平台不支持的能力不应显示一个无效开关。

频谱由音频数据采样产生，界面只做必要的平滑和绘制。Seek、暂停、换歌后的过渡不能继续显示前一首数据。性能验证区分采样开销、绘制开销和原生 GPU 行为，不以软件 Tester 帧率替代原生性能结论。

扩展播放功能时，至少同步命令语义、恢复快照、错误分类、系统控制、相关功能文档和受影响回归；若没有涉及其中某项，在交付中说明边界即可，不必修改无关实现。
