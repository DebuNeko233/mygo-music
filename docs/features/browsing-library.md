# 浏览、账号与资料库

本章覆盖 API 数据到页面的业务路径。UI、异步发布和持久化的共性规则分别见[界面开发](../ui-development.md)与[生命周期](../data-lifecycle.md)。下列测试名称用于定位，具体行为以测试源码为准。

## 1. 登录与账号生命周期

入口包括账号菜单、需要登录的收藏/歌单操作，以及登录失效后的重新登录。扫码结果不能立刻污染当前活动会话：QR 尝试持有自己的凭据，确认与提交分阶段进行，账号模型管理有效性，App 协调其他功能重置。

| 责任 | 位置 |
| --- | --- |
| QR 协议、Cookie、凭据保存和钥匙串分类 | [netease/auth.go](../../internal/features/netease/auth.go) |
| 登录表单、验证、保存错误、请求身份 | [viewmodel/account](../../internal/ui/viewmodel/account) |
| 登录弹窗、QR 绘制、帮助、复制与重试 | [view/account.go](../../internal/ui/view/account.go) |
| 会话切换后的播放器、资料库和缓存协调 | [app/account.go](../../internal/app/account.go) |

必须保留：网络故障可保留离线账号，不因临时错误清空资料库；同 UID 重新登录仍是新的生命周期；旧 QR、资料查询或退出回调不能覆盖新登录。凭据保存失败必须可见，不能假装登录已持久化。

Linux 缺少 Secret Service 的情况提供[应用帮助资源](../../internal/features/netease/keyring_help.md)。存储不支持和某条凭据不存在不同；帮助复制失败应反馈，重新尝试应开启有效的新流程。该 Markdown 是运行时嵌入资源，不能因整理 docs 而移动或删除。

回归：`internal/features/netease/auth_test.go`、`ui/viewmodel/account/account_test.go`、`app/account_test.go`、`account_library_test.go`、`logout_test.go`、视图 `account_test.go`。扩展登录方式必须复用会话边界，不为新界面另建账号事实。

## 2. 首页、每日推荐与排行榜

[Home ViewModel](../../internal/ui/viewmodel/home/home.go)共享首页、排行榜和每日推荐的数据，但各模块独立发布。某模块阻塞或失败不能阻止健康模块出现；重试保留可用旧内容，成功空结果与失败区分。

每日推荐以北京时间早上六点的刷新边界判断是否跨日，不取用户当前时区的午夜。每日卡片与每日页面共享歌曲来源，播放/暂停不能创建第二套队列。App [home.go](../../internal/app/home.go)注入服务；展示归 view home、daily 和 cards。

回归：`ui/viewmodel/home/home_test.go`、`home_day_test.go`、`app/home_mvvm_test.go`，独立卡片命令见 `ui/view/home_test.go`。增加推荐模块应明确是否独立失败、如何缓存以及何时刷新。

## 3. 搜索、联想与链接

普通查询、在线联想、当前歌单/本地歌曲建议和分享链接使用同一个用户输入入口，但执行路径不同：

- [search 模型](../../internal/ui/viewmodel/search)管理查询、分类、分页和联想；历史保留最近二十条，重复查询移到最前。
- 曲目建议依据当前来源重新匹配，在选择时再次检查歌曲是否仍存在、可播，不能直接使用旧建议的索引。
- [netease/links.go](../../internal/features/netease/links.go)解析支持的 URL、分享文本、短链和 `orpheus://`；[Links 模型](../../internal/ui/viewmodel/links/links.go)管理取消及解析结果。
- App [search.go](../../internal/app/search.go)与 [music_links.go](../../internal/app/music_links.go)协调导航、播放、启动意图和系统协议注册。

输入变化或切页后旧联想不可覆盖新查询；分页失败在原游标重试；普通文本不应被误判为无效链接。短链请求可取消，解析完成后不能抢走用户后来手动开始的播放。冷启动文件和链接共同服从 App 启动意图排序。

回归：`ui/viewmodel/search/search_test.go`、`features/netease/links_test.go`、`app/catalog_test.go`、`music_links_test.go`、`ui_suggest_playlist_test.go`、`local_search_test.go`。

## 4. 共享资料库与歌单编辑

[library.State](../../internal/features/library/library.go)是喜欢歌曲、歌单和收藏专辑的共享事实；[Library ViewModel](../../internal/ui/viewmodel/library)管理账号作用域、加载、刷新和编辑。侧栏、详情页和播放器控件借用同一份已发布状态。

支持喜欢/取消喜欢、节目喜欢、收藏/取消收藏、创建/重命名/删除歌单及歌曲增删。是否允许编辑要按归属判断；不能因入口变化绕过自己的歌单、收藏歌单和“我喜欢”的语义限制。

重要边界：

1. 乐观结果立即同步所有消费者；失败只回滚本次仍有效的编辑。
2. 旧网络刷新不能覆盖较新的本地编辑，也不能让已删除歌单重新出现。
3. 刷新相同内容保持已有切片或视图身份，减少滚动、封面和队列扰动。
4. 快照聚合到编辑稳定后，复制给后台写盘；旧账号请求不可持久化到新账号。
5. 已接受但未完成的写入仍应被读取流程看见，不能因内存裁剪而出现离线内容短暂丢失。
6. 详情缓存有预算，但可见、正在编辑或被请求持有的对象需保护，结束后再恢复裁剪。

