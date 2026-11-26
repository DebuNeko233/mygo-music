# 贡献流程与 PR 规范

本文是贡献操作与 PR 格式的详细维护位置，入口为 [CONTRIBUTING.md](../CONTRIBUTING.md)。按以下顺序完成首次贡献：**Fork → 克隆自己的 Fork → 新建分支 → 实施 → 本地验证并提交 → 推送贡献分支 → 等待适用 CI → 创建 PR → 根据审查更新**。

示例中的 `YOUR_USERNAME`、分支名和 `path/to/changed-file` 都是占位内容，请替换为自己的账号与实际改动。文档中的推送、PR 和合并命令用于说明流程，不代表代理已经获得执行授权。

## 首次贡献

### 1. Fork 仓库

打开 [DebuNeko233/nyago-music](https://github.com/DebuNeko233/nyago-music)，点击 **Fork**，在自己的 GitHub 账号下创建副本。首次贡献从主仓库当前 `main` 开始，复制默认分支即可。

确认自己的仓库地址是 `https://github.com/YOUR_USERNAME/nyago-music`。如果以前已经 Fork 过，先通过 GitHub 的同步入口更新自己的 `main`；遇到分叉或冲突时，按本文后面的“同步与后续贡献”处理，不覆盖已有工作。

### 2. 克隆自己的 Fork

克隆地址必须属于自己的账号：

```sh
git clone https://github.com/YOUR_USERNAME/nyago-music.git
cd nyago-music
git remote -v
```

确认 `origin` 指向自己的 Fork。下文的贡献分支将推送到这里；配置 `upstream` 留在后面的同步章节，首次操作不需要插入该步骤。

### 3. 新建贡献分支

从刚克隆的 `main` 创建描述性分支，不直接在 `main` 开发：

```sh
git switch main
git switch -c fix/cache-directory
git branch --show-current
git status --short
```

一个分支解决一个完整问题。分支名使用小写英文和连字符，建议采用以下前缀：

| 用途 | 示例 |
| --- | --- |
| 修复问题 | `fix/cache-directory` |
| 新增功能 | `feat/sidebar-settings` |
| 修改文档 | `docs/contribution-guide` |
| 重构实现 | `refactor/player-state` |
| 补充测试 | `test/session-save` |
| 修改 CI | `ci/source-path-filters` |

如果维护者明确指定继续已有分支，沿用指定分支。工作区中已有其他改动时先隔离自己的工作，必要时使用 worktree；不要清空工作区或覆盖别人的内容。

### 4. 按架构实施

先按[开发环境](development.md)准备工具链并确认相关基线测试可运行，再阅读[架构](architecture.md)、[代码规范](coding-standards.md)、[新增功能流程](adding-features.md)及对应[功能文档](features/README.md)。

小修复可以直接准备补丁。涉及用户行为、存储格式、架构或依赖的大改动，先通过 issue 或讨论说明问题、方案、兼容影响和验证范围，再展开实现。本项目的业务音源限定为网易云音乐，另支持本地音频；新增其他服务或改变发行源不能混进普通修复。

实现时明确服务、ViewModel、View 和 App 的职责，保持状态所有权及资源释放路径清楚。新增代码文件应有多行职责简介，生成文件的变更同步到生成器。新增功能、行为变化和结构调整需要同步测试与相关文档，具体要求引用对应手册，不在这里复制第二套技术规范。

提交错误报告或复现材料时，写清步骤、预期与实际结果、系统与架构、应用版本或提交 SHA。界面问题补窗口尺寸、缩放与截图；音频问题说明格式、来源及播放操作。日志和图片需脱敏，不上传 Cookie、token、个人曲库数据库、完整受版权保护的音乐或模型与编译产物。环境问题应单独记录，不用清理个人数据代替定位问题。

### 5. 本地检查、测试并提交

先运行最小相关测试，再完成[测试手册](testing.md)规定的范围。生产 Go 逻辑变更需要常规构建、vet 和全量回归；UI、并发和平台变化按手册补充相应检查。纯文档修改执行[文档校验](documentation.md)，不为了获得绿色状态启动无关应用测试。

测试必须反映真实行为。失败先定位原因，不能通过删掉有效断言或反复重跑掩盖问题。实际未验证的环境与平台要记入 PR；软件渲染、交叉编译和原生运行不能混为一谈。默认不运行会改变真实账号状态的服务测试。

本地验证完成后，审查改动并提交。下面的文件路径和提交标题都要换成当前任务的实际内容：

```sh
git status --short
git diff --check
git diff
git add -- path/to/changed-file
git diff --cached --check
git diff --cached
git commit -m "fix: 修复切换缓存目录后设置未保存的问题"
git rev-parse HEAD
```

新增文件也要打开检查，未跟踪文件不会自动出现在普通 `git diff` 中。显式选择暂存路径，排除凭据、日志和生成产物；一个提交应形成可理解的完整改动，不夹带无关格式化、依赖升级或发版操作。保留已有提交签名要求，签名失败时排查原因，不静默关闭签名或修改全局 Git 配置。

### 6. 推送到自己的贡献分支

第一次推送前，进入自己 Fork 的 **Actions** 页面，按提示启用工作流，确保后续贡献分支 push 能运行适用检查。GitHub 默认不会在 Fork 中自动运行工作流，见[官方事件说明](https://docs.github.com/en/actions/reference/workflows-and-actions/events-that-trigger-workflows#workflows-in-forked-repositories)。

再次确认分支和 `origin`，然后推送本次贡献分支：

```sh
git branch --show-current
git remote -v
git push -u origin fix/cache-directory
```

后续提交继续推送同一分支即可。如果推送被拒绝，先获取并检查远端新增提交，按后文整合；不要直接强推覆盖。普通贡献不推送版本标签、不创建 Release，也不需要配置发行私钥。

### 7. 等待该提交适用的 CI

在自己 Fork 的 Actions 页面查看贡献分支最新提交的检查，核对分支名和完整提交 SHA。正常情况下，适用 CI 通过后再创建准备好审查的 PR。

CI 按[测试手册中的路径规则](testing.md#9-ci-的实际分工)触发。不要把“没有检查”直接当作“检查通过”，先区分以下情况：

| 状态 | 应如何处理 |
| --- | --- |
| 排队或运行中 | 等待该次运行完成 |
| 代码、测试或构建失败 | 定位并修复，重新本地验证、提交、推送，再等新 SHA 的结果 |
| 旧提交的运行被取消 | 以当前最新 SHA 为准；最新 SHA 的适用检查被取消时仍需完成验证 |
| 纯文档或 PR 模板修改，没有匹配路径 | 正常；记录本地文档检查及“CI 按路径规则未触发”，无需空提交或改无关源码来启动 CI |
| 应有检查却未运行 | 检查 Fork 是否启用 Actions、事件与分支、路径规则，以及账号或仓库的运行限制 |
| CI 因基础设施、权限或服务故障无法完成 | 说明失败日志和限制；需要维护者帮助时先创建 Draft PR，不声称已经通过 |

Full UI Race 当前只对 `main` 的相关变更自动运行，贡献分支和 PR 不会自动运行这一整套检查；有并发改动时仍须按测试手册完成本地相关 race。需要完整 CI race 时由维护者安排适当的手动验证。

### 8. 向主仓库创建 PR

在 GitHub 打开贡献分支，选择创建 Pull Request，并确认比较方向：

| 字段 | 应选择 |
| --- | --- |
| base repository（接收改动） | `DebuNeko233/nyago-music` |
| base branch | `main`，或维护者明确指定的目标分支 |
| head repository（提供改动） | `YOUR_USERNAME/nyago-music` |
| compare / head branch | 本次贡献分支，例如 `fix/cache-directory` |

检查 Files changed 和提交列表只包含本次贡献，按下文填写标题与正文，再创建 PR。需要在实现或检查完成前讨论时可以创建 **Draft PR**，明确未完成项；完成适用验证后再请求正式审查。

PR 创建后，还需查看主仓库 PR 的 Checks。Fork 中的绿色 push 检查不能代替主仓库针对 PR 的检查。首次贡献等情况可能需要维护者批准运行，具体由仓库设置决定，见[GitHub 的 Fork 工作流审批说明](https://docs.github.com/en/actions/how-tos/manage-workflow-runs/approve-runs-from-forks)；等待审批不等于失败，也不要为此索要发行密钥或放宽工作流权限。

### 9. 根据审查反馈更新

在原贡献分支修改实现、回归测试和文档，重新执行适用本地检查，然后继续提交、推送同一分支。已有 PR 会随分支更新，不需要为同一个问题重复创建 PR。

对每条反馈说明修改位置、验证结果或不同意见。范围变化时重新整理 PR 标题与正文，让描述对应最终实现；补充最新 SHA 的 CI 结果，不沿用旧提交的绿色状态。不要仅为让检查通过而删除行为覆盖或重试到成功。

合并由维护者审查决定。PR 合并后可清理已经合并的个人贡献分支，再从更新后的主线开始下一项工作。正式发行由维护者另按 [RELEASING.md](../RELEASING.md)执行，普通贡献不修改发布仓库、密钥、版本号或用户更新路径。

## PR 标题与正文规范

### 标题

采用 `类型: 简明结果`，说明解决的问题或新增行为，不写 `update`、`fix bugs` 等无法辨认内容的标题。中文或清楚的英文均可，示例如下：

| 类型 | 用途 | 示例 |
| --- | --- | --- |
| `fix` | 修复错误 | `fix: 修复切换缓存目录后设置未保存的问题` |
| `feat` | 新增能力 | `feat: 支持隐藏指定侧边栏入口` |
| `refactor` | 保持行为的结构调整 | `refactor: 将歌词解析规则归入 lyrics 包` |
| `perf` | 有验证依据的性能优化 | `perf: 减少播放列表滚动时的重复封面解码` |
| `test` | 测试与测试工具 | `test: 补充退出时播放进度保存的回归` |
| `docs` | 文档与贡献模板 | `docs: 补充首次贡献流程` |
| `ci` | 自动检查和工作流 | `ci: 按源码路径触发构建检查` |
| `chore` | 不属于上述类型的维护 | `chore: 更新开发工具配置` |

提交标题建议沿用同样风格。一个 PR 应围绕一个可审查的问题；只有紧密相关的改动才合在一起，不以增加标题前缀代替拆分无关范围。

### 正文

使用仓库的 [PR 模板](../.github/PULL_REQUEST_TEMPLATE.md)。模板只提供填写结构，规则以本文和对应技术手册为准。内容可按改动规模精简，但问题、结果和验证证据不能省略。

| 部分 | 写什么 |
| --- | --- |
| 问题与结果 | 触发条件、原有表现、预期结果及修改后的行为 |
| 主要改动与兼容性 | 关键实现与责任归属、必要取舍、旧配置和数据及平台影响；没有兼容性影响就明确写“无” |
| 验证 | 实际命令与结果、系统和架构、最新提交 SHA、CI 链接或未触发原因、尚未验证的限制 |
| 界面变化 | 有可见 UI 改动时提供前后截图或录屏，标明主题、尺寸和必要操作；不适用时删除 |
| 关联事项 | 相关 Issue、外部修复或文档；没有则删除，只有确实解决的问题才使用关闭 Issue 的关键字 |

描述最终实现，不粘贴聊天记录、完整调试日志、废弃方案或敏感信息。不要只写“测试通过”而没有命令和范围，也不要将未运行、不适用、排队、取消等状态记成通过。

模板中的检查项只勾选已经完成的内容；不适用时写明原因，不机械勾选。模板进入主仓库默认分支后才能用于正常的自动填充，未合入时可以手动复制其内容，参见[GitHub PR 模板说明](https://docs.github.com/en/communities/using-templates-to-encourage-useful-issues-and-pull-requests/creating-a-pull-request-template-for-your-repository)。

## 同步与后续贡献

这里的 `upstream` 是 **Nyago Music 主仓库**，与本项目历史上基于的原项目无关。配置和同步放在首次贡献流程之后，供已有 Fork 更新基线或处理冲突时使用。

### 添加主仓库 remote

先查看现有 remote；只有尚未配置 `upstream` 时才执行添加：

```sh
git remote -v
git remote add upstream https://github.com/DebuNeko233/nyago-music.git
git remote -v
```

`origin` 应继续指向自己的 Fork，`upstream` 指向 `DebuNeko233/nyago-music`。如果已有同名 remote，先核对用途和地址，不直接覆盖。

### 从最新主线开始下一项贡献

先提交或妥善保存已有工作，确认工作区状态，再获取主仓库最新提交。可以直接从 `upstream/main` 新建下一条贡献分支：

```sh
git status --short
git fetch upstream
git switch --no-track -c fix/next-problem upstream/main
```

这样不需要改写已有贡献分支或个人 `main`。本地验证后仍用 `git push -u origin fix/next-problem` 推送到自己的 Fork。

### 更新正在审查的贡献分支

只有需要跟进主线或解决冲突时才整合；不要为同步而混入其他贡献。下面示例保留已有提交历史：

```sh
git switch fix/cache-directory
git status --short
git fetch upstream
git merge upstream/main
```

合并前应保证工作区干净。发生冲突时，查看 `git status`，逐个理解两侧变更、编辑冲突文件并检查结果，再显式 `git add` 已解决文件并执行 `git merge --continue`。暂时无法正确解决时用 `git merge --abort` 退出本次合并，再与维护者沟通；不要整体选择一侧掩盖冲突。

如果是自己的远端贡献分支出现并发提交，先执行 `git fetch origin`，查看并合并 `origin/fix/cache-directory`，不要直接强推。整合完成后重新测试、推送并等待适用 CI；不得用旧 SHA 的验证结果替代整合后的验证。

## 维护者与代码代理的交付例外

普通外部贡献采用上述 Fork 与 PR 流程。维护者在当前任务中明确授权直接推送指定分支时，按指定范围验证和推送，不额外创建 PR；指定已有分支时不机械新建分支。

明确要求“先给我看”“只在本地修改”或“不推送”时，停留在本地修改与验证阶段，提供可审阅的 diff 和结果，不创建远端分支、PR、标签或 Release。以前的推送授权不能覆盖当前任务的新限制。其他授权与资源边界见[代理工作约定](agent-workflow.md)。
