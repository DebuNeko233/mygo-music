"""
验证正式发行到 Homebrew 的版本、双架构摘要和元数据同步，以及过期任务的跳过。
使用本地发行样本和 GitHub 替身，不读取部署密钥或推送真实 tap。
"""

import base64
import hashlib
import json
from pathlib import Path
import subprocess
import tempfile
import unittest
from unittest.mock import patch

import homebrew_update as update


CASK = '''# Keep this comment and the cleanup policy.
cask "nyago-music" do
  arch arm: "arm64", intel: "amd64"
  version "0.3.1"
  sha256 arm:   "ARM",
         intel: "INTEL"
  url "https://github.com/example/music/releases/download/v#{version}/old-#{version}-#{arch}.dmg"
  name "Old Music"
  app "Old Music.app"
  uninstall quit: "old.identifier"
  depends_on macos: :ventura
  zap trash: ["~/Library/Caches/Nyago Music"]
end
'''.replace("ARM", "a" * 64).replace("INTEL", "b" * 64)


class GitHub:
    def __init__(self, version="0.3.2"):
        self.tag = "v" + version
        self.latest = self.tag
        self.config = {"version": version, "slug": "nyago-music", "name": "Nyago Music",
                       "identifier": "io.github.example.nyagomusic", "updates": {"github": "example/music"}}
        self.payloads = {f"nyago-music-{version}-arm64.dmg": b"ARM disk image",
                         f"nyago-music-{version}-amd64.dmg": b"Intel disk image"}
        self.payloads["SHA256SUMS"] = "".join(
            f"{hashlib.sha256(data).hexdigest()}  {name}\n" for name, data in self.payloads.items()).encode()
        self.release = {"tag_name": self.tag, "draft": False, "prerelease": False}
        self.refresh_assets()
        self.api_calls = []
        self.downloads = 0

    def refresh_assets(self):
        self.release["assets"] = [{"name": name, "size": len(data), "state": "uploaded",
                                   "digest": "sha256:" + hashlib.sha256(data).hexdigest()}
                                  for name, data in self.payloads.items()]

    def api(self, endpoint):
        self.api_calls.append(endpoint)
        if endpoint.endswith("/releases/latest"):
            return {"tag_name": self.latest}
        if endpoint.endswith("/releases/tags/" + self.tag):
            return self.release
        if endpoint.endswith("/contents/mygo.json?ref=" + self.tag):
            return {"content": base64.b64encode(json.dumps(self.config).encode()).decode()}
        raise AssertionError(endpoint)

    def download(self, command, **kwargs):
        assert command[:3] == ["gh", "release", "download"]
        assert command[3] == self.tag
        root = Path(command[command.index("--dir") + 1])
        names = [command[i + 1] for i, value in enumerate(command) if value == "--pattern"]
        assert set(names) == set(self.payloads)
        for name in names:
            (root / name).write_bytes(self.payloads[name])
        self.downloads += 1
        return subprocess.CompletedProcess(command, 0)


