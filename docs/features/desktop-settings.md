# 桌面集成与设置

本章覆盖主题、侧边栏、设置窗口、键盘、系统菜单、托盘、刘海播放器和更新。所有设置先区分持久化用户偏好、临时窗口状态与平台能力；不是所有开关都应该成为全局变量。

## 设置链路

[preferences](../../internal/preferences)定义配置、默认值、输入校正与批量保存；[settings 视图](../../internal/desktop/view/settings)负责控件与分组；[App settings](../../internal/app/settings.go)负责保存调度、平台副作用和窗口刷新。新增选项按[新增功能流程](../adding-features.md)完成整条链路。

设置加载应容忍旧配置缺字段。新字段的零值、缺失值与用户明确关闭是否相同，必须在设计时说明。需要归一化的范围放配置层；视图不能假定手工编辑的 JSON 一定合法。

调整设置后保存并刷新相关窗口，不能每帧写文件。独立设置窗口与主窗口访问同一配置所有者，但控件状态按窗口管理。搜索设置时，匹配结果必须保留可操作性和清晰分组；不能因过滤隐藏焦点目标后仍吞键盘操作。

回归：[preferences/settings_test.go](../../internal/preferences/settings_test.go)、[settings_test.go](../../internal/app/settings_test.go)、[settings_design_test.go](../../internal/app/settings_design_test.go)、[settings_mvvm_perf_test.go](../../internal/app/settings_mvvm_perf_test.go)。测试真实交互与保存结果，不写重复整张表单文案的测试。

## 侧边栏页面显示

用户入口集中在设置的“侧边栏”分组；统一说明“选择在侧边栏显示的页面”，各行只展示页面名与开关。不要在各功能页面重复加入“在侧边栏显示此入口”。

只有以下七项允许开关：每日推荐、最近播放、私人 FM、排行榜、播客、本地歌曲、音乐网盘。首页、搜索、资料库等固定入口不纳入该列表。

配置是 `HiddenSidebarPages` 内的七个明确布尔字段，`true` 表示隐藏，缺字段等于 `false`，所以旧安装升级后保持全部显示。界面开关表达“显示”，与存储字段相反。局部派生布尔值在 `OnClick` 中提交反值；不能依赖下一次构建读取已经重置的局部值。

隐藏只影响导航入口，不禁用功能、不关闭当前页、不改变历史、队列、播放位置或登录状态。用户仍可恢复入口；当前页入口被隐藏时继续正常工作。恢复和重启后的持久化都要验证。

实现入口：[sidebar.go](../../internal/desktop/view/settings/sidebar.go)、[shell](../../internal/desktop/view/shell)、[bindings_settings.go](../../internal/app/bindings_settings.go)。回归：[sidebar_settings_test.go](../../internal/app/sidebar_settings_test.go)、[sidebar_test.go](../../internal/app/sidebar_test.go)及 preferences 兼容测试。

## 外观、背景和字体

主题支持跟随系统及显式外观，强调色、界面字体、页面背景与播放页效果各有设置。字体选择不能覆盖歌词原文语言对应的 fallback；界面字体、应用内歌词字体和桌面歌词字体应按其已有作用域分别处理。

图片选择后先校验读取结果再发布，慢解码不进入 UI 构建。背景透明度与混色要覆盖浅色、深色、无图、坏图和尺寸改变；派生纹理或主色缓存失效须跟随源图。

多窗口变化通过统一状态和重绘机制传递。不能在某个窗口临时修改全局主题后忘记其他窗口。动效需要保持减少动态效果和暂停场景的可用性，稳定界面不持续请求无意义帧。

入口：[appearance.go](../../internal/app/appearance.go)、[backdrop.go](../../internal/desktop/view/playback/backdrop.go)。验证 [appearance_test.go](../../internal/app/appearance_test.go)、对应 backdrop/字体/封面测试与实际视觉输出。

## 快捷键、菜单与窗口

[desktop/shortcuts](../../internal/desktop/shortcuts)维护快捷键能力与绑定，[App hotkeys](../../internal/app/hotkeys.go)组装实际动作。新增快捷键先定义作用域：控件、当前页面、窗口还是系统全局。输入框、模态框、隐藏控件和失焦窗口都要避免误触。

空格播放、上一首/下一首、搜索焦点、后退/前进和系统媒体按钮应汇入同一命令。不要由菜单直接修改播放器内部字段。控件禁用状态与命令可执行性需一致。

原生菜单、文件对话框、托盘和多窗口生命周期由 App 组合。窗口关闭与退出应用不是同一操作；隐藏到托盘不能释放仍在播放的服务，真正退出则走统一 shutdown。托盘菜单读取当前状态，不长久捕获已经过期的队列或账号对象。

回归：[hotkeys_test.go](../../internal/app/hotkeys_test.go)、[space_test.go](../../internal/app/space_test.go)、[history_test.go](../../internal/app/history_test.go)、[minimize_test.go](../../internal/app/minimize_test.go)、[lifecycle_test.go](../../internal/app/lifecycle_test.go)。系统全局快捷键、托盘点击和窗口激活仍需目标平台原生验证。

## 刘海播放器与平台差异

刘海播放器是可选的快速控制与活动展示，设置涉及实际刘海和虚拟刘海、屏幕位置及菜单栏行为。macOS 实现与其他平台后备由平台文件隔离，不能在通用视图强行调用原生 API。

鼠标进入、离开、首次播放提示、菜单栏隐藏与恢复必须成对处理；多屏或无刘海设备不能出现无法关闭的覆盖层。播放视图状态与原生窗口状态分别由其所有者管理。

入口：[notch.go](../../internal/desktop/view/playback/notch.go)、[notch_darwin.go](../../internal/desktop/view/playback/notch_darwin.go)，回归 [notch_test.go](../../internal/app/notch_test.go)。原生屏幕几何和系统菜单栏行为不能仅凭软件渲染测试宣称已验证。

## 更新与发行边界

[updates.go](../../internal/app/updates.go)在运行前接入 MyGo 更新插件，连接自动检查、手动检查、自动下载和最近检查状态。测试通过可替换的适配验证行为，不在测试中直接启动真实更新窗口或更新应用。

更新源、版本和资源以 [mygo.json](../../mygo.json)为准。目前更新源指向 `zhangnjsd/mygo-music`。开发 fork 的普通代码推送不会自动把用户更新源改成 fork；若需要独立发行，必须单独决定版本、更新签名、发布仓库和迁移策略。

运行时设置修改、普通贡献推送和发行发布是不同授权范围。发行操作见 [RELEASING.md](../../RELEASING.md)，依赖与上游关系见[依赖与上游同步](../dependencies-and-upstream.md)。
