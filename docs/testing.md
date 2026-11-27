# 测试与验证

测试验证可观察行为和真正可能回归的边界，不复制实现。本文是本仓库测试规则的统一来源；代理和贡献指南只链接这里。功能对应的回归入口见[功能索引](features/README.md)。

## 1. 测什么，不测什么

必须覆盖实际改变的行为：点击、按键、拖动、滚动产生的状态；解析与时间计算；队列、定位、缓存、解码；修复过的错误条件；异步取消和过期结果；空闲帧预算。

禁止为了覆盖率添加以下测试：

- 把所有标签、表单默认值、选项顺序逐项抄成断言。
- 验证 MyGo 自身的动画或控件实现，而没有应用行为。
- 只断言某方法被调用，无法证明用户结果正确。
- 用固定睡眠等待动画，或依赖机器时钟的周期相位。
- 对完整截图做脆弱的逐像素相等；允许检查图层覆盖等必须保证的几何/像素性质。

旧配置迁移、保存关闭状态和重新加载属于行为兼容测试，不是“检查默认值”。新增能力不要求机械增加测试数量；已有测试能覆盖时扩展或复用。

## 2. 测试归属

| 被验证对象 | 放置位置 | 典型入口 |
| --- | --- | --- |
| API 协议、响应与错误 | `internal/features/netease/*_test.go` | [auth_test.go](../internal/features/netease/auth_test.go) |
| 模型命令、分页、代次 | `internal/ui/viewmodel/<feature>/*_test.go` | [cloud/viewmodel_test.go](../internal/ui/viewmodel/cloud/viewmodel_test.go) |
| 独立控件和交互 | 所属 `ui/view` 包旁 | [playback/lyric_edges_test.go](../internal/ui/view/playback/lyric_edges_test.go) |
| App 接线、跨窗口和跨功能 | `internal/app/*_test.go` | [sidebar_settings_test.go](../internal/app/sidebar_settings_test.go) |
| 存储、音频、缓存、解码 | 所属服务包旁 | [player/streaming_boundaries_test.go](../internal/features/player/streaming_boundaries_test.go) |
| 构建和打包工具 | 工具目录旁 | [scripts/sherpa/main_test.go](../scripts/sherpa/main_test.go) |
| 层次依赖 | `internal/app/architecture` | [boundaries_test.go](../internal/app/architecture/boundaries_test.go) |

每个新测试前写一句行为注释。混合文件迁移时逐函数判断，不把集成测试为了目录整齐拆成失去接线验证的单元测试。

包内夹具留在该包 `helpers_test.go` 或有具体名字的测试文件；不要复制整个 App 作为“小夹具”。真正通用的界面操作在 [internal/ui/view/testutil](../internal/ui/view/testutil)，ViewModel 等待工具在 [ui/viewmodel/internal/testutil](../internal/ui/viewmodel/internal/testutil)。生产代码不得导入它们。样本放在所有者的 `testdata/`，说明生成方式和许可。

## 3. UI 测试

使用 `ui.NewTester` 或 `ui.NewTesterWith`。App 集成通过 [newTestApp](../internal/app/helpers_test.go)，独立播放视图通过 [playback helpers](../internal/ui/view/playback/helpers_test.go)构建。先隔离用户数据，再操作界面。

1. 设置最小有效模型和必要回调，不启动真实账号请求。
2. 按可访问性标签定位，执行 Click、RightClick、Press/Move/Release、Key 或 Scroll。
3. 用 `Frame`、`Advance` 或共享测试工具推进虚拟时间。
4. 检查最终业务状态、回调效果和资源释放，不只检查控件存在。
5. 涉及条件渲染时额外执行几帧，覆盖隐藏、恢复、切页与窗口变化。

测试开始时的时钟不是固定零点；动画相位必须以 `tt.Now()` 或受控时钟为基准。`FramePending` 不计入等待 After/Painter.After 的所有情况；单看它为 false 不能证明没有周期唤醒。

新 UI 测试连续执行五次：

```sh
CGO_ENABLED=0 go test ./internal/app -run '^TestSidebarSettingsApplyAndReload$' -count=5
```

失败先复现和定位。不能靠重跑直到通过交付；调整脆弱断言要保留原有行为覆盖，确实无可靠价值的测试才删除并说明。

## 4. 异步、文件与网络测试

用 channel 或已有 Post 队列控制完成顺序，而不是依赖请求恰好谁先返回。至少考虑：A 请求晚于 B 完成、同 ID 的新会话、关闭后回调、部分失败、取消后立即重试、磁盘旧快照晚于网络新结果。

