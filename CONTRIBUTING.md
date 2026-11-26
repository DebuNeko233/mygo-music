# 贡献指南

欢迎为 Nyago Music 提交修复、新功能、测试、平台适配和文档。完整操作、命令示例、分支命名及 PR 规范统一维护在[贡献流程与 PR 规范](docs/contributing.md)。

## 首次贡献顺序

1. **Fork 仓库**：将 [DebuNeko233/nyago-music](https://github.com/DebuNeko233/nyago-music) Fork 到自己的账号。
2. **克隆自己的 Fork**：确认 `origin` 指向自己的仓库。
3. **新建贡献分支**：从 `main` 创建描述性分支，例如 `fix/cache-directory`。
4. **按架构实施**：围绕一个完整问题修改，同步相关测试与文档。
5. **本地检查、测试并提交**：按改动范围完成验证，审查差异后提交。
6. **推送贡献分支**：推送到自己 Fork 中的同名分支。
7. **等待适用 CI**：核对最新提交 SHA，检查通过后再发起正式审查。
8. **创建 PR**：从自己的贡献分支向 `DebuNeko233/nyago-music` 的 `main` 提交，填写 [PR 模板](.github/PULL_REQUEST_TEMPLATE.md)。
9. **根据审查反馈更新**：继续提交到原分支，重新验证，由维护者审查合并。

详细步骤见[贡献流程](docs/contributing.md)。配置 `upstream`、同步主仓库和处理冲突放在该文档的后续贡献章节，不插入首次贡献的前三步。

纯文档和 PR 模板修改按路径规则可能不触发 CI：记录已完成的本地文档检查即可，不等待不存在的检查。需要提前讨论或 CI 因环境限制无法完成时，可以创建 Draft PR 并注明未完成项。触发范围与验证要求见[测试手册](docs/testing.md)。

## 实施前阅读

- [开发环境](docs/development.md)：工具链、运行、构建与调试。
- [架构](docs/architecture.md)与[代码规范](docs/coding-standards.md)：职责、依赖、文件简介和兼容性。
- [新增功能流程](docs/adding-features.md)与[功能索引](docs/features/README.md)：修改落点和行为契约。
- [测试手册](docs/testing.md)与[文档维护](docs/documentation.md)：适用检查及文档更新要求。

小修复可以直接准备补丁；涉及行为、存储格式、架构或依赖的大改动，先说明问题、方案、兼容影响和验证范围。代码代理从 [AGENTS.md](AGENTS.md)进入，详细授权边界见[代理工作约定](docs/agent-workflow.md)。

## PR 与交付

PR 标题采用 `类型: 简明结果`，例如 `fix: 修复切换缓存目录后设置未保存的问题`。正文说明问题与结果、主要改动与兼容性、实际验证；界面变化附截图或录屏，关联事项按需填写。完整格式、类型示例和模板使用方式见[PR 规范](docs/contributing.md#pr-标题与正文规范)。

维护者明确授权直接推送时，按指定分支执行，不强制创建 PR；要求先本地审阅或不推送时，保留本地修改并报告验证结果。普通贡献不创建版本标签或 Release，正式发行另见 [RELEASING.md](RELEASING.md)。
