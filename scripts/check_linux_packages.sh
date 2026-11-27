#!/usr/bin/env bash
# 验证实际 Linux 安装包的目录、桌面播放动作和 Arch 打包结果。
# 仅用于构建验证，不进入应用或安装包。
set -euo pipefail
stage=$(cd "${1:?usage: check_linux_packages.sh build/linux-amd64}" && pwd)
check_root=$(mktemp -d)
launcher_config="$PWD/mygo.json"
trap 'rm -rf "$check_root"' EXIT

check_actions() {
  desktop-file-validate "$1"
  python3 - "$1" "$2" "$launcher_config" <<'PY'
import configparser
import json
import sys
from pathlib import Path

entry = configparser.ConfigParser(interpolation=None)
entry.optionxform = str
entry.read(sys.argv[1], encoding="utf-8")
registered = [item for item in entry["Desktop Entry"]["Actions"].split(";") if item]
config = json.loads(Path(sys.argv[3]).read_text())
assert registered == [action["id"] for action in config["linux"]["actions"]], registered
mime_types = set(entry["Desktop Entry"]["MimeType"].split(";"))
for association in config["fileAssociations"]:
    assert association["mimeType"] in mime_types, association["mimeType"]
for scheme in config["urlSchemes"]:
    assert "x-scheme-handler/" + scheme in mime_types, scheme
for action in config["linux"]["actions"]:
    assert action["id"] in registered, action["id"]
    command = entry["Desktop Action " + action["id"]]["Exec"]
    expected = sys.argv[2] + " " + " ".join(action["args"])
    assert command == expected, (command, expected)
    assert "%U" not in command, command
PY
}

dpkg-deb --extract "$stage"/nyago-music_*.deb "$check_root/deb"
test -x "$check_root/deb/opt/Nyago Music/nyago-music"
grep -Fx 'Exec="/opt/Nyago Music/nyago-music" %U' "$check_root/deb/usr/share/applications/nyago-music.desktop"
test "$(dpkg-deb --field "$stage"/nyago-music_*.deb Package)" = nyago-music
check_actions "$check_root/deb/usr/share/applications/nyago-music.desktop" '"/opt/Nyago Music/nyago-music"'

mkdir -p "$check_root/rpm"
bsdtar -xf "$stage"/nyago-music-*.rpm -C "$check_root/rpm"
check_actions "$check_root/rpm/usr/share/applications/nyago-music.desktop" '"/opt/Nyago Music/nyago-music"'

# Run package() with the same source layout as makepkg, including spaced roots.
srcdir="$check_root/Arch source"
pkgdir="$check_root/Arch package"
mkdir -p "$srcdir" "$pkgdir"
tar -xzf "$stage"/nyago-music-*-linux-*.tar.gz -C "$srcdir"
check_actions "$srcdir/nyago-music.desktop" nyago-music
cp "$stage"/nyago-music-256x256.png "$stage"/nyago-music-512x512.png "$srcdir/"
source "$stage/PKGBUILD"
package
test -x "$pkgdir/opt/Nyago Music/nyago-music"
diff -r "$check_root/deb/opt" "$pkgdir/opt"
diff "$check_root/deb/usr/share/applications/nyago-music.desktop" "$pkgdir/usr/share/applications/nyago-music.desktop"
check_actions "$pkgdir/usr/share/applications/nyago-music.desktop" '"/opt/Nyago Music/nyago-music"'
printf '%s\n' 'Verified Linux package paths, playback actions and Arch payload'
