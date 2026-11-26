"""
验证发行下载链接随版本和仓库变化，并只包含当前版本的更新内容。
防止错误架构、包名或缺失更新章节进入自动生成的 Release 说明。
"""

import re
import unittest

from release_notes import render_notes, version_notes


class ReleaseNotesTest(unittest.TestCase):
    # 下载矩阵覆盖真实打包命名，切换版本后所有下载仍指向同一发行。
    def test_downloads_follow_release_configuration(self):
        config = {
            "version": "2.4.6",
            "slug": "example-player",
            "updates": {"github": "example/player"},
        }
        notes = render_notes(config, "## 未发布\n以后\n## 2.4.6\n修复\n### 详情\n说明\n## 2.4.5\n旧内容\n")
        urls = re.findall(r"\]\((https://[^)]+)\)", notes)
        base = "https://github.com/example/player/releases/download/v2.4.6/"
        expected = {
            "example-player-2.4.6-arm64.dmg",
            "example-player-2.4.6-amd64.dmg",
            "example-player-setup-2.4.6-arm64.exe",
            "example-player-setup-2.4.6-amd64.exe",
            "example-player_2.4.6_arm64.deb",
            "example-player_2.4.6_amd64.deb",
            "example-player-2.4.6-1.aarch64.rpm",
            "example-player-2.4.6-1.x86_64.rpm",
            "example-player-2.4.6-linux-arm64.tar.gz",
            "example-player-2.4.6-linux-amd64.tar.gz",
            "SHA256SUMS",
        }
        self.assertEqual(set(urls), {base + name for name in expected})
        self.assertEqual(len(urls), len(expected))
        self.assertTrue(notes.endswith("修复\n### 详情\n说明\n"))
        self.assertNotIn("旧内容", notes)
        self.assertNotIn("以后", notes)

    # 缺失或空更新章节必须阻止发行，不能拿上一版说明顶替。
    def test_missing_or_empty_version_is_rejected(self):
        for changelog in ("## 1.0.0\n旧版", "## 2.4.6\n\n## 1.0.0\n旧版"):
            with self.subTest(changelog=changelog), self.assertRaises(ValueError):
                version_notes(changelog, "2.4.6")


if __name__ == "__main__":
    unittest.main()
