"""
从最新正式发行读取配置并校验两份 DMG，更新 tap 的 cask 与当前版本说明。
文件更新与 Git 推送分离，发布凭据仅由工作流的 checkout 和提交步骤使用。
"""

import argparse
import base64
import json
from pathlib import Path
import re
import subprocess

from release_assets import checksum, checksums


def gh_json(endpoint):
    result = subprocess.run(["gh", "api", endpoint], check=True, capture_output=True, text=True)
    return json.loads(result.stdout)


def stable_tag(tag):
    return re.fullmatch(r"v\d+\.\d+\.\d+", tag) is not None


def current_release(repository, tag):
    return stable_tag(tag) and gh_json(f"repos/{repository}/releases/latest")["tag_name"] == tag


def replace_one(text, pattern, value):
    text, count = re.subn(pattern, lambda match: value, text, flags=re.MULTILINE)
    if count != 1:
        raise ValueError("tap format changed; expected one match: " + pattern)
    return text


def cask_version(text):
    match = re.search(r'^  version "(\d+\.\d+\.\d+)"$', text, re.MULTILINE)
    if match is None:
        raise ValueError("missing stable cask version")
    return match[1]


def version_parts(version):
    return tuple(map(int, version.split(".")))


def ruby_string(value):
    return json.dumps(value, ensure_ascii=False).replace("#{", r"\#{")


def render_cask(text, config, hashes):
    version, slug = config["version"], config["slug"]
    if not stable_tag("v" + version) or re.fullmatch(r"[a-z0-9-]+", slug) is None:
        raise ValueError("invalid release version or slug")
    if version_parts(cask_version(text)) > version_parts(version):
        raise ValueError("refusing to downgrade the cask")
    if cask_version(text) == version:
        match = re.search(r'sha256 arm:\s+"([a-f0-9]{64})",\s+intel:\s+"([a-f0-9]{64})"', text)
        if match is None or (match[1], match[2]) != (hashes["arm64"], hashes["amd64"]):
            raise ValueError("same-version DMG hashes changed; refusing to overwrite")
    text = replace_one(text, r'^  version ".*"$', f'  version "{version}"')
    text = replace_one(text, r'^  sha256 arm:\s+"[a-f0-9]{64}",\n\s+intel:\s+"[a-f0-9]{64}"$',
                       f'  sha256 arm:   "{hashes["arm64"]}",\n         intel: "{hashes["amd64"]}"')
    repository = config["updates"]["github"]
    url = f'https://github.com/{repository}/releases/download/v#{{version}}/{slug}-#{{version}}-#{{arch}}.dmg'
    text = replace_one(text, r'^  url ".*"$', f'  url "{url}"')
    text = replace_one(text, r'^  name ".*"$', "  name " + ruby_string(config["name"]))
    text = replace_one(text, r'^  app ".*"$', "  app " + ruby_string(config["name"] + ".app"))
    return replace_one(text, r'^  uninstall quit: ".*"$', "  uninstall quit: " + ruby_string(config["identifier"]))


def prepare(repository, tag, tap, downloads):
    if not current_release(repository, tag):
        return {"changed": False, "version": "", "eligible": False}
    release = gh_json(f"repos/{repository}/releases/tags/{tag}")
    if release["draft"] or release["prerelease"] or release["tag_name"] != tag:
        return {"changed": False, "version": "", "eligible": False}
    contents = gh_json(f"repos/{repository}/contents/mygo.json?ref={tag}")
    config = json.loads(base64.b64decode(contents["content"]))
    if config["version"] != tag[1:] or config["updates"]["github"] != repository:
        raise ValueError("release configuration differs from tag/repository")
    if re.fullmatch(r"[a-z0-9-]+", config["slug"]) is None:
        raise ValueError("invalid released package slug")
    cask = tap / "Casks/nyago-music.rb"
    original = cask.read_text()
    if version_parts(cask_version(original)) > version_parts(config["version"]):
        return {"changed": False, "version": config["version"], "eligible": False}

    names = {arch: f'{config["slug"]}-{config["version"]}-{arch}.dmg' for arch in ("arm64", "amd64")}
    selected = ["SHA256SUMS", *names.values()]
    remote = {asset["name"]: asset for asset in release["assets"]}
    if any(name not in remote or remote[name]["state"] != "uploaded" for name in selected):
        raise ValueError("release is missing complete DMG/checksum assets")
    downloads.mkdir(parents=True, exist_ok=False)
    command = ["gh", "release", "download", tag, "--repo", repository, "--dir", str(downloads)]
    for name in selected:
        command.extend(["--pattern", name])
    subprocess.run(command, check=True)
    for name in selected:
        path, asset = downloads / name, remote[name]
        if path.stat().st_size != asset["size"]:
            raise ValueError("release asset size mismatch: " + name)
        if asset.get("digest") and asset["digest"] != "sha256:" + checksum(path):
            raise ValueError("release asset digest mismatch: " + name)
    expected = checksums(downloads)
    hashes = {arch: checksum(downloads / name) for arch, name in names.items()}
    if any(expected.get(names[arch]) != digest for arch, digest in hashes.items()):
        raise ValueError("DMG differs from release SHA256SUMS")

    updated = render_cask(original, config, hashes)
    readme = tap / "README.md"
    before = readme.read_text()
    after = replace_one(before, r"当前正式版为 `\d+\.\d+\.\d+`。", f'当前正式版为 `{config["version"]}`。')
    # Compute and validate both replacements before writing either file.
    changed = updated != original or before != after
    if changed:
        cask.write_text(updated)
        readme.write_text(after)
    return {"changed": changed, "version": config["version"], "eligible": True}


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("command", choices=("prepare", "guard"))
    parser.add_argument("--repository", required=True)
    parser.add_argument("--tag", required=True)
    parser.add_argument("--tap", type=Path)
    parser.add_argument("--downloads", type=Path)
    parser.add_argument("--output", type=Path, required=True)
    args = parser.parse_args()
    if re.fullmatch(r"[A-Za-z0-9_.-]+/[A-Za-z0-9_.-]+", args.repository) is None:
        parser.error("invalid repository")
    if args.command == "prepare":
        if args.tap is None or args.downloads is None:
            parser.error("prepare requires --tap and --downloads")
        result = prepare(args.repository, args.tag, args.tap, args.downloads)
    else:
        result = {"current": current_release(args.repository, args.tag)}
    with args.output.open("a") as stream:
        for key, value in result.items():
            stream.write(f"{key}={str(value).lower() if isinstance(value, bool) else value}\n")
    print(json.dumps(result))


if __name__ == "__main__":
    main()
