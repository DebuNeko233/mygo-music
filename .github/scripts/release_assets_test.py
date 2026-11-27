"""
验证三平台独立产物汇总、损坏或缺失输入拒绝，以及草稿上传的完整性与顺序。
使用临时文件和 GitHub CLI 替身，不写真实发行或读取签名私钥。
"""

from concurrent.futures import ThreadPoolExecutor
import json
from pathlib import Path
import shutil
import subprocess
import tempfile
import unittest
from unittest.mock import patch

import release_assets as assets


class GitHub:
    def __init__(self, draft=True, missing=False):
        self.draft, self.missing = draft, missing
        self.files = {"install.sh": b"old script", "SHA256SUMS": b"old partial checksum"}
        self.calls = []
        self.fail_upload = False
        self.view_error = None

    def run(self, command, **kwargs):
        action = command[2]
        self.calls.append(command)
        if action == "view":
            if self.view_error or self.missing:
                return subprocess.CompletedProcess(command, 1, "", self.view_error or "release not found")
            body = {"isDraft": self.draft, "assets": [{"name": name} for name in self.files]}
            return subprocess.CompletedProcess(command, 0, json.dumps(body), "")
        if action == "create":
            assert "--draft" in command and "--verify-tag" in command
            self.missing, self.files = False, {}
        elif action == "edit":
            assert "--draft" in command
        elif action == "delete-asset":
            del self.files[command[4]]
        elif action == "upload":
            if self.fail_upload:
                raise subprocess.CalledProcessError(1, command)
            for path in map(Path, command[4:command.index("--clobber")]):
                if path.name.startswith("update-"):
                    manifest = json.loads(path.read_text())
                    name = manifest["url"].rsplit("/", 1)[1]
                    assert self.files[name] == (path.parent / name).read_bytes()
                    assert "SHA256SUMS" in self.files
                self.files[path.name] = path.read_bytes()
        else:
            raise AssertionError(action)
        return subprocess.CompletedProcess(command, 0, "", "")


