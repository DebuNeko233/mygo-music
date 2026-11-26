"""
生成应用 Release 的下载矩阵、安装指引与当前版本更新说明。
链接取自发行配置，应用内更新清单继续使用 CHANGELOG 的原始版本章节。
"""

import json
from pathlib import Path


def version_notes(changelog, version):
    lines = []
    inside = False
    for line in changelog.splitlines():
        if line.startswith("## "):
            if inside:
                break
            inside = line.strip() == f"## {version}"
        elif inside:
            lines.append(line)
    notes = "\n".join(lines).strip()
    if not notes:
        raise ValueError(f"CHANGELOG 缺少非空版本章节：{version}")
    return notes


def render_notes(config, changelog):
    version = config["version"]
    slug = config["slug"]
    repository = config["updates"]["github"]
    base = f"https://github.com/{repository}/releases/download/v{version}"

    def link(label, filename):
        return f"[{label}]({base}/{filename})"

    rows = []
    for arch, label, rpm_arch in (
        ("arm64", "ARM64 / aarch64", "aarch64"),
        ("amd64", "x64 / x86_64", "x86_64"),
    ):
        mac = link("DMG", f"{slug}-{version}-{arch}.dmg")
        windows = link("EXE 安装包", f"{slug}-setup-{version}-{arch}.exe")
        linux = " · ".join(
            (
                link("DEB", f"{slug}_{version}_{arch}.deb"),
                link("RPM", f"{slug}-{version}-1.{rpm_arch}.rpm"),
                link("TAR.GZ", f"{slug}-{version}-linux-{arch}.tar.gz"),
            )
        )
        rows.append(f"| **{label}** | {mac} | {windows} | {linux} |")

    return "\n".join(
        [
            "## 下载与安装",
            "",
            "按操作系统和处理器架构选择：",
            "",
            "| 架构 / 操作系统 | macOS | Windows | Linux |",
            "| --- | --- | --- | --- |",
            *rows,
            "",
            "- **macOS**：Apple 芯片选 ARM64，Intel 芯片选 x64；打开 DMG，将应用拖入「应用程序」。要求 macOS 13 或更新版本。",
            "- **Windows**：Intel / AMD 处理器通常选 x64，Windows on ARM 设备选 ARM64；下载后运行 EXE 安装包。",
            "- **Linux**：按系统架构选择；Debian / Ubuntu 系选 DEB，Fedora / RHEL 系选 RPM，其他发行版可下载 TAR.GZ。",
            "",
            f"文件校验：{link('SHA256SUMS', 'SHA256SUMS')}。macOS 应用未经过 Apple 公证；若系统阻止打开，请在「系统设置 → 隐私与安全性」中允许运行。",
            "",
            "### Homebrew（macOS）",
            "",
            "```sh",
            "brew tap debuneko233/nyago-music",
            "brew trust --cask debuneko233/nyago-music/nyago-music",
            "brew install --cask debuneko233/nyago-music/nyago-music",
            "```",
            "",
            "Homebrew 7 需要先信任 cask；旧版没有 `trust` 命令时可跳过第二步。已安装用户运行 `brew update`，再运行 `brew upgrade --cask --greedy nyago-music`。",
            "",
            "## 更新内容",
            "",
            version_notes(changelog, version),
            "",
        ]
    )


if __name__ == "__main__":
    config = json.loads(Path("mygo.json").read_text(encoding="utf-8"))
    changelog = Path("CHANGELOG.md").read_text(encoding="utf-8")
    print(render_notes(config, changelog), end="")
