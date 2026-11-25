# 本地音乐与封面

本地歌曲入口管理文件夹、曲库、缺失文件和歌词匹配；系统打开音频文件还可以启动临时播放。本地文件身份、网易云元数据身份和队列条目身份必须分开。

## 代码分工

| 位置 | 职责 |
| --- | --- |
| [localcatalog](../../internal/localcatalog) | 扫描、目录编辑、歌曲身份、缺失/重定位、歌词文件规则 |
| [localmedia](../../internal/localmedia/metadata.go) | 读取本地标签、时长等媒体元数据 |
| [viewmodel/local](../../internal/viewmodel/local) | 曲库状态、过滤与本地操作的状态转换 |
| [view/local](../../internal/desktop/view/local) | 列表、歌词选择交互 |
| [App local_music](../../internal/app/local_music.go) | 文件选择、后台扫描、目录持久化与播放器协调 |
| [App local_open](../../internal/app/local_open.go) | 系统打开、临时文件、预备解码结果的交接 |
| [App local_lyrics](../../internal/app/local_lyrics.go) | 在线歌词候选、用户确认、文件保存与当前播放刷新 |
| [desktop/artwork](../../internal/desktop/artwork) | 本地内嵌封面、在线图片、匹配与解码缓存 |
| [App track_artwork](../../internal/app/track_artwork.go) | 当前曲目、账号、匹配请求和发布代次协调 |

## 扫描与目录编辑

扫描工作在后台执行；结果回到 App 时确认目录和请求仍有效。文件大小和修改时间未变时复用元数据及稳定 ID，避免每次打开页面重新解码整个曲库。重叠目录不应产生重复条目，单个损坏文件不能让整个扫描失败。

文件暂时不可访问与用户主动删除不同。缺失文件应能呈现并重定位，不能把移动磁盘离线当作用户删除所有歌曲。移除目录时只操作受管理记录，不删除用户音频。

曲库文件由 App 按用户数据路径保存到 `local-music.json`。歌词文件规则由 localcatalog 维护。涉及移除目录、目录提交与歌词清理时，先确认目录状态成功提交，再删除不再归属的歌词；写入失败不得提前清理仍在使用的数据。旧扫描结果不能复活用户刚移除的目录。

重定位需要更新曲库条目及队列中所有相关条目，包括重复出现的同一文件。批量更新形成一致的新版本，不得逐项发布半更新的队列。新扫描也不能覆盖用户之后进行的元数据或路径编辑。

回归：[catalog_test.go](../../internal/localcatalog/catalog_test.go)、[local_music_test.go](../../internal/app/local_music_test.go)、[local_update_test.go](../../internal/player/local_update_test.go)，模型自身行为在 [viewmodel/local](../../internal/viewmodel/local)旁验证。

## 搜索、播放与定位

本地搜索过滤当前曲库，但不改变底层文件身份。重排、过滤、扫描完成后保持选中项的语义。列表播放复用 App 播放命令，不能把过滤后的行号当成未过滤队列的索引。

当前歌曲定位遵循[播放手册](playback.md)：只滚动需要的列表；清除过滤或加载数据时保留用户意图，过期定位不能影响新页面。验证 [local_search_test.go](../../internal/app/local_search_test.go)、[local_locate_test.go](../../internal/app/local_locate_test.go)。

## 系统打开和临时播放

从文件关联或打开文件入口播放，不要求先把文件夹加入曲库。已在曲库中的文件复用条目；临时文件不能为了获取歌词而强制持久化成正式曲库成员。

临时播放保留原有队列及继续播放语义。完成后能够继续原队列，不能把它整体替换成一首歌；原位置和随机顺序也需要保留。新打开意图到来时，旧的预备解码结果即使成功也要释放，不能抢占新歌曲。

预备解码器成功交给播放器后由播放器关闭；交接前失败、取消或过期则由准备任务关闭。尤其检查 AAC/MP4 底层资源，不能只清空 Go 指针。平台文件关联在 App 的 `fileassoc_*` 中适配，不让 player 依赖操作系统窗口接口。

验证 player 的 [temporary.go](../../internal/player/temporary.go)对应测试、[local_test.go](../../internal/player/local_test.go)、[lifecycle_test.go](../../internal/player/lifecycle_test.go)，以及 App 的本地打开、文件关联集成回归。

## 在线歌词匹配

匹配是显式选择候选并确认的操作，不能仅因搜索结果相似就静默覆盖歌词。候选远端 ID 提供歌词及补充元数据，不能取代本地文件身份。

每次搜索和确认需要请求身份。对话框关闭、歌曲改变、目录移除、重新搜索后，旧结果不能回填。正式条目的保存失败应反馈并保留可重试状态，不能先显示保存成功再丢失文件。临时播放的匹配不应创建虚假的持久化曲库条目。

新歌词发布时更新当前歌曲显示，并遵守歌词缓存代次。已删除目录不能被迟到的歌词写入重新创建。回归集中于 [local_lyrics_test.go](../../internal/app/local_lyrics_test.go)和本地模型测试。

## 封面来源与匹配

封面可来自网易云 URL、文件内嵌图片或根据本地/网盘元数据找到的候选。读取嵌入图片与解码都是后台工作；并发请求相同文件应合并，不要由列表每一行重复读取同一个大文件。

匹配优先使用有效标签，文件名只作有限后备。标题、歌手和时长共同辅助判断，混音、现场、伴奏等不同版本不能因标题相同就强配。候选不足或歧义应保留无封面的正常状态。

缓存分别限制下载数据、解码后像素和本地嵌入图开销。任务槽不能在后台解码完成、但 UI 尚未发布时过早释放，否则结果队列会堆积大量图片。当前曲目快速切换时拒绝旧图发布，并及时释放不用的图像。

账号变化、文件修改、缓存迁移与失败重试都影响请求有效性。主色和模糊图是派生数据，失效需与原图一致；不要把大图解码搬进颜色查询或绘制回调。

回归：[artwork 缓存与匹配测试](../../internal/desktop/artwork)、[track_artwork_test.go](../../internal/app/track_artwork_test.go)、[cover_switch_test.go](../../internal/app/cover_switch_test.go)。验证大图限制和资源释放，不能只检查最终 URL。