App [library_commands.go](../../internal/app/library_commands.go)、[library_refresh.go](../../internal/app/library_refresh.go)负责登录和跨功能接线；[playlist_cache.go](../../internal/app/playlist_cache.go)负责写入与 scope/epoch。页面及侧栏布局归 [view/library](../../internal/ui/view/library)。

回归：模型 `library_test.go`、`playlist_test.go`、`snapshots_test.go`；App `playlistedit_test.go`、`library_refresh_test.go`、`playlist_cache_test.go`、`sidebar_test.go`。播放或定位歌曲另见[播放手册](playback.md)。

## 5. 歌手、专辑和用户详情

Artist、Album、User 各自有独立 ViewModel，App [details.go](../../internal/app/details.go)提供账号有效性、导航和服务。歌手页面有热门/最新歌曲、专辑和动态；专辑有完整歌曲与收藏；用户页区分公开内容和本人私有关注/订阅。

请求合并不应让同 ID 的新页面接受旧结果。歌手关注操作完成后，早发出的资料请求不能撤销新关注状态。分页空结果应停止无效继续请求，动态等待必要的歌手资料和可见状态。专辑离线快照必须保留 API JSON 默认忽略的歌曲。

回归在 `ui/viewmodel/artist`、`album`、`user` 旁，跨账号和共享收藏见 `app/details_mvvm_test.go`。新增详情标签须说明单独失败是否影响其他标签、可见性如何控制加载和恢复。

## 6. 音乐网盘

[netease/cloud.go](../../internal/features/netease/cloud.go)保留上传身份，不用匹配到的公共歌曲 ID 替换用户上传 ID。服务端原始 offset 与过滤后的行数分开计算。

[Cloud 模型](../../internal/ui/viewmodel/cloud/cloud.go)区分继续扫描和整表刷新。刷新暂存完整替代列表，失败时旧列表保持可搜索；重试不会混入旧去重集合。App [cloud.go](../../internal/app/cloud.go)协调完整搜索范围、队列补全和当前歌曲定位。

后台继续加载不能改变用户的新队列。定位目标在歌曲变化、离页或取消后失效；已能定位时可以先显示目标，不必等待无关剩余分页。新增网盘操作不得退回只搜索第一页。

回归：`features/netease/cloud_test.go`、`ui/viewmodel/cloud`、`app/cloud_test.go`、`cloud_completion_test.go`。

## 7. 最近播放与续播

[Recent 模型](../../internal/ui/viewmodel/recent/recent.go)分别加载歌曲、歌单、专辑、播客和节目，各分类互不阻塞。局部失败保留其他类别，快照与旧版本格式兼容，磁盘读取不阻塞 UI。

登录后的跨设备续播使用 [Playback.LastStretch](../../internal/ui/viewmodel/playback/pickup.go)选取同设备连续收听片段；App [pickup.go](../../internal/app/pickup.go)决定何时读取和是否恢复。不能在用户已有新播放意图时抢占队列。

回归：`ui/viewmodel/recent/recent_test.go`、`ui/viewmodel/playback/pickup_test.go`、`app/recent_test.go`、`recent_mvvm_test.go`。

## 8. 播客与节目

[Podcasts 模型](../../internal/ui/viewmodel/podcast/podcast.go)管理推荐、热门、详情、节目分页与订阅。卡片播放可能需要同时等待频道信息和节目，重复点击应合并；后来的手动选择或新队列替代旧播放意图。

节目分页去重但保留原始游标，失败重试同一页。订阅与个人页面共享数据；旧订阅列表不能覆盖刚完成的操作，失败回滚不破坏并发编辑。字幕与模型另归[歌词与字幕](lyrics-captions.md)。

回归：`ui/viewmodel/podcast/podcast_test.go`、`app/podcast_mvvm_test.go` 和 `catalog_test.go`。增加节目功能必须检查歌曲/节目身份、评论线程及喜欢接口的差异。

## 9. 私人 FM、心动与评论

[Radio 模型](../../internal/ui/viewmodel/radio/radio.go)管理模式和补歌。心动模式通过注入查询读取最新资料库，不持有 Library ViewModel；同模式的新请求也需要身份区分。启动中更改模式或手动播放优先，不喜欢失败要反馈，不能无条件跳下一首。

[Comments 模型](../../internal/ui/viewmodel/comments/comments.go)管理热门/最新评论、分页和点赞。切歌再返回同一歌曲仍应隔离旧线程请求；加载时保留计数，页内及跨页去重不能错误改变服务端游标。点赞回复不能覆盖更新的点击。

回归：`ui/viewmodel/radio/radio_test.go`、`comments/comments_test.go`，App `fm_mode_test.go`、`fm_trash_test.go`、`radio_test.go`、`comments_test.go`。播放期间的间奏热评属于歌词视图与评论数据的组合，不应再建评论请求源。
