# 下载、缓存与离线数据

歌曲下载是用户持有的导出文件；播放器缓存是应用管理的可清理数据。两者可以复用完整的源字节，但不能共享一个会被写标签或清理的最终文件。

## 歌曲导出入口与归属

单曲菜单提供下载，歌单/专辑菜单提供整表下载，设置提供保存目录、下载音质、命名及歌词选项。默认目录是 `~/Music/Nyago Music`，首次使用时创建。下载音质与播放音质独立；服务端权限、可用地址和实际返回音质仍可能限制结果。

| 所有者 | 职责 |
| --- | --- |
| [download](../../internal/download) | 下载任务、元数据整理、临时产物、最终发布 |
| [audiotag](../../internal/audiotag) | 音频标签和封面读写 |
| [audioformat](../../internal/audioformat) | 实际格式与导出扩展名 |
| [player/export.go](../../internal/player/export.go) | 复用完整音频缓存或共享下载能力 |
| [viewmodel/downloads](../../internal/viewmodel/downloads) | 任务列表、进度、取消和重试的界面状态 |
| [App download](../../internal/app/download.go) | 歌曲来源、用户设置与任务协调 |
| [settings/download](../../internal/desktop/view/settings/download.go) | 下载选项展示和命令入口 |

任务入队时捕获目录、音质、命名和歌词选项；用户修改设置影响后续任务，不让正在运行的任务中途换目标。任务列表是当前进程的状态，不承诺重启自动续跑。

## 文件、标签与歌词契约

命名支持歌手—标题、标题—歌手和仅标题等现有选项。已有音频后缀应正确处理；歌手缺失时不留下多余分隔符。目标重名使用序号避让，不能覆盖用户原文件。

标签包含来源实际提供的标题、歌手、专辑、专辑歌手、日期、碟号、曲号和发行方等。曲号依据专辑顺序，不能用当前歌单的行号冒充。缺失字段不编造。网盘原文件已含标签、封面、歌词时尽量保留，仅补充缺失信息。

可选 `.lrc` 旁置文件使用正确时间对齐的歌词和翻译。歌词缺失不能使可用音频导出无故失败，也不能输出不相干的上一首歌词。标签与歌词失败如何反馈由任务结果表达，不在视图中吞掉错误。

导出流程先获得符合条件的源数据，写临时文件、补充标签并验证，再发布最终路径。取消、重试、失败和退出需要清理自己的临时产物。发布遵循各平台实现的冲突保护，不能以“先检查不存在，再直接覆盖”替代。

## 缓存复用和格式验证

仅复用完整、音质及格式符合要求的缓存；不能把正在补洞的流式文件当作完成。导出修改的是副本，不修改正在播放的缓存。文件格式根据字节识别，不能仅改扩展名伪装成另一格式；导出不承诺转码。

覆盖的格式路径包括 MP3、FLAC、WAV（含浮点及扩展格式）、Ogg Vorbis、ADTS AAC/HE-AAC、M4A AAC/ALAC/HE-AAC；3GP/3G2 的导出扩展名按容器策略规范化。分片 M4A 等边界由格式夹具验证。

标签写入使用 `go.senan.xyz/taglib` 及其 Wasm 运行时，不要求用户安装独立标签命令。依赖许可随应用资源分发。升级标签库要同时检查资源许可及播放器能否重新读取导出文件。

验证层次：

1. 元数据与命名规则的纯逻辑测试。
2. 写标签前后重新读取，验证元数据与容器仍有效。
3. 对格式夹具解码，比较写标签前后的 PCM 样本，确保未损坏音频。
4. 播放器统一路径验证导出文件、冷缓存与热缓存的读取和 Seek。
5. 受平台运行时约束的签名产物另作原生验证。

主要回归：[download_test.go](../../internal/download/download_test.go)、[metadata_test.go](../../internal/download/metadata_test.go)、[audiotag](../../internal/audiotag)、[export_test.go](../../internal/player/export_test.go)、[tagged_formats_test.go](../../internal/player/tagged_formats_test.go)、[App download_test.go](../../internal/app/download_test.go)。

## macOS 签名运行时

Wasm 引擎运行时涉及匿名内存权限。当前 [macos.entitlements.plist](../../macos.entitlements.plist)包含 `allow-unsigned-executable-memory`；普通 `go test` 或只检查签名存在不能证明 Hardened Runtime 下可以下载写标签。