为可替换网络、剪贴板或系统操作安装测试替身，并用 `t.Cleanup` 或 defer 恢复。修改全局替身的测试不使用 `t.Parallel`。HTTP 夹具使用 `httptest`，文件写入使用 `t.TempDir()`；不使用个人曲库与真实凭据。

验证持久化时等待现有保存器 Flush/Wait，而非 sleep。失败测试应检查原文件、临时文件和发布状态，不能只检查返回 error。导出音频验证写标签前后 PCM，不只检查扩展名。

## 5. 常规命令与范围

先执行最小相关测试，然后完成提交前检查：

```sh
git diff --check
go build ./...
go tool mygo vet .
CGO_ENABLED=0 go test ./...
```

`go tool mygo vet .` 在当前固定框架中先执行 Go vet，再检查 UI 生命周期等规则；CI 也使用此入口。`go test ./...` 包含 internal 和工具测试，满足内部包回归要求。只要动过生产 Go 逻辑，不能以单个定向测试替代全量回归。

发行说明、资产汇总/发布及 Homebrew 同步工具由 Release Notes 工作流单独验证；改动这些工具或对应工作流时本地执行：

```sh
python3 -m unittest discover -s .github/scripts -p '*_test.py'
```

文档专属改动按[文档维护](documentation.md)校验；不写无意义的文案测试。修改文档里的可执行脚本、生成器或配置时，仍应验证它们的实际行为。已通过的检查不无理由反复执行；新修改、失败或未解决风险才触发追加验证。

## 6. 竞态检查

共享状态、下载、音频、存储、异步结果与关闭流程变化时，执行相关包 race。示例：

```sh
CGO_ENABLED=1 go test -race ./internal/features/player -count=1 -timeout 30m
CGO_ENABLED=1 go test -race ./internal/app -run '^TestSidebarSettings' -count=1 -timeout 30m
CGO_ENABLED=1 go test -race ./internal/app -timeout 30m
```

完整 App 软件渲染在 race 下可能很慢。超时不直接等于竞态，也不应反复增加超时掩盖挂起；检查具体测试、堆栈、执行平台和已有预算。定向 race 不能报告为完整 App race 通过。

## 7. 截图、原生与性能

```sh
frames_dir="$PWD/debug-frames/sidebar"
mkdir -p "$frames_dir"
NM_FRAMES="$frames_dir" CGO_ENABLED=0 go test ./internal/app -run '^TestSidebarSettingsFrames$' -count=1
```

生成后必须实际查看图片。布局检查至少包括明暗外观、常规与最小窗口；字体检查中日英及回退；交互检查焦点、鼠标、键盘、滚动、覆盖与恢复。主题切换有动画时用测试时钟等待稳定后截图。

macOS 歌词全屏原生回归通过仅测试程序导入的 MyGo 自动化插件读取已绘制的窗口，不给可见窗口补帧；测试使用合成歌词和临时用户目录。`MYGO_E2E` 让 App 测试主程序运行原生事件循环，`MYGO_AUTOMATION` 指定独立本地 socket，两者均不进入正式应用：

```sh
MYGO_E2E=1 MYGO_AUTOMATION=/tmp/nyago-lyrics-test.sock CGO_ENABLED=0 go test ./internal/app -run '^TestNativeFullScreenLyricsFirstFrame$' -count=5
```

软件渲染不证明 Metal、DPI、系统字体、透明窗口、Wayland 或原生全局快捷键正确。修改这些行为必须尽量实机检查；环境不足写明缺口，不能用交叉编译代替。

性能测量固定源码 SHA、硬件、OS、窗口尺寸、缩放、刷新率、歌曲/数据、输入序列和采样区间。分别记录 CPU、GPU、分配、I/O、帧间隔或空闲唤醒的指标及样本数量。测试总耗时不能作为运行时 GPU 性能对照，显示器刷新率也不是实测帧率。

## 8. 显式环境开关

| 开关 | 用途和限制 |
| --- | --- |
| `LIVE` | 允许标记的真实服务测试；通常还需特定会话或环境 |
| `NM_SESSION` | 某些真实接口测试的会话来源，不能输出或提交凭据 |
| `NM_LIVE_MUTATE` | 某些上报验证的额外真实写入开关；设置它不等于已获用户授权 |
| `NM_FRAMES` | 已存在目录，用于保存软件渲染截图 |
| `NM_LYRICS_CACHE` | 明确指定本地歌词缓存做只读结构检查 |
| `NM_LYRICS_REPORT` | 可选私有歌词抽样报告，不能直接公开 |
| `SPEECH_WAV`、`SPEECH_ROOT` | 真实转写样本和模型目录，可能触发模型下载 |
| `SHERPA_ONNX_DIR`、`SPEECH_MODEL` | 替代的本地库和模型路径 |
| `NM_SHOTS`、`NM_SCENE` | 仅 `nyago_debug` 构建包含的原生截图/场景入口，普通构建和 release 不编译；见 [debug_shots.go](../internal/app/debug_shots.go) |

