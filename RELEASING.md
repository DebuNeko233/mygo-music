# 独立发行

Nyago Music 的正式仓库是 `DebuNeko233/nyago-music`，开发主线是 `main`。应用安装包、签名更新与字幕库资源由本仓库提供。普通贡献见 [CONTRIBUTING.md](CONTRIBUTING.md)，项目边界见[独立维护与依赖](docs/dependencies-and-upstream.md)。

本指南用于已授权的发行操作；主线转正或普通推送不会自动创建应用版本标签。应用发行与字幕库资源发行分别管理。

## 1. 首次独立发行前

- 核对 `mygo.json` 的 `updates.github` 为本仓库，公钥对应本仓库独立私钥。
- Actions Secret `MYGO_UPDATER_PRIVATE_KEY` 必须存在。不要使用原项目的公钥配新私钥。
- 字幕清单引用的本仓库资源必须已发布；核对全部六个平台，不仅当前机器。
- 旧原项目/旧预览版需要用户手动安装独立构建完成首次迁移；此后由新地址和公钥管理更新。
- `v0.3.1` 起使用 `Nyago Music` 名称生成独立数据和缓存目录，不迁移旧文件。应用标识及凭据服务暂时保留；发行说明必须区分文件数据隔离与钥匙串登录。
- `v0.3.2` 起包名为 `nyago-music`、应用标识为 `io.github.debuneko233.nyagomusic`，保留 Nyago Music 数据目录，但需重新登录；发行说明必须包含旧安装身份的手动切换要求，详见[兼容说明](docs/dependencies-and-upstream.md#2-更新与用户数据兼容)。
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

通过 PR 将版本准备合入 main，不直接推送受保护主线。核对版本和变更说明位于同一待发行提交，再创建版本标签。下面的 `0.3.0` 仅为示例，必须替换成已决定的实际版本：

```sh
git tag v0.3.0
git push origin v0.3.0
```

## 4. 构建草稿与发布

[Release 工作流](.github/workflows/release.yml)由 `v*` 标签触发，检查配置、版本和变更说明，使用[说明生成器](.github/scripts/release_notes.py)生成下载表格与当前版本更新内容，创建草稿并上传 macOS、Windows、Linux 的产物与签名更新。手动触发也必须选择匹配实际版本的标签，不能在 main 上绕过版本检查。

项目仅提供安装包、应用内更新及 Homebrew 安装，不分发 `install.sh` 或 `install.ps1`。固定 MyGo CLI 仍会生成这些文件；普通 Build 在上传 artifacts 前删除脚本并过滤校验清单，Release 则在全部平台上传结束后执行 `finalize-release`，移除两个脚本资产并同步 `SHA256SUMS`。必须等待收尾任务成功，再公开草稿；本地使用 `-upload` 时也必须完成相同清理。不要删除更新归档、增量包或签名清单。

README 的安装入口固定指向 `releases/latest`。每次应用发行的说明以操作系统为列、架构为行，列出 DMG、Windows EXE、Linux DEB/RPM/TAR.GZ 的版本固定下载链接，并提供 Homebrew 指引；不会把旧版说明中的链接指向新版本。版本、文件名前缀和仓库从 mygo.json 读取，更新正文来自 CHANGELOG 对应章节。应用内更新清单仍使用原始 CHANGELOG 正文，不加入下载表格。

本地预览与验证：

```sh
python3 -m unittest discover -s .github/scripts -p '*_test.py'
python3 .github/scripts/release_notes.py > /tmp/nyago-release-notes.md
```

新增操作系统、架构或改变安装包命名时，必须同步生成器和测试，核对表中每个文件都出现在草稿资产中。更新现有 Release 说明时使用生成结果作为 `gh release edit --notes-file`，保留已经发布的包和签名文件。

macOS 当前采用 ad-hoc 签名并跳过公证；更新签名与 Apple Developer ID 是两件事。Windows 安装包使用 NSIS，普通 Build 工作流产物没有正式更新私钥签名，不能冒充已经验证的更新发行。

本地发布需已配置 GitHub 身份及匹配的更新私钥，按 CLI 的环境变量机制提供，不在文档或 shell 历史写真实值。构建命令例：

```sh
go tool mygo build -platform darwin/arm64,darwin/amd64 -sign - -skip-notarize
```

`-upload` 会实际创建/修改远端发行，只有在本次发行授权内使用。先检查草稿中的安装包、SHA256SUMS、各平台 update manifest、签名与 changelog；测试安装和升级路径后再公开草稿并设为 latest。

```sh
gh release edit v0.3.0 --repo DebuNeko233/nyago-music --draft=false --latest
curl -fsSL https://github.com/DebuNeko233/nyago-music/releases/latest/download/SHA256SUMS
```

正式发行前 `releases/latest` 可能不存在；不能让 README 的安装步骤回退到原项目。已公开的发行故障需要明确修复策略，不静默覆盖相同版本的已下载字节。

## 5. 字幕原生库资源

[生成器](scripts/sherpa/main.go)从 sherpa-onnx 的固定版本获取并校验依赖，打包六个平台库及 Silero VAD，生成 [natives.go](internal/features/speech/natives.go)。macOS 签名需要在 Mac 上完成；当前 ad-hoc 应用使用 `-sign -`。

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

核对准确版本和提交、最新版本页、安装包下载目标、签名更新清单、字幕库下载及账号/配置保留情况。发行成功不能只看上传步骤绿色。记录未验证的平台与问题，后续修复从本仓库 main 开始。


## 7. 同步 Homebrew tap

应用正式发行公开并完成下载验证后，更新 [DebuNeko233/homebrew-nyago-music](https://github.com/DebuNeko233/homebrew-nyago-music) 的 `Casks/nyago-music.rb`：版本号与 ARM/Intel DMG 各自的 SHA-256 必须来自同一正式版本。下载两份 DMG 计算摘要，并与该发行的 `SHA256SUMS` 比对。

`v0.3.1` 起 cask 的 `app` 和 CI 中的包路径为 `Nyago Music.app`。必须与对应版本的 DMG 内容一致，不能只修改 cask 路径却继续引用旧包。

检查应用实际名称、包文件名、最低系统版本与 cask 中的 `app`、`url`、`depends_on` 是否一致。完成 tap 的格式、在线审查、两个架构下载与 CI 安装验证后推送 main；具体命令见 [tap 维护说明](https://github.com/DebuNeko233/homebrew-nyago-music#维护与发版)。

应用内更新与 Homebrew cask 版本分别维护。`brew livecheck` 仅发现应用的新正式版，不会自动提交 cask；不要把字幕库资源发行、草稿或预览版同步成稳定版，也不要为更新 cask 改写已发布的应用 tag。
