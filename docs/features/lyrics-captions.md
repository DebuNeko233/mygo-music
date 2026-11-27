# 歌词、舞台与播客字幕

本章覆盖从歌词原始数据到显示行、逐字动画、桌面歌词，以及独立的播客转写链路。时间轴与文字分类属于业务逻辑，字体和动画属于视图，模型下载与识别属于服务。

## 所有者与数据流

| 路径 | 责任 |
| --- | --- |
| [lyrics](../../internal/features/lyrics) | LRC/YRC 解析、时钟对齐、翻译、署名分类、显示数据、缓存 |
| [viewmodel/playback 的阅读模型](../../internal/ui/viewmodel/playback/lyric_reading.go) | 阅读、手动浏览与跟随意图 |
| [view/playback](../../internal/ui/view/playback) | 逐字行、长列表、字体、舞台、特效、桌面歌词 |
| [speech](../../internal/features/speech) | 模型管理、原生识别引擎、重采样、转写行 |
| [viewmodel/captions](../../internal/ui/viewmodel/captions) | 字幕任务状态与界面操作 |
| [speech/transcript](../../internal/features/speech/transcript) | 转写结果持久化 |
| [App captions](../../internal/app/captions.go) | 当前节目、播放器、窗口和字幕任务协调 |

歌词请求在工作线程加载，发布时检查缓存代次、账号与歌曲请求身份。相同歌曲再次被选择也可能是新请求，不能只比较 song ID。缓存陈旧可刷新，但不能让旧网络结果覆盖较新的本地匹配或账号状态。通用规则见[数据生命周期](../data-lifecycle.md)。

## 显示数据与来源选择

[BuildDisplay](../../internal/features/lyrics/display.go)是普通与沉浸显示共同的数据准备入口。正文和制作署名分开，署名仍保留原始可读文本，并按角色与贡献者去重；不能简单删除疑似署名的所有行。

正文整体选择 LRC 或 YRC，不能把两条正文时间线随意拼接后按文字去重。重复副歌是正常歌词，不是重复数据。YRC 只有制作信息而 LRC 有演唱正文时，保留 LRC 正文并补充可确认的署名。YRC JSON 注解只在满足分类条件时成为补充署名，不进入演唱时间轴。

翻译需要匹配正确的原文时间线。偏移、重复句、无效时间、LRC 与 YRC 时钟差异交给 `reconcile.go` 处理，不能用数组索引硬配。语言识别根据原文文字，不能因翻译为中文就强制中文字体。没有歌词不等于纯音乐；保留“未知”“尚未加载”“无歌词”等状态区别。

回归：[display_test.go](../../internal/features/lyrics/display_test.go)、[reconcile_test.go](../../internal/features/lyrics/reconcile_test.go)、[language_test.go](../../internal/features/lyrics/language_test.go)、[cache_test.go](../../internal/features/lyrics/cache_test.go)。

## 纯净歌词偏好

设置 → 播放中的“纯净歌词”默认开启，新配置及缺少该字段的旧配置采用此默认值，已明确保存的关闭选择继续保留。开启后普通/沉浸歌词、舞台、律动页单行歌词和桌面歌词共同采用纯净展示：隐藏已确认的制作署名，以及借助当前歌曲资料确认的歌名/歌手标题；移除已知演唱者或标准分声部标记的前缀，保留其后的歌词正文与翻译。重复或有逐字时间的单独歌名句、普通冒号句和不能可靠对齐的逐字前缀继续保留，避免误删实际演唱。

转换规则归 [pure.go](../../internal/features/lyrics/pure.go)，不改变解析来源、磁盘缓存、下载导出的歌词或播放器原始时间轴。Player 只保留当前歌曲的一份纯净派生结果；歌词替换和当前本地歌曲资料更新时重建，切歌时清空。不保留历史歌曲的纯净副本，也不在每帧做分类或复制。切换偏好只选择已准备的结果，立即刷新各歌词窗口并持久化，不请求歌词、不定位音频；阅读视图切换后跟随当前播放句。

原本没有正文的来源保留已有空状态提示，包括空响应或仅制作信息来源沿用的“纯音乐，请欣赏”，以及明确的“暂无歌词”；纯净开关不重新判断是否属于纯音乐。只有原本可显示的标题、演唱标记等被全部过滤后，才新增“暂无可显示歌词”，有明确纯音乐提示时仍保留该提示。重复准备纯净展示也必须保留相同状态。匹配本地歌词的入口按当前展示判断是否缺少正文。

回归：[pure_test.go](../../internal/features/lyrics/pure_test.go)、[lyric_presentation_test.go](../../internal/features/player/lyric_presentation_test.go)、[设置接线测试](../../internal/app/pure_lyrics_test.go)、[阅读与定位测试](../../internal/ui/view/playback/pure_lyrics_test.go)。

## 制作署名分类规则

角色词典集中在 [credit_roles.go](../../internal/features/lyrics/credit_roles.go)，判定集中在 [credits.go](../../internal/features/lyrics/credits.go)。添加规则必须同时给出应该识别和不应识别的例子。

