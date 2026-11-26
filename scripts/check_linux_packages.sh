#!/usr/bin/env bash
# 验证实际 Linux 安装包的目录、带空格的桌面命令和 Arch 打包结果。
# 仅用于构建验证，不进入应用或安装包。
set -euo pipefail
stage=$(cd "${1:?usage: check_linux_packages.sh build/linux-amd64}" && pwd)
check_root=$(mktemp -d)
trap 'rm -rf "$check_root"' EXIT

dpkg-deb --extract "$stage"/nyago-music_*.deb "$check_root/deb"
test -x "$check_root/deb/opt/Nyago Music/nyago-music"
grep -Fx 'Exec="/opt/Nyago Music/nyago-music" %U' "$check_root/deb/usr/share/applications/nyago-music.desktop"
test "$(dpkg-deb --field "$stage"/nyago-music_*.deb Package)" = nyago-music

# Run package() with the same source layout as makepkg, including spaced roots.
srcdir="$check_root/Arch source"
pkgdir="$check_root/Arch package"
mkdir -p "$srcdir" "$pkgdir"
tar -xzf "$stage"/nyago-music-*-linux-*.tar.gz -C "$srcdir"
cp "$stage"/nyago-music-256x256.png "$stage"/nyago-music-512x512.png "$srcdir/"
source "$stage/PKGBUILD"
package
test -x "$pkgdir/opt/Nyago Music/nyago-music"
diff -r "$check_root/deb/opt" "$pkgdir/opt"
diff "$check_root/deb/usr/share/applications/nyago-music.desktop" "$pkgdir/usr/share/applications/nyago-music.desktop"
printf '%s\n' 'Verified Linux package paths and Arch payload'
