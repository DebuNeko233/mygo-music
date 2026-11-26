# 代码代理入口

适用于本仓库。详细开发规则统一维护在 [docs](docs/README.md)，不要在本文件复制第二套规范。用户当前任务的明确范围和已有授权优先于默认交付流程。

## 开始前阅读

1. [代理工作约定](docs/agent-workflow.md)：工作区、授权、执行与交付。
2. [架构](docs/architecture.md)：分层、依赖方向、状态与资源所有者。
3. [代码规范](docs/coding-standards.md)：文件组织、多行简介、错误和兼容。
4. [测试手册](docs/testing.md)：测试归属、必要检查和验证限制。

先核对实际源码、Git 状态和任务目标，再实施。简单任务只补读相关章节；不要为获取完整背景加载全部功能手册。

## 按任务补读

| 任务 | 文档 |
| --- | --- |
| 新功能、页面、设置 | [新增功能流程](docs/adding-features.md) + [功能索引](docs/features/README.md) |
| UI、动画、焦点、控件 | [界面开发](docs/ui-development.md) |
| 请求、账号、缓存、存储、关闭 | [数据生命周期](docs/data-lifecycle.md) |
| 依赖升级、框架切换、合并上游 | [依赖与上游](docs/dependencies-and-upstream.md) |
| 文档、代理规则或技能 | [文档维护](docs/documentation.md) |
| 环境、构建、签名验证 | [开发环境](docs/development.md) |
| 提交或贡献 | [CONTRIBUTING.md](CONTRIBUTING.md) |

Nyago Music 维护技能位于 [.agents/skills/mygo-maintenance/SKILL.md](.agents/skills/mygo-maintenance/SKILL.md)，按相同文档入口执行。

## 交付检查

按测试手册完成适用检查，核对最终 diff 与文档。报告实际通过项、未验证限制和提交/推送状态。已有明确推送授权就按目标执行；要求不提 PR 时不得创建 PR。发行不属于普通实现或推送流程。
