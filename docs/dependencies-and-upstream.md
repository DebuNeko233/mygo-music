# 独立维护、依赖与外部修复

本项目由 `DebuNeko233/nyago-music` 独立维护，`main` 为正式开发主线。原项目只作为历史来源和可选择参考的代码来源，不再是自动同步、版本或发行的权威。独立维护不意味着删除历史作者、许可证或第三方依赖。

## 1. 项目边界与事实来源

| 项目 | 当前状态 | 核对来源 |
| --- | --- | --- |
| 正式仓库与开发主线 | `DebuNeko233/nyago-music` / `main` | 实际 remote 与 GitHub 默认分支 |
| 原项目 | `zhangnjsd/mygo-music`，保留来源致谢与历史 | README、Git 历史、许可证 |
| 应用更新源 | `DebuNeko233/nyago-music` | [mygo.json](../mygo.json) |
| 更新签名 | 本项目独立 Ed25519 公钥与对应 Actions Secret | mygo.json / `MYGO_UPDATER_PRIVATE_KEY` |
| 字幕原生库资源 | 本仓库 `sherpa-onnx-2.0.1` 资源发行 | [生成器](../scripts/sherpa/main.go)、[生成清单](../internal/features/speech/natives.go) |
| MyGo require | `github.com/egoist/mygo v0.10.0` | [go.mod](../go.mod) |
| 实际 MyGo 实现 | `github.com/DebuNeko233/mygo v0.10.1-0.20261128041207-f0871338f28e` | go.mod 的 replace |
| Go 工具链 | `go 1.27.4` | go.mod |

这些是当前仓库状态，不是“永远最新”的承诺。未来版本由本项目独立决定；原项目发布新版本不构成本项目必须同步的理由。历史功能分支保留记录，不作为新贡献默认基线。

GitHub 的 fork 标记与代码、资源、签名独立是不同事项。退出 fork 网络是平台管理操作，可能影响元数据；不能通过删除重建仓库、重写历史或丢弃发行资产来冒充普通代码迁移。是否已解除以 GitHub 实际状态为准。

## 2. 更新与用户数据兼容

未来构建只查询本仓库更新并信任本项目公钥。原项目客户端仍带原地址和公钥，不能通过本项目提交远程改变；首次迁移需要手动安装本项目的新构建。旧预览版也应核对其内置配置，不能假定自动迁移已经生效。

当前包名（slug）为 `nyago-music`，应用标识为 `io.github.debuneko233.nyagomusic`，显示名为 `Nyago Music`。数据、缓存和日志目录仍按显示名生成，已有 Nyago Music 数据继续读取；系统凭据按新应用标识隔离，不自动复制旧凭据，需要重新登录。文件关联和 Windows 卸载身份也使用新标识。

从旧包名 `mygo-cloudmusic` / 标识 `dev.egoist.cloudmusic` 切换时，先退出并卸载旧应用，再手动安装新包；普通卸载保留用户数据，不执行 reset 或 Homebrew `--zap`。Linux 包管理器把新 slug 视作另一个包，Windows 把新 ID 视作另一个卸载身份，不能并排安装到同一个目录；旧客户端的自动更新不承担这次身份迁移。macOS 保留 `Nyago Music.app` 名称，可替换旧应用，但需要重新登录和重新选择文件默认打开方式。

私钥只放受限本地备份或仓库 Actions Secret，不能提交到 Git。当前工作流在创建发行草稿前检查更新仓库是否匹配及密钥是否存在；构建工具进一步验证公私钥匹配。不得删除签名验证来让更新“先跑起来”。

字幕库来自 sherpa-onnx 和 ONNX Runtime 等独立依赖，打包资源由本仓库托管。修改清单前保证六个平台资源已准备并验证；不能把原项目 URL 替换为不存在的新 URL 后就宣称完成。模型供应商的下载地址仍属于第三方依赖，按其校验机制使用。

## 3. MyGo 框架维护

`require` 的官方模块路径与 `replace` 后实际源码不同：

```sh
go list -m -json github.com/egoist/mygo
go list -m -f '{{.Dir}}' github.com/egoist/mygo
```