[test_signed_download.py](../../scripts/test_signed_download.py)验证签名环境下的下载与播放器冷/热路径，默认可用临时 ad-hoc 签名，也支持显式身份及输出目录。运行方式见[开发环境](../development.md)。正式产物仍要确认最终权限和运行结果，不能因为独立探针通过就声称所有发行包验证完毕。不得把个人证书名称、密钥或本机绝对路径写成仓库要求。

## 缓存类别与所有者

| 类别 | 所有者 | 需要保留的边界 |
| --- | --- | --- |
| 音频 | player | 完整性、音质、并发读取、容量与活动文件 |
| 通用磁盘条目 | [diskcache](../../internal/diskcache) | 键、序列化与落盘完整性 |
| 页面数据 | [pagecache](../../internal/pagecache)及 App | 账号作用域、代次、后台快照 |
| 歌词 | lyrics | 过期刷新、歌曲身份与显式发布 |
| 图片 | desktop/artwork | 下载/解码预算、嵌入封面、图像释放 |
| 布局迁移 | [cachemove](../../internal/cachemove) | 目录验证、事务、日志与恢复 |

缓存路径不能散落在视图里拼接。当前受管理布局包括 `songs`、`browse-v1`、`artwork-v2`；根目录可能含其他应用或用户文件，不能整根复制、删除或把未知文件视作垃圾。登录态、配置和用户曲库不因清缓存而全部删除。

缓存数据可以先展示并后台更新，但成功的网络新结果不应被迟到磁盘读取覆盖。退出登录与同账号重新登录均切换代次。新账号不能看到旧账号的私有资料库缓存。

歌曲列表的小封面由 [artwork/list.go](../../internal/desktop/artwork/list.go)记录可见区，在原有 64 MiB 位图预算内优先保留最近三个列表页的最后一屏，每页最多 30 张，合计最多 8 MiB。保留条目仍由同一个缓存持有和计费；滚动替换当前页的可见区，重复封面只计算一次保留成本。硬容量压力可先回收最旧页面的保留条目，不能为了返回页面而突破总预算。播放封面、页面大图和模糊背景不占用这项列表保留额度。

列表封面位图就绪后直接显示，包括磁盘或本地内嵌封面恢复；其他封面的既有淡入策略保持不变。磁盘读取和解码仍在后台，未保留或首次访问的封面允许短暂占位。缓存 `Stats` 提供累计内存命中、加载启动、容量淘汰和可见列表占位次数，以及当前列表保留条目和字节数；这些计数按 UI 构建调用统计，不是独立屏幕帧数，不包含 URL 或账号信息。

歌单后台刷新可能在 `p1.music.126.net` 与 `p2.music.126.net` 之间切换同一封面的地址。公开 HTTPS 地址的路径一致、查询仅含规范尺寸参数且不携带 Cookie 时，显示层在新地址加载期间复用已有位图，避免已显示的歌曲封面退回占位图。新地址仍独立请求、解码及校验，磁盘缓存键不合并；其他域名、端口、用户信息、版本或签名查询不进入这项镜像复用。明确撤销的新地址仍显示缺失状态。

回归入口：[list_test.go](../../internal/desktop/artwork/list_test.go)、[list_cover_test.go](../../internal/app/list_cover_test.go)、[list_cover_io_test.go](../../internal/app/list_cover_io_test.go)、[image_cache_test.go](../../internal/netease/image_cache_test.go)。测试覆盖多页返回首帧、后台刷新切换 CDN 时的中间构建与解码完成、垃圾回收、容量及可见区边界、撤销失效与断网后的磁盘恢复；`NM_FRAMES` 可输出明暗及常规/最小窗口的软件渲染结果。

## 目录迁移事务

设置中的迁移由 [App cache_directory](../../internal/app/cache_directory.go)协调。底层路径校验、文件清单、日志与复制放 cachemove，不将 UI 回调混入文件算法。

迁移要覆盖相同目录、父子嵌套、符号链接解析、跨磁盘、已有目标文件、空间不足、取消、保存配置失败和进程中断。内容冲突保留目标数据并报错，不能默认覆盖。

正确顺序是冻结受影响写入，准备并验证目标，提交配置并重绑读取者，随后回收旧文件。复制或配置提交失败应保留旧目录的可用数据。旧 reader 不能在提交后继续向待删除目录写入；恢复日志必须与实际提交阶段一致。

测试覆盖 [cachemove](../../internal/cachemove)、[cache_directory_test.go](../../internal/app/cache_directory_test.go)、[cache_layout_test.go](../../internal/app/cache_layout_test.go)、[cache_review_test.go](../../internal/app/cache_review_test.go)。不能以“成功复制几个文件”的测试替代事务失败验证。
