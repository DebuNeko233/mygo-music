# 开发环境、运行与构建

本文负责本地环境和命令入口；测试策略见[测试手册](testing.md)，提交规则见[贡献指南](../CONTRIBUTING.md)，正式发行见 [RELEASING.md](../RELEASING.md)。命令默认在目标工作树根目录执行。

## 环境与基线

```sh
git status --short --branch
git remote -v
git log -5 --oneline
go version
go env GOOS GOARCH CGO_ENABLED
go list -m github.com/egoist/mygo
```

当前 [go.mod](../go.mod)要求 Go `1.27.4`，并将 MyGo CLI 声明为 Go tool。以目标提交的模块文件为准，使用 `go tool mygo`，避免调用 PATH 上另一个版本的工具。

首次工具链和模块下载需要网络。失败时先区分 DNS、代理、权限和版本问题，不删除校验和或悄悄更改依赖。普通开发不修改全局 Go 配置。

项目通过 Go、动态库和 WASM 接入平台能力。软件渲染测试可使用 `CGO_ENABLED=0`；race 需要支持的原生工具链与 `CGO_ENABLED=1`。能运行测试不意味着具备图形会话、语音模型或打包工具。

## 分支与工作树

延续已有功能、PR 或用户指定分支时，在其原来源分支继续。正式开发主线为 `origin/main`；`refactor/app-feature-boundaries` 是已转正架构的历史开发分支，不再作为新功能默认基线。

新任务默认从本仓库最新 `origin/main` 开始；已有任务另有明确目标时按目标继续。本项目独立维护，不把原项目 main 作为默认开发或同步基线。

```sh
git fetch origin
# 基线按本次贡献目标替换。
git worktree add -b feat/example ../nyago-example origin/main
cd ../nyago-example
git status --short --branch
```

存在未提交修改时确认归属，必要时使用独立工作树。禁止重置、清除或擅自提交他人改动。工作树隔离是工具，不代表应自动创建公开 issue 或 PR。

## 运行与快速检查

```sh
go tool mygo dev
```

截图巡览与性能场景仅在显式 `nyago_debug` 构建中可用，例如 `GOFLAGS='-tags=nyago_debug' go tool mygo dev` 配合 `NM_SHOTS`、`NM_SCENE`。普通构建及 release 不设置这个标签，不包含这些环境入口和采样逻辑；单独设置 `NM_SHOTS` 不会启用它们。

这会打开原生桌面应用，需要图形会话。没有显示服务器的容器或无法访问窗口系统的环境不能据此验证界面行为。

```sh
go build ./...
go tool mygo vet .
CGO_ENABLED=0 go test ./...
```

普通回归保持 `LIVE`、`NM_LIVE_MUTATE`、`NM_FRAMES` 和个人语音测试变量未设置。仅在对应验证被需要时显式开启，详见[测试环境开关](testing.md)。

## 缓存、产物与重置

可使用任务专用构建缓存，而不更改全局环境：

```sh
mkdir -p .gocache
GOCACHE="$PWD/.gocache" CGO_ENABLED=0 go test ./internal/features/lyrics
```

[.gitignore](../.gitignore)已覆盖 `build/`、`.gocache/`、`debug-frames/`、`profiles/` 等产物。缓存、个人歌词报告、账号数据、安装包和一次性截图不提交；可公开回归样本放在所属包 `testdata/`。

[scripts/reset.sh](../scripts/reset.sh)清理真实应用数据，可能主动退出应用，不是测试前置步骤。确实要重置时先运行 `bash scripts/reset.sh --help`，确认开发版/安装版、登录与歌曲缓存的保留选项。不会为了获得干净测试状态清除个人数据。

## 平台构建

```sh
go tool mygo build
```

[mygo.json](../mygo.json)决定打包资源、标识和平台元数据。MyGo 打包还生成安装程序；普通 `go build` 只证明 Go 包可以编译。

```sh
CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build ./...
CGO_ENABLED=0 GOOS=windows GOARCH=amd64 go build ./...
```

以上不能报告为 Linux/Windows 实机验证。Windows 安装程序需要 NSIS，macOS 打包需要 Mac，Wayland portal、Linux 钥匙串及系统音量需要对应桌面服务。

### 安装路径与应用身份

显示名为 `Nyago Music`，包名为 `nyago-music`，应用 ID 为 `io.github.debuneko233.nyagomusic`。Linux 的 `linux.installDirName` 明确设为 `Nyago Music`；目录名与包名分开配置，打包逻辑由 MyGo CLI 维护。

| 平台/方式 | 新安装的默认目录 |
| --- | --- |
| macOS | `/Applications/Nyago Music.app`（无写入权限时脚本可回退到 `~/Applications`） |
| Windows | `%LOCALAPPDATA%\Programs\Nyago Music`（同 ID 重装保留已登记路径） |
| Linux DEB / RPM / PKGBUILD | `/opt/Nyago Music` |
| Linux install.sh | `~/.local/Nyago Music` |

Linux 可执行文件仍名为 `nyago-music`，桌面入口使用带引号的绝对路径。安装后的自更新使用实际安装目录。身份切换的旧包卸载、新包手动安装和重新登录要求见[依赖与兼容](dependencies-and-upstream.md#2-更新与用户数据兼容)。

已经发布的 `v0.3.1` 资产与 Homebrew cask 仍对应旧包名和标识；下一次正式发行需在新资产可下载后同步 cask 的下载文件名和 `uninstall quit`。普通分支推送不修改既有发行资产。

## macOS 本地测试包

没有个人证书时，可按 CI 使用临时签名：

```sh
revision=$(git rev-parse --short HEAD)
build_dir="build/test-$revision"
go tool mygo build -platform darwin/arm64 -sign - -skip-notarize -o "$build_dir"
app_path="$build_dir/darwin-arm64/Nyago Music.app"
codesign --verify --deep --strict --verbose=2 "$app_path"
hdiutil verify "$build_dir/darwin-arm64/"*.dmg
```

Intel 平台改为 `darwin/amd64`。使用已授权 Developer ID 时，以本机实际身份替换 `-sign -`；不复制参考材料中的个人证书指纹。`-skip-notarize` 表示没有 Apple 公证。

标签写入和 HE-AAC 解码涉及运行期代码生成，需额外执行：

```sh
python3 scripts/test_signed_download.py --output build/signed-download-check
```

[该脚本](../scripts/test_signed_download.py)构建、签名并执行下载与解码测试，覆盖冷/热缓存。默认临时签名；真实证书使用 `--identity`。静态验签、脚本执行和最终安装包实测分别记录；不可用时注明限制，不自行增加未经分析的系统权限。

## 排错与交付

| 现象 | 下一步 |
| --- | --- |
| `httptest` 无法 bind | 环境禁止本地监听，按授权机制开放回环测试端口后重跑，不删除测试 |
| 构建缓存写入失败 | 改用有权限的任务缓存目录，不改 HOME |
| 无原生窗口 | 核对图形会话与日志，区分启动失败和托盘驻留 |
| 全局快捷键失败 | 检查系统权限、portal 和桌面环境，保留可见错误 |
| Linux 无钥匙串 | 按登录帮助配置 Secret Service；不存在某条凭据与存储不可用不同 |
| 无法转写 | 核对模型、平台库、摘要和安装状态 |
| 签名或推送失败 | 核对身份与执行权限，保留已完成工作，不把工具失败描述为代码失败 |

交付测试包写明完整源码 SHA、是否包含未提交修改、平台/架构、签名方式、验签结果和实测范围。启动成功不意味着播放、下载和退出均已验证。
