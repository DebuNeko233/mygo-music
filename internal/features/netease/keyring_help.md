# 启用系统钥匙串（keyring）

本应用通过系统的「凭据存储 / 钥匙串」安全地保存网易云音乐的登录会话
（MUSIC_U、__csrf 两个 cookie）。它不会把密码或 cookie 写入任何可被
其他程序读取的明文文件。

当前系统上检测不到可用的凭据存储服务，所以每次重启应用都需要重新
扫码登录。安装并启用下面任意一个 keyring 服务即可解决。

## Linux：安装一个 Secret Service 实现

应用通过 freedesktop 的 Secret Service D-Bus 接口
（org.freedesktop.secrets）保存凭据。以下任意一种都可以提供这个接口，
按你的桌面环境选一个：

### GNOME / 通用：GNOME Keyring

    # Debian / Ubuntu
    sudo apt install gnome-keyring libsecret-1-0
    # Fedora
    sudo dnf install gnome-keyring libsecret
    # Arch / Manjaro
    sudo pacman -S gnome-keyring libsecret

安装后注销再登录（或重启）。大多数桌面会自动通过 PAM 在登录时解锁
keyring。用 `secret-tool store --label=test service test account test`
回车后输入任意内容再 Ctrl+D，不报「The name is not activatable」
即为可用。

### KDE Plasma：KWallet

    # Arch
    sudo pacman -S kwallet kwalletmanager libsecret

打开「系统设置 → KDE 钱包」，勾选「启用 KDE 钱包子系统」。然后在
终端启动 Secret Service 兼容层：

    /usr/lib/kf6/ksecretd &        # 或 /usr/bin/ksecretd

也可以在 ~/.config/kwalletrc 写入：

    [Wallet]
    Enabled=true
    First Use=false
    [KSecretD]
    Enabled=true

然后重新登录桌面。第一次有程序写入时 KWallet 会弹出向导让你创建
默认钱包，选 Blowfish 加密并设一个密码即可（不需要 GPG）。

### 任何桌面 / 窗口管理器：KeePassXC

安装 KeePassXC 后，在「工具 → 设置 → Secret Service 集成」里勾选
「启用 KeePassXC freedesktop.org Secret Service 集成」，并解锁一个
数据库作为存储后端。适合 Hyprland、Sway、i3 等没有完整桌面的环境。

### 验证

任意一种装好后，在终端执行：

    gdbus call --session \
      --dest org.freedesktop.DBus \
      --object-path /org/freedesktop/DBus \
      --method org.freedesktop.DBus.GetNameOwner \
      org.freedesktop.secrets

能返回一个 bus 名（如 ':1.123'）就说明 Secret Service 已经就位，
重启本应用后再登录一次，以后就不会掉登录了。

## macOS / Windows

macOS 使用系统「钥匙串访问」，Windows 使用「凭据管理器」，正常安装
的系统都有，无需额外配置。如果在这两个平台上看到这个提示，通常是
系统组件损坏，请尝试修复系统。
