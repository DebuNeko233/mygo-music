# 独立发行

Nyago Music 的正式仓库是 `DebuNeko233/nyago-music`，开发主线是 `main`。应用更新、安装脚本与字幕库资源由本仓库提供。普通贡献见 [CONTRIBUTING.md](CONTRIBUTING.md)，项目边界见[独立维护与依赖](docs/dependencies-and-upstream.md)。

本指南用于已授权的发行操作；主线转正或普通推送不会自动创建应用版本标签。应用发行与字幕库资源发行分别管理。

## 1. 首次独立发行前

- 核对 `mygo.json` 的 `updates.github` 为本仓库，公钥对应本仓库独立私钥。
- Actions Secret `MYGO_UPDATER_PRIVATE_KEY` 必须存在。不要使用原项目的公钥配新私钥。
- 字幕清单引用的本仓库资源必须已发布；核对全部六个平台，不仅当前机器。
- 旧原项目/旧预览版需要用户手动安装独立构建完成首次迁移；此后由新地址和公钥管理更新。
- 应用标识与数据目录暂时保留以兼容安装。若要同时安装两个互不干扰的版本，另行设计标识及数据迁移。
- 旧历史版本记录保留，首次独立应用版本号应明确选择并高于待升级版本；不能把旧预览产物标成新主线版本。

GitHub fork 网络脱离可能影响平台元数据。执行前核对已存在发行、标签、资源、仓库配置及密钥备份；发生脱离后再次验证 Secret 和资源可用性。

## 2. 更新签名密钥

密钥长期稳定，普通发版不重新生成。遗失私钥会导致已安装客户端无法信任后续更新。

首次初始化可使用固定 MyGo CLI：

```sh
go tool mygo keygen -o signing-local/standalone-updates
gh secret set MYGO_UPDATER_PRIVATE_KEY --repo DebuNeko233/nyago-music \
  < signing-local/standalone-updates/mygo-update.key
```

将生成的 `mygo-update.pub` 内容填入 mygo.json 的 `updates.publicKey`。`signing-local/` 已忽略，私钥使用限制权限并另行安全备份。不要打印私钥、加入提交或写入命令参数；GitHub Secret 不能反向取回明文备份。

发行工作流只向构建进程提供私钥，MyGo 会检查其是否匹配配置公钥。工作流还检查更新仓库等于当前执行仓库，防止 fork 或旧配置误向另一项目发布。

## 3. 准备版本

确认主线、准确提交、工作区和 CI：

```sh
git status --short --branch
git fetch origin
git rev-parse HEAD
git diff --check
go build ./...
go tool mygo vet .
CGO_ENABLED=0 go test ./...
```

按[测试手册](docs/testing.md)补充受影响的 race、原生、视觉与签名下载验证。三平台打包通过不能替代各平台实机检查，交付必须说明差别。

更新 mygo.json 的 version 和 CHANGELOG.md 中对应的 `## 版本号` 章节。章节必须非空；子标题使用三级标题，否则提取逻辑会提前结束。发行说明包括直接提交的改动，不能只依赖 GitHub 从 PR 自动生成的列表。不要改写历史版本的作者与链接。

提交并推送 main。核对版本和变更说明位于同一待发行提交，再创建版本标签。下面的 `0.3.0` 仅为示例，必须替换成已决定的实际版本：

```sh
git tag v0.3.0
git push origin v0.3.0
```

## 4. 构建草稿与发布

[Release 工作流](.github/workflows/release.yml)由 `v*` 标签触发，检查配置、版本和变更说明，创建草稿并上传 macOS、Windows、Linux 的产物与签名更新。手动触发也必须选择匹配实际版本的标签，不能在 main 上绕过版本检查。

macOS 当前采用 ad-hoc 签名并跳过公证；更新签名与 Apple Developer ID 是两件事。Windows 安装包使用 NSIS，普通 Build 工作流产物没有正式更新私钥签名，不能冒充已经验证的更新发行。

本地发布需已配置 GitHub 身份及匹配的更新私钥，按 CLI 的环境变量机制提供，不在文档或 shell 历史写真实值。构建命令例：

```sh
go tool mygo build -platform darwin/arm64,darwin/amd64 -sign - -skip-notarize
```

`-upload` 会实际创建/修改远端发行，只有在本次发行授权内使用。先检查草稿中的安装包、SHA256SUMS、安装脚本、各平台 update manifest、签名与 changelog；测试安装和升级路径后再公开草稿并设为 latest。

```sh
gh release edit v0.3.0 --repo DebuNeko233/nyago-music --draft=false --latest
curl -fsSL https://github.com/DebuNeko233/nyago-music/releases/latest/download/SHA256SUMS
```

正式发行前 `releases/latest` 可能不存在；不能让 README 的安装步骤回退到原项目。已公开的发行故障需要明确修复策略，不静默覆盖相同版本的已下载字节。

## 5. 字幕原生库资源

[生成器](scripts/sherpa/main.go)从 sherpa-onnx 的固定版本获取并校验依赖，打包六个平台库及 Silero VAD，生成 [natives.go](internal/speech/natives.go)。macOS 签名需要在 Mac 上完成；当前 ad-hoc 应用使用 `-sign -`。

```sh
go run ./scripts/sherpa -sign - -out build/sherpa
```

生成后核对各资源的压缩/解压大小与 SHA-256，保留依赖许可证及来源说明。检查生成文件没有遗漏其他平台，运行 speech 与生成器测试。升级原生库需检查 C ABI 和真实加载，不只检查摘要。

上传到本仓库的资源发行，设置 prerelease 且 latest=false，使其不参与应用更新：

```sh
gh release create sherpa-onnx-2.0.1 --repo DebuNeko233/nyago-music \
  --target main --prerelease --latest=false \
  --title 'sherpa-onnx 2.0.1 runtime assets' \
  --notes-file /path/to/reviewed-runtime-notes.md build/sherpa/*
```

资源标签已存在时先比较已发布摘要，不能用 clobber 覆盖不同字节。需要更换已有字节时选择新资源标签并同步生成器/清单。资源准备可使用草稿，确认完整后公开；消费该 URL 的应用版本不得先于资源发布。

手动只打包部分平台会保留其他平台清单，但不自动生成其他平台文件。迁移到新的托管仓库时必须准备全部清单资源，不能凭部分平台命令就完成迁移。

## 6. 发行后核对

核对准确版本和提交、最新版本页、安装脚本下载目标、签名更新清单、字幕库下载及账号/配置保留情况。发行成功不能只看上传步骤绿色。记录未验证的平台与问题，后续修复从本仓库 main 开始。
