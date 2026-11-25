---
name: mygo-maintenance
description: Maintain this MyGO Rev. desktop music repository, including features, MyGo UI fixes, architecture changes, tests, and dependency or upstream updates. Use the repository docs for ownership, UI lifetimes, verification, and delivery rules.
---

# MyGO Rev. 维护

本技能是仓库文档的任务路由，不维护独立规则副本。路径均相对本文件；仓库根目录是 `../../..`。

## 执行顺序

1. 确认实际仓库、Git 状态、目标分支和用户已有授权。
2. 阅读 [AGENTS.md](../../../AGENTS.md)及[文档入口](../../../docs/README.md)，按任务补读下面的主题。
3. 从现有实现确认所有者和行为，再修改代码；具体约束遵循所链接的文档。
4. 按[测试手册](../../../docs/testing.md)完成相应验证，审查最终 diff，按已授权方式交付并报告限制。

## 按任务加载

| 任务 | 阅读 |
| --- | --- |
| 结构、功能落点与接线 | [架构](../../../docs/architecture.md)、[新增功能](../../../docs/adding-features.md) |
| 控件、窗口、动画、焦点和 UI 崩溃 | [界面开发](../../../docs/ui-development.md) |
| 异步、账号、缓存、存储和资源释放 | [数据生命周期](../../../docs/data-lifecycle.md) |
| 播放、歌词、本地音乐、下载或浏览 | [功能索引](../../../docs/features/README.md)中的对应手册 |
| 编码与文件简介 | [代码规范](../../../docs/coding-standards.md) |
| 环境与原生验证 | [开发环境](../../../docs/development.md) |
| MyGo 升级、切换官方或同步上游 | [依赖与上游](../../../docs/dependencies-and-upstream.md) |
| 文档或技能规则更新 | [文档维护](../../../docs/documentation.md) |
| 工作区、权限、提交和推送 | [代理工作约定](../../../docs/agent-workflow.md) |

使用框架 API 前，以 `go.mod` 的实际替换和 `go list -m -f '{{.Dir}}' github.com/egoist/mygo` 定位源码。不要凭 require 行假定正在使用官方实现；不要将新版本文档直接套到固定版本。

发生文档与源码冲突时核对事实并更新对应 docs，不在技能里新增临时例外。需要报告验证不足时说明实际未覆盖范围；不得用“编译通过”替代原生运行或性能证据。