真实收听上报可能改变账号历史；只在用户明确要求或接受这类验证时启用。默认不联网测试，不把真实请求当作测试替身的便捷替代。

## 9. CI 的实际分工

| 工作流 | 当前内容 | 不能据此声称 |
| --- | --- | --- |
| [test.yml](../.github/workflows/test.yml) / Test | Linux 构建、Go/MyGo vet、非 App internal 测试、两片 App 测试、非 App race 与选定 App 并发测试 | 已完整运行所有 App race 或所有平台原生界面 |
| [race.yml](../.github/workflows/race.yml) / Full UI Race | 六片完整 App race；main 的相关文件变更或手动触发；可指定完整 SHA | 任意分支 push 都会自动运行 |
| [go.yml](../.github/workflows/go.yml) / Build | macOS/Windows/Linux 的 amd64 与 arm64 安装产物；Linux 额外执行 `scripts/check_linux_packages.sh` 核对真实 DEB/RPM/Arch/归档的桌面动作、安装目录与命令，并运行 desktop-file-validate | 全部软件行为和签名运行已实测 |
| [release-notes.yml](../.github/workflows/release-notes.yml) / Release Notes | 发行说明、资产汇总/上传、Homebrew 同步工具测试与当前版本说明校验 | 已运行应用构建、竞态检查或真实跨仓库写入 |
| [release.yml](../.github/workflows/release.yml) / Release | 版本检查、三平台并行签名构建、单任务汇总/上传草稿 | 普通开发可以直接触发发行 |
| [homebrew.yml](../.github/workflows/homebrew.yml) / Update Homebrew | 正式发行后校验 DMG、更新 tap、在线审查及快进推送 | PR 检查会自动写 tap；已完成所有平台的实机播放验证 |

Test 与 Build 对所有分支的 push 和 PR 使用路径过滤；Full UI Race 只自动检查 main 的相关改动，不再每日定时执行。自动触发范围如下：

| 改动 | 自动运行 |
| --- | --- |
| 根目录 Go 文件、`internal/` 生产代码与内嵌资源、`icon/`、`go.mod` / `go.sum` | Build、Test、main 上的 Full UI Race |
| `*_test.go`、`testdata/`、共享测试辅助包 | Test、main 上的 Full UI Race；不打包安装产物 |
| `mygo.json` | Build、Test、main 上的 Full UI Race、Release Notes |
| `macos.entitlements.plist`、`scripts/check_linux_packages.sh` | Build |
| `scripts/sherpa/` | Test（包含工具编译与 vet；仍需本地运行该工具的测试） |
| App 测试分片脚本 | Test、main 上的 Full UI Race |
| 发行说明/资产/Homebrew 工具、其测试、Release 和 Update Homebrew 工作流 | Release Notes |
| 某个 CI 工作流自身 | 该工作流，便于验证 CI 配置修改 |
| README、`docs/`、贡献指南、AGENTS、CHANGELOG、代理技能等说明文件 | 不触发上述应用或工具 CI |

`internal/features/netease/keyring_help.md` 等通过 `go:embed` 打包的文件属于应用资源，不能按 Markdown 扩展名一律排除。新增源码根目录、构建输入或测试工具时，同步对应工作流的 push 和 PR 路径清单。Build、Full UI Race 与 Release Notes 保留手动运行；Release 仍只通过版本标签或显式手动发行触发。

使用 GitHub 原生路径过滤，规则见[官方工作流语法](https://docs.github.com/en/actions/reference/workflows-and-actions/workflow-syntax#onpushpull_requestpull_request_targetpathspaths-ignore)。如以后配置 PR 必需检查，应避免把会因路径过滤而整条跳过的工作流设为无条件必需，否则文档 PR 可能一直等待结果。

CI 并发策略可能取消旧 SHA 的任务。按本次完整 SHA、事件和目标仓库检查，不能引用旧提交的绿色结果；Test 不覆盖 `scripts/sherpa` 的全部测试，因此本地全量 `./...` 有额外价值。

## 10. 交付报告

列实际执行的命令、通过/失败/跳过、源码 SHA 和平台。环境导致失败时记录原因及后续成功验证，不将跳过项目合并为“全通过”。截图应标明外观和尺寸；CI 链接应对应准确提交。未执行的原生、签名或真实账户验证单独说明。
