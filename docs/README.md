# 开发文档入口

这套文档描述 MyGO Rev. 当前分层架构、功能契约和贡献规则，面向开发者、审查者及代码代理。项目是 Go 与 MyGo 驱动的原生桌面音乐客户端；业务音源限定为网易云音乐，另支持本地音频。本目录不保存一次性任务报告、聊天记录或未经实现的产品承诺。

## 阅读顺序

第一次参与，请依次阅读[贡献指南](../CONTRIBUTING.md)、[开发环境](development.md)、[架构](architecture.md)、[代码规范](coding-standards.md)和[测试手册](testing.md)。随后按任务阅读对应文档，不要求每次加载全部功能手册。

| 任务 | 补充阅读 |
| --- | --- |
| 增加页面、功能或持久化设置 | [新增功能流程](adding-features.md)及对应功能文档 |
| 调整控件、布局、动画、焦点 | [MyGo 界面开发](ui-development.md) |
| 新增请求、缓存、后台任务、账号操作 | [数据与生命周期](data-lifecycle.md) |
| 修改播放器、音频流、队列 | [播放与音频](features/playback.md) |
| 修改歌词、字体或字幕 | [歌词与字幕](features/lyrics-captions.md) |
| 修改本地曲库、临时打开或封面匹配 | [本地音乐](features/local-music.md) |
| 修改下载、离线、缓存迁移 | [下载与缓存](features/download-cache.md) |
| 修改登录、首页、搜索、歌单、播客或 FM | [浏览、账号与资料库](features/browsing-library.md) |
| 修改设置、侧栏、快捷键、窗口和托盘 | [桌面与设置](features/desktop-settings.md) |
| 更新依赖或同步上游 | [依赖与上游同步](dependencies-and-upstream.md) |
| 新增、移动或修订文档 | [文档维护](documentation.md) |
| 由代码代理执行工作 | [代理工作约定](agent-workflow.md)，从根目录 [AGENTS.md](../AGENTS.md)进入 |

全部功能的对应关系见[功能索引](features/README.md)。正式发行的操作入口仍为根目录 [RELEASING.md](../RELEASING.md)，普通贡献不执行发行流程。

## 规则如何理解

- **必须／禁止**是本仓库的审查约束，适用于所描述的代码和操作，不扩大任务授权。
- **建议**是默认做法，可以用具体的依赖、生命周期或验证理由选择其他实现。
- **示例**是说明路径，分支、平台、路径和账号必须按当前环境替换。
- **当前行为**是已有实现的契约。需求明确要求改变时，同步修改实现、测试和文档，说明兼容性影响。
- **尚未验证**与**测试通过**必须区分。编译、软件渲染、原生运行、性能测量和正式签名提供不同证据。

用户在当前任务中的明确范围和已有授权优先于默认工作流。技术事实核对源码、配置和工作流；文档过期时修正文档，不能为了符合旧文字倒退现有正确行为。

## 唯一维护位置

| 事项 | 事实或规则的来源 |
| --- | --- |
| 工具链、依赖、MyGo 替换 | [go.mod](../go.mod)、[go.sum](../go.sum) |
| 版本、更新源、资源和系统配置 | [mygo.json](../mygo.json)、[macOS 权限](../macos.entitlements.plist) |
| CI 任务和触发条件 | [.github/workflows](../.github/workflows) |
| 层次规则 | [architecture.md](architecture.md)，可执行检查为 [boundaries_test.go](../internal/architecture/boundaries_test.go) |
| 测试策略 | [testing.md](testing.md)，具体断言在被测代码所属包旁 |
| 代码风格与文件简介 | [coding-standards.md](coding-standards.md) |
| 功能的长期契约 | [features](features/README.md) |
| 文档新增、移动和失效处理 | [documentation.md](documentation.md) |
| 一次变更做了什么 | 提交、必要时的 PR 描述、测试日志，不塞入架构文档 |

AGENTS.md、技能文件和 CONTRIBUTING.md 是入口，不能各自维护一套不同的架构或测试规则。

## 完成标准

代码交付应回答：用户行为怎样变化、代码放在哪一层、状态和资源由谁拥有、失败及过期结果怎么处理、哪些测试实际通过、哪些平台尚未验证、文档是否与最终实现一致。仅能编译或只完成正常路径不等于完成。

文档交付应回答：说明能否在源码找到依据、读者能否走通链接、命令是否引用真实入口、旧规则是否移除，以及是否区分普通开发与真实账户操作、推送、PR、发行。
