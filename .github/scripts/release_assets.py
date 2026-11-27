"""
收集各平台签名产物，校验并汇总完整校验清单，由单一任务上传发行草稿。
构建目录与 Actions artifact 只读，安装脚本和松散应用文件不进入 Release。
"""

import argparse
import hashlib
import json
from pathlib import Path
import shutil
import subprocess


PLATFORMS = ("darwin", "windows", "linux")
ARCHES = ("amd64", "arm64")
SCRIPTS = {"install.sh", "install.ps1"}


def is_manifest(name):
    return name.startswith("update-") and name.endswith(".json")


def checksum(path):
    digest = hashlib.sha256()
    with path.open("rb") as stream:
        for chunk in iter(lambda: stream.read(1024 * 1024), b""):
            digest.update(chunk)
    return digest.hexdigest()


def checksums(root):
    result = {}
    for line in (root / "SHA256SUMS").read_text().splitlines():
        digest, name = line.split(maxsplit=1)
        if name in result or len(digest) != 64 or any(c not in "0123456789abcdef" for c in digest):
            raise ValueError("invalid or duplicate checksum: " + name)
        result[name] = digest
    return result


def regular(root, name):
    path = root / name
    if Path(name).name != name or path.is_symlink() or not path.is_file():
        raise ValueError("missing or invalid release file: " + str(path))
    return path


def collect(config, root, platform, nested=False):
    """Require both architectures and every file referenced by their manifests."""
    slug, version = config["slug"], config["version"]
    base = f'https://github.com/{config["updates"]["github"]}/releases/download/v{version}/'
    files = {}
    expected_hashes = checksums(root)
    for arch in ARCHES:
        target = f"{platform}-{arch}"
        directory = root / target if nested else root
        name = f"update-{target}.json"
        manifest_path = regular(directory, name)
        manifest = json.loads(manifest_path.read_text())
        if manifest["version"] != version:
            raise ValueError("wrong manifest version: " + name)
        files[name] = manifest_path
        archive = f"{slug}-{version}-{target}.tar.gz"
        downloads = [(archive, manifest)]
        for delta in manifest.get("deltas", []):
            downloads.append((f'{slug}-{delta["from"]}-to-{version}-{target}.delta', delta))
        for name, entry in downloads:
            path = regular(directory, name)
            if entry["url"] != base + name or entry["size"] != path.stat().st_size or not entry["signature"]:
                raise ValueError("invalid signed download: " + name)
            if name in files:
                raise ValueError("duplicate download: " + name)
            files[name] = path
        if platform == "darwin":
            installers = [f"{slug}-{version}-{arch}.dmg"]
        elif platform == "windows":
            installers = [f"{slug}-setup-{version}-{arch}.exe"]
        else:
            rpm_arch = "x86_64" if arch == "amd64" else "aarch64"
            installers = [f"{slug}_{version}_{arch}.deb", f"{slug}-{version}-1.{rpm_arch}.rpm"]
        for name in installers:
            files[name] = regular(directory, name)
    for name, path in files.items():
        if not is_manifest(name) and expected_hashes.get(name) != checksum(path):
            raise ValueError("checksum mismatch: " + name)
    return files


def write_assets(files, output):
    output.mkdir(parents=True, exist_ok=False)
    lines = []
    for name, source in sorted(files.items()):
        target = output / name
        shutil.copyfile(source, target)
        # Preserve MyGo's checksum format: installers, archives and deltas.
        if not is_manifest(name):
            lines.append(f"{checksum(target)}  {name}\n")
    (output / "SHA256SUMS").write_text("".join(lines))


def stage(config, source, output, platform):
    write_assets(collect(config, source, platform, nested=True), output)


def merge(config, source, output):
    files = {}
    if {path.name for path in source.iterdir()} != {"release-" + p for p in PLATFORMS}:
        raise ValueError("expected exactly three platform artifacts")
    for platform in PLATFORMS:
        root = source / ("release-" + platform)
        current = collect(config, root, platform)
        if {path.name for path in root.iterdir()} != set(current) | {"SHA256SUMS"}:
            raise ValueError("unexpected platform asset: " + platform)
        if set(checksums(root)) != {name for name in current if not is_manifest(name)}:
            raise ValueError("unexpected platform checksum: " + platform)
        if files.keys() & current.keys():
            raise ValueError("duplicate asset across platforms")
        files.update(current)
    write_assets(files, output)


def publish(config, source, notes, repository, tag):
    if repository != config["updates"]["github"] or tag != "v" + config["version"]:
        raise ValueError("release repository/tag differs from configuration")
    if not notes.read_text().strip():
        raise ValueError("empty release notes")
    files = {}
    for platform in PLATFORMS:
        files.update(collect(config, source, platform))
    if {path.name for path in source.iterdir()} != set(files) | {"SHA256SUMS"}:
        raise ValueError("unexpected release asset")
    if set(checksums(source)) != {name for name in files if not is_manifest(name)}:
        raise ValueError("unexpected global checksum")

    def gh(*args, capture=False):
        return subprocess.run(["gh", "release", *args, "--repo", repository],
                              check=not capture, capture_output=capture, text=True)

    existing = gh("view", tag, "--json", "isDraft,assets", capture=True)
    if existing.returncode == 0:
        release = json.loads(existing.stdout)
        if not release["isDraft"]:
            raise ValueError("refusing to modify a published release: " + tag)
        unexpected = {asset["name"] for asset in release["assets"]} - set(files) - {"SHA256SUMS"} - SCRIPTS
        if unexpected:
            raise ValueError("unexpected existing draft asset: " + ", ".join(sorted(unexpected)))
        gh("edit", tag, "--draft", "--title", tag, "--notes-file", str(notes))
        for asset in release["assets"]:
            if asset["name"] in SCRIPTS:
                gh("delete-asset", tag, asset["name"], "--yes")
    elif "release not found" in existing.stderr.lower() or "HTTP 404" in existing.stderr:
        gh("create", tag, "--draft", "--verify-tag", "--title", tag, "--notes-file", str(notes))
    else:
        raise RuntimeError("could not inspect release: " + existing.stderr)

    # Publish manifests only after every package and the global checksum landed.
    packages = sorted(name for name in files if not is_manifest(name)) + ["SHA256SUMS"]
    manifests = sorted(name for name in files if is_manifest(name))
    for names in (packages, manifests):
        gh("upload", tag, *(str(source / name) for name in names), "--clobber")


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--config", type=Path, default=Path("mygo.json"))
    commands = parser.add_subparsers(dest="command", required=True)
    for command in ("stage", "merge", "publish"):
        sub = commands.add_parser(command)
        sub.add_argument("--source", type=Path, required=True)
        if command == "publish":
            sub.add_argument("--notes", type=Path, required=True)
            sub.add_argument("--repository", required=True)
            sub.add_argument("--tag", required=True)
        else:
            sub.add_argument("--output", type=Path, required=True)
        if command == "stage":
            sub.add_argument("--platform", choices=PLATFORMS, required=True)
    args = parser.parse_args()
    config = json.loads(args.config.read_text())
    if args.command == "stage":
        stage(config, args.source, args.output, args.platform)
    elif args.command == "merge":
        merge(config, args.source, args.output)
    else:
        publish(config, args.source, args.notes, args.repository, args.tag)


if __name__ == "__main__":
    main()