当前固定提交包含 CLI 的 `linux.installDirName` 和 `linux.actions`，让 DEB、RPM、PKGBUILD、安装与更新脚本使用独立目录名并携带桌面播放动作。`linux.actions` 的参数分别转义，动作中不附带文件占位符。

macOS 窗口层记录进入及退出全屏的过渡状态：过渡期间由 AppKit 布局红绿灯。退出时暂时隐藏自定义按钮，完成或失败后在下一主线程事件内恢复可见性、完成原生布局，再恢复 `TrafficLightPosition`，避免露出系统默认靠边位置；重新开始过渡时延后恢复，关闭后丢弃任务，并保留 `HideWindowButtons` 的选择。仅用全屏样式位判断会在退出动画尚未结束时误套用窗口坐标。相关真实窗口回归见框架的 [traffic_lights_fullscreen_darwin_test.go](https://github.com/DebuNeko233/mygo/blob/0c8a96393478646b10418a67385633da46e9ca17/internal/e2e/traffic_lights_fullscreen_darwin_test.go)，覆盖过渡中缩放与可见性、取消和重新进入、关闭及显式隐藏选项、普通/内嵌标题栏及原生 UI/WebView 的完整全屏往返。

原生 UI 主机允许订阅 `WindowHidden` 的视图在不可见时完成资源清理，但不把该清理场景提交到表面或替换原生背景，保留最后可见画面供系统全屏动画使用；截图仍能读取清理后的最新场景，恢复可见时提交新内容。首次绘制不受此限制，未订阅可见性的视图仍保留输入驱动的绘制行为。

macOS 字形位图上下文同时设置允许与启用字体平滑，避免只请求平滑时普通字形、各亮度档位和显式加粗全部相同。常规文本保留系统平滑偏好，显式 `Thicken` 强制平滑，`Flat` 与彩色字形保持各自路径；字形度量与终端网格不变。权限名称测试保留上游 `bf9b5cbfd53cece4c5a74d9e0ba4972c651d0644` 对 macOS 27 的命名适配。

MyGo fork 保留歌词绘制层缓存能力（包括 `DrawCached`）和 macOS 帧就绪改动。项目独立不要求移除所有第三方来源或改写模块导入；禁止借此顺手删除已依赖的 fork 能力。

更新框架或回归官方需要证明 API 可用、缓存失效和资源释放正确、原生帧调度无可观察退化，并完成相关平台验证。性能对照固定硬件、场景和采样方法，不以测试总耗时证明 GPU 性能。具体界面生命周期见[UI 手册](ui-development.md)。

## 4. 选择性引入外部修复

不配置自动整仓同步或把原项目 main 当作本项目基线。需要外部修复时：

1. 说明本项目实际存在的问题，定位外部完整提交 SHA、版本与适用前提。
2. 比较现有实现，确认修复尚未存在，评估协议、数据格式和平台影响。
3. 按本架构移植行为：视图归 desktop/view，状态归 ViewModel，算法归服务，跨功能协调归 App。
4. 保留适用的版权、许可证与作者归属，并在提交中记录来源。
5. 增加或迁移有价值的行为回归，按[测试手册](testing.md)验证。
6. 更新长期功能契约；移植过程写提交说明，不把冲突流水账塞入架构文档。

不机械选择整份 ours/theirs；无文本冲突也可能绕过账号代次、复制状态或使用已过期 UI API。大规模合并必须有明确需求与审查依据，而非例行追踪原项目版本。

## 5. 普通依赖更新

每次更新给出具体原因，只更新相关模块并审查 go.mod/go.sum。不要凭记忆使用最新 API，优先核对固定模块源码与对应版本官方说明。MyGo 迁移工具先预览，再人工检查生命周期和身份。

音频/标签库更新检查格式夹具、PCM、许可与签名运行时；speech 更新检查 C ABI、模型版本、全部平台原生库及生成器。新增依赖说明现有能力为何不足，以及运行时、体积与许可成本。

版本、资源发布及独立更新密钥按 [RELEASING.md](../RELEASING.md)维护。主线推送不等于发布应用版本；正式发行仍需明确的发行操作。