- 支持中文、日文、英文角色，以及大小写、空白、全角半角和常用分隔形式的归一化；归一化服务于比较，显示文本仍保留。
- 冒号前像角色不够，后面还需要有效贡献者。复合角色需要能解释各角色，不做无限宽泛的关键词命中。
- `音乐设计/监制` 等复合署名按各自明确角色识别；正文句子包含“音乐设计”不构成署名，未知职责与空贡献者继续保留。
- 支持弦乐编排、声乐编辑、音频助理、合成器操作、译词、Rap 创作、VOCALOID 调校等制作字段，以及录音室、宣传发行的明确角色写法。角色别名不意味着允许任意包含这些词的句子成为署名。
- 英文 `Mixed by`、`Recorded at` 等是表达形式的一部分，需要相应的角色和贡献者证据。
- `Music`、`Vocal`、`Rap`、`Chorus`、`Spoken` 等歧义词要结合开头/结尾位置、明确署名邻居、重复情况和逐字时间证据判断。
- 开头署名块末尾的声乐署名可结合后续至少四秒的间隔确认；超过十秒的间隔会切断署名块关联。修改阈值必须覆盖临界值及真实误判形态，不能为单首歌堆特例。
- 演唱、原唱、翻唱、和声与人声署名可以位于经两个独立强署名确认的片头或片尾块边缘；仍需没有逐字时间且未重复。该边缘规则不扩展到 Rap、Chorus 或普通说唱/合唱分段标记。
- 正文中有逐字时间或重复出现的角色短句，不能仅靠词典吞掉。相互匹配的 LRC 贡献者信息可辅助确认 YRC 署名。
- 弱角色的逐字行只接受已确认为署名的 LRC 同角色、同贡献者在十秒以内的互证；相同文字出现在更远的演唱段落中继续保留。PGM 等缩写单独出现时也不作为强署名。
- 比例号 `∶` 可作为字段冒号参与匹配；标题后重复的异形字段需与已确认署名互证。归一化只作用于匹配副本，不改写正文中的符号，也不取消逐字与重复句保护。

新增别名应说明语言、角色含义和误判风险，不能按某个歌曲 ID 硬编码分类结果。回归集中于 [credits_test.go](../../internal/features/lyrics/credits_test.go)；可选地对自己的歌词缓存执行：

```sh
NM_LYRICS_CACHE=/path/to/browse-v1 go test ./internal/features/lyrics -run '^TestCreditsCachedLyrics$'
```

实际缓存结构以 [credits_cache_test.go](../../internal/features/lyrics/credits_cache_test.go)为准。报告可通过 `NM_LYRICS_REPORT` 输出到仓库外；缓存和完整商业歌词不能作为调试产物随提交上传。公共回归使用最小必要的合成片段。

## 逐字、滚动和舞台

`timed_line.go` 处理换行、字形位置与逐字遮罩，`timed_list.go` 处理长列表与滚动锚点。修改字体、字号或窗口尺寸时必须重新匹配测量与绘制，避免中文/日文 fallback 导致高亮漂移。长歌词不能每帧重新排版全部内容。

手动浏览、暂停跟随、点击跳转与自动跟随必须保留各自意图。用户浏览时不要被每个播放位置更新立即拉回；恢复跟随再按当前时间定位。状态机放阅读模型，视图保存滚动和动画状态。

阴影、模糊和舞台效果遵守缓存失效边界。`DrawCached` 是当前 MyGo fork 的重要能力；文字或尺寸变更必须使缓存更新，稳定层不应重复重建。边缘清除区域不能擦掉滚动条或清晰的手动阅读文字，不再需要的画布及时释放。舞台样式、Folia 等效果须覆盖不同尺寸、暂停和减少动态效果设置。

间奏热评复用评论状态，不能由歌词视图另建账号请求链路。频谱背景消费播放器频谱，不能从绘制回调启动音频采样。

验证独立视图的 `timed_line_test.go`、`timed_list_test.go`、`lyric_edges_test.go`、`lyric_effects_test.go`、`stage_test.go` 和 `perf_test.go`；跨窗口及页面行为放 App。字体、遮罩、渐变还需要导出帧或原生目视检查，文本存在断言不能替代。

## 桌面歌词

桌面歌词有独立窗口和显示设置，包括字体、描边、对齐、行数及翻译。锁定后的输入穿透与解锁入口必须配套；不能让用户锁定后无处恢复。窗口位置持久化应在屏幕变化后约束到可见范围。

Windows 无边框与阴影行为由平台文件处理；Linux/Wayland 的命中区域提交和重绘行为需原生验证。普通窗口 Tester 不能证明系统穿透、置顶或屏幕几何正确。相关入口：[desklyrics.go](../../internal/ui/view/playback/desklyrics.go)、[desklyrics_windows.go](../../internal/ui/view/playback/desklyrics_windows.go)、[desklyrics_test.go](../../internal/app/desklyrics_test.go)。

## 播客字幕与模型

字幕任务面向当前节目，但识别引擎不依赖播放器视图。`speech.Engine` 拥有识别器并串行访问；任务切换通过取消和身份检查拒绝旧结果。关闭、取消、移除模型时需要等待正在使用的原生资源释放，不能先删动态库或模型目录再要求引擎退出。

模型下载处理校验、续传、取消和清理。显示下载完成之前确认资源可用；损坏或半下载状态不能伪装成已安装。转写结果按独立快照排队保存，不能让后续修改污染待写结果。

原生绑定里的结构布局和看似未使用的字段可能属于 C ABI，不能按 Go 引用次数删字段。生成文件应修改生成器或来源，再生成全部目标平台，不能只改当前机器的输出。模型/库版本升级同时检查 `features/speech`、`scripts/sherpa` 及打包路径。

普通单元测试验证任务与数据流，不会证明真实模型能加载。真实转写验证使用 [speech_test.go](../../internal/features/speech/speech_test.go)规定的 `SPEECH_WAV`、`SPEECH_ROOT` 或 `SHERPA_ONNX_DIR`/`SPEECH_MODEL`，可能需要额外下载；不要在未授权情况下上传用户音频。交付明确区分模拟测试、真实识别与各平台原生库验证。