class ReleaseAssetsTest(unittest.TestCase):
    def setUp(self):
        self.temp = tempfile.TemporaryDirectory()
        self.addCleanup(self.temp.cleanup)
        self.root = Path(self.temp.name)
        self.config = {"slug": "nyago-music", "version": "1.2.3", "updates": {"github": "example/music"}}
        self.inputs = self.root / "inputs"
        self.inputs.mkdir()
        self.output = self.root / "complete"
        self.notes = self.root / "notes.md"
        self.notes.write_text("Release notes")
        self.builds = {}
        for platform in assets.PLATFORMS:
            build = self.root / ("build-" + platform)
            build.mkdir()
            hashes = {}
            for arch in assets.ARCHES:
                target = f"{platform}-{arch}"
                folder = build / target
                folder.mkdir()
                names = [f"nyago-music-1.2.3-{target}.tar.gz", f"nyago-music-1.2.2-to-1.2.3-{target}.delta"]
                if platform == "darwin":
                    names += [f"nyago-music-1.2.3-{arch}.dmg"]
                elif platform == "windows":
                    names += [f"nyago-music-setup-1.2.3-{arch}.exe"]
                else:
                    rpm = "x86_64" if arch == "amd64" else "aarch64"
                    names += [f"nyago-music_1.2.3_{arch}.deb", f"nyago-music-1.2.3-1.{rpm}.rpm"]
                for name in names:
                    path = folder / name
                    path.write_bytes(name.encode())
                    hashes[name] = assets.checksum(path)
                def download(name):
                    return {"url": "https://github.com/example/music/releases/download/v1.2.3/" + name,
                            "size": len(name.encode()), "signature": "test-signature"}
                manifest = download(names[0]) | {"version": "1.2.3", "deltas": [download(names[1]) | {"from": "1.2.2"}]}
                (folder / f"update-{target}.json").write_text(json.dumps(manifest))
                # These common names must never collide or enter the release.
                (folder / "install.sh").write_text(platform)
                (folder / "nyago-music").write_text("loose executable")
            (build / "SHA256SUMS").write_text("".join(f"{value}  {name}\n" for name, value in hashes.items()))
            self.builds[platform] = build

    def staged(self):
        def stage(platform):
            assets.stage(self.config, self.builds[platform], self.inputs / ("release-" + platform), platform)
        with ThreadPoolExecutor(max_workers=3) as pool:
            list(pool.map(stage, assets.PLATFORMS))

    def complete(self):
        self.staged()
        assets.merge(self.config, self.inputs, self.output)

    def publish(self, github):
        with patch("release_assets.subprocess.run", side_effect=github.run):
            assets.publish(self.config, self.output, self.notes, "example/music", "v1.2.3")

    # Concurrent platform artifacts retain every install/update hash in one table.
    def test_parallel_builds_merge_without_lost_hashes_or_scripts(self):
        self.complete()
        hashes = assets.checksums(self.output)
        self.assertEqual(len(hashes), 20)
        self.assertEqual(len(list(self.output.iterdir())), 27)
        self.assertFalse(any((self.output / name).exists() for name in assets.SCRIPTS | {"nyago-music"}))
        for name, digest in hashes.items():
            self.assertEqual(digest, assets.checksum(self.output / name))
        for platform, build in self.builds.items():
            for original in build.glob("*/update-*.json"):
                self.assertEqual(original.read_bytes(), (self.output / original.name).read_bytes())

    # Missing platform output prevents creation of a partial release directory.
    def test_missing_platform_fails(self):
        self.staged()
        shutil.rmtree(self.inputs / "release-linux")
        with self.assertRaises(ValueError):
            assets.merge(self.config, self.inputs, self.output)
        self.assertFalse(self.output.exists())

    # Artifact transfer corruption must fail before publishing any file.
    def test_corrupt_artifact_fails(self):
        self.staged()
        path = next((self.inputs / "release-darwin").glob("*.dmg"))
        path.write_bytes(b"damaged")
        with self.assertRaisesRegex(ValueError, "checksum mismatch"):
            assets.merge(self.config, self.inputs, self.output)
        self.assertFalse(self.output.exists())

    # A common script or unexpected duplicate asset cannot sneak into the bundle.
    def test_unexpected_artifact_fails(self):
        self.staged()
        (self.inputs / "release-linux/install.sh").write_text("extra")
        with self.assertRaisesRegex(ValueError, "unexpected platform asset"):
            assets.merge(self.config, self.inputs, self.output)

    # A checksum for an absent asset must not enter the global release table.
    def test_unexpected_checksum_fails(self):
        self.staged()
        path = self.inputs / "release-linux/SHA256SUMS"
        path.write_text(path.read_text() + "0" * 64 + "  absent.deb\n")
        with self.assertRaisesRegex(ValueError, "unexpected platform checksum"):
            assets.merge(self.config, self.inputs, self.output)

    # An installer missing for either CPU architecture rejects that platform.
    def test_missing_installer_fails(self):
        (self.builds["windows"] / "windows-arm64/nyago-music-setup-1.2.3-arm64.exe").unlink()
        with self.assertRaisesRegex(ValueError, "missing or invalid"):
            assets.stage(self.config, self.builds["windows"], self.output, "windows")

    # A manifest may only reference this repository/version and real signed files.
    def test_invalid_manifest_fails(self):
        path = self.builds["linux"] / "linux-amd64/update-linux-amd64.json"
        original = json.loads(path.read_text())
        for key, value in [("version", "1.2.2"), ("url", "https://elsewhere.invalid/app"), ("size", 0), ("signature", "")]:
            with self.subTest(key=key):
                path.write_text(json.dumps(original | {key: value}))
                with self.assertRaises(ValueError):
                    assets.stage(self.config, self.builds["linux"], self.output, "linux")

    # Every advertised delta must exist, but versions without deltas remain valid.
    def test_optional_and_missing_deltas(self):
        folder = self.builds["darwin"] / "darwin-amd64"
        next(folder.glob("*.delta")).unlink()
        with self.assertRaisesRegex(ValueError, "missing or invalid"):
            assets.stage(self.config, self.builds["darwin"], self.output, "darwin")
        for arch in assets.ARCHES:
            path = self.builds["darwin"] / f"darwin-{arch}/update-darwin-{arch}.json"
            manifest = json.loads(path.read_text())
            manifest.pop("deltas")
            path.write_text(json.dumps(manifest))
        assets.stage(self.config, self.builds["darwin"], self.output, "darwin")
        self.assertFalse(list(self.output.glob("*.delta")))

    # The one publisher replaces a partial table and uploads manifests last.
    def test_publish_existing_draft_has_complete_hashes_and_order(self):
        self.complete()
        github = GitHub()
        self.publish(github)
        self.assertEqual(github.files, {p.name: p.read_bytes() for p in self.output.iterdir()})
        uploads = [call for call in github.calls if call[2] == "upload"]
        self.assertEqual(len(uploads), 2)
        self.assertTrue(all(Path(p).name.startswith("update-") for p in uploads[-1][4:uploads[-1].index("--clobber")]))

    # A missing release is created as a draft only after all assets validated.
    def test_create_draft(self):
        self.complete()
        github = GitHub(missing=True)
        self.publish(github)
        self.assertEqual(github.calls[1][2], "create")
        self.assertEqual(len(github.files), 27)

    # Published releases are immutable even when the same tag is dispatched again.
    def test_published_release_is_not_modified(self):
        self.complete()
        github = GitHub(draft=False)
        with self.assertRaisesRegex(ValueError, "published release"):
            self.publish(github)
        self.assertEqual([call[2] for call in github.calls], ["view"])

    # API failures are not mistaken for an absent release and do not create one.
    def test_api_error_does_not_create_release(self):
        self.complete()
        github = GitHub()
        github.view_error = "HTTP 403: Forbidden"
        with self.assertRaises(RuntimeError):
            self.publish(github)
        self.assertEqual([call[2] for call in github.calls], ["view"])

    # Unknown remote assets are preserved and reported before changing a draft.
    def test_unknown_remote_asset_stops_upload(self):
        self.complete()
        github = GitHub()
        github.files["unrelated-file.txt"] = b"owned by someone else"
        with self.assertRaisesRegex(ValueError, "unexpected existing draft asset"):
            self.publish(github)
        self.assertEqual([call[2] for call in github.calls], ["view"])

    # Failed package uploads cannot expose manifests pointing at absent files.
    def test_upload_failure_stops_before_manifests(self):
        self.complete()
        github = GitHub()
        github.fail_upload = True
        with self.assertRaises(subprocess.CalledProcessError):
            self.publish(github)
        self.assertFalse(any(name.startswith("update-") for name in github.files))

    # A wrong destination or corrupt merged package must never invoke GitHub.
    def test_invalid_publish_input_never_invokes_github(self):
        self.complete()
        github = GitHub()
        with patch("release_assets.subprocess.run", side_effect=github.run):
            with self.assertRaises(ValueError):
                assets.publish(self.config, self.output, self.notes, "other/repository", "v1.2.3")
            next(self.output.glob("*.exe")).write_bytes(b"damaged")
            with self.assertRaises(ValueError):
                assets.publish(self.config, self.output, self.notes, "example/music", "v1.2.3")
        self.assertEqual(github.calls, [])


if __name__ == "__main__":
    unittest.main()