class HomebrewUpdateTest(unittest.TestCase):
    def setUp(self):
        self.temp = tempfile.TemporaryDirectory()
        self.addCleanup(self.temp.cleanup)
        self.root = Path(self.temp.name)
        self.tap = self.root / "tap"
        (self.tap / "Casks").mkdir(parents=True)
        self.cask = self.tap / "Casks/nyago-music.rb"
        self.cask.write_text(CASK)
        self.readme = self.tap / "README.md"
        self.readme.write_text("当前正式版为 `0.3.1`。\n保留安装与卸载说明。\n")
        self.github = GitHub()
        self.calls = 0
        self.patch_api = patch("homebrew_update.gh_json", side_effect=self.github.api)
        self.patch_download = patch("homebrew_update.subprocess.run", side_effect=self.github.download)
        self.patch_api.start()
        self.patch_download.start()
        self.addCleanup(self.patch_api.stop)
        self.addCleanup(self.patch_download.stop)

    def prepare(self, tag=None):
        self.calls += 1
        return update.prepare("example/music", tag or self.github.tag, self.tap, self.root / f"downloads-{self.calls}")

    # Both CPU hashes and release-tag metadata update while cleanup remains intact.
    def test_updates_verified_release_and_preserves_cleanup(self):
        result = self.prepare()
        self.assertTrue(result["changed"])
        text = self.cask.read_text()
        self.assertIn('version "0.3.2"', text)
        for arch in ("arm64", "amd64"):
            self.assertIn(hashlib.sha256(self.github.payloads[f"nyago-music-0.3.2-{arch}.dmg"]).hexdigest(), text)
        self.assertIn('nyago-music-#{version}-#{arch}.dmg', text)
        self.assertIn('app "Nyago Music.app"', text)
        self.assertIn('uninstall quit: "io.github.example.nyagomusic"', text)
        self.assertIn('depends_on macos: :ventura', text)
        self.assertIn('zap trash: ["~/Library/Caches/Nyago Music"]', text)
        self.assertIn("保留安装与卸载说明。", self.readme.read_text())
        self.assertIn("当前正式版为 `0.3.2`。", self.readme.read_text())
        self.assertIn("repos/example/music/contents/mygo.json?ref=v0.3.2", self.github.api_calls)

    # Repeating the same verified release leaves both files byte-identical.
    def test_idempotent_retry(self):
        self.prepare()
        before = self.cask.read_bytes(), self.readme.read_bytes()
        self.assertFalse(self.prepare()["changed"])
        self.assertEqual(before, (self.cask.read_bytes(), self.readme.read_bytes()))

    # A resource tag, preview, or former latest release cannot update stable casks.
    def test_nonstable_or_old_release_is_skipped(self):
        for tag in ("sherpa-onnx-2.0.1", "v0.3.3-beta.1", "v0.3.0"):
            with self.subTest(tag=tag):
                self.assertFalse(self.prepare(tag)["eligible"])
        self.assertEqual(self.github.downloads, 0)
        self.assertEqual(self.cask.read_text(), CASK)

    # Even a manually selected numeric tag must be published and stable.
    def test_draft_or_prerelease_is_skipped(self):
        for flag in ("draft", "prerelease"):
            with self.subTest(flag=flag):
                self.github.release[flag] = True
                self.assertFalse(self.prepare()["eligible"])
                self.github.release[flag] = False
        self.assertEqual(self.github.downloads, 0)

    # Mislabelled release configuration cannot select another package or repository.
    def test_tag_configuration_mismatch_fails(self):
        self.github.config["version"] = "0.3.9"
        with self.assertRaisesRegex(ValueError, "configuration differs"):
            self.prepare()
        self.assertEqual(self.github.downloads, 0)

    # Both disk images and the checksum table must be fully uploaded first.
    def test_incomplete_assets_fail(self):
        self.github.release["assets"].pop()
        with self.assertRaisesRegex(ValueError, "missing complete"):
            self.prepare()
        self.assertEqual(self.github.downloads, 0)
        self.assertEqual(self.cask.read_text(), CASK)

    # Damaged asset bytes fail before any tap file is changed.
    def test_github_digest_mismatch_fails(self):
        self.github.payloads["nyago-music-0.3.2-arm64.dmg"] = b"corrupt image"
        with self.assertRaisesRegex(ValueError, "mismatch"):
            self.prepare()
        self.assertEqual(self.cask.read_text(), CASK)

    # A matching API digest is insufficient when the same-release checksum differs.
    def test_release_checksum_mismatch_fails(self):
        self.github.payloads["nyago-music-0.3.2-arm64.dmg"] = b"different image"
        self.github.refresh_assets()
        with self.assertRaisesRegex(ValueError, "SHA256SUMS"):
            self.prepare()
        self.assertEqual(self.cask.read_text(), CASK)

    # Repacking an already assigned stable version cannot silently replace its hash.
    def test_same_version_repack_is_rejected(self):
        self.prepare()
        before = self.cask.read_bytes()
        self.github.payloads["nyago-music-0.3.2-arm64.dmg"] = b"repacked image"
        self.github.payloads["SHA256SUMS"] = "".join(
            f"{hashlib.sha256(data).hexdigest()}  {name}\n" for name, data in self.github.payloads.items()
            if name.endswith(".dmg")).encode()
        self.github.refresh_assets()
        with self.assertRaisesRegex(ValueError, "same-version"):
            self.prepare()
        self.assertEqual(self.cask.read_bytes(), before)

    # Moving GitHub Latest backwards must never downgrade a newer tap version.
    def test_newer_tap_version_is_preserved(self):
        self.cask.write_text(CASK.replace('version "0.3.1"', 'version "0.4.0"'))
        self.assertFalse(self.prepare()["eligible"])
        self.assertEqual(self.github.downloads, 0)

    # Format drift is reported before either version document is overwritten.
    def test_readme_drift_does_not_partially_write_cask(self):
        self.readme.write_text("A differently organized README")
        with self.assertRaisesRegex(ValueError, "tap format changed"):
            self.prepare()
        self.assertEqual(self.cask.read_text(), CASK)

    # A newer release arriving during the Homebrew audit prevents the stale push.
    def test_latest_guard_after_new_release(self):
        self.prepare()
        self.github.latest = "v0.3.3"
        self.assertFalse(update.current_release("example/music", "v0.3.2"))


if __name__ == "__main__":
    unittest.main()
