# NYIST Connect 校园网助手

新增跨平台入口 `cmd/nyist`；原有 `cli` 与 oneclick 脚本保留，不共用配置。

## Windows：直接打开

运行 `dist/nyist-connect.exe`（Windows x64，无需安装 Go）。

1. 下拉选择 **nyist-tec / nyist-stu**。
2. 输入校园网账号与密码，然后点击 **保存设置** 或 **连接**。
3. 按需勾选：
   - **登录 Windows 时自动启动**：当前用户登录后启动，不需要管理员权限。
   - **启动时最小化到托盘**：启动后隐藏主窗口；托盘左键或右键菜单可重新打开。
   - **后台运行**：关闭窗口时保留托盘与保活；不勾选则关闭窗口退出。
   - **启动后自动连接并保持在线**：启动后自动认证；连接后每 30 秒检查在线状态。
4. 托盘右键可打开窗口、连接、检查状态、断开认证或退出应用。

**退出不注销当前校园网认证**；“断开认证”需要确认，并会停止本应用的保活。仅勾选后台运行不会自动发起认证。保存设置不会即时重启保活，相关启动行为在下次启动时生效。

配置默认位于 `%APPDATA%\AuthingNYIST\config.json`。密码使用当前 Windows 用户的 DPAPI 加密保存；不要把配置复制到其他账户/电脑使用。自启动写入当前用户 Run 注册表项。移动程序前先取消自启动并保存，移动后再启用。

同一 Windows 会话只运行一个实例；再次打开会唤起现有窗口。高级启动参数：`--config "绝对路径"`、`--minimized`、`--startup`。

## Linux：终端菜单

选择 `dist/nyist-connect-linux-amd64` 或 `dist/nyist-connect-linux-arm64`，例如：

```sh
chmod +x ./nyist-connect-linux-amd64
./nyist-connect-linux-amd64
```

菜单可选择网络、输入隐藏密码、保存配置、认证、查状态、确认注销、后台运行及管理 systemd 自启动。手动后台启动会输出 PID，使用 `kill -TERM PID` 停止；systemd 后台可通过菜单单独停止。**注销前先停止其他保活实例，否则可能重新认证。**

```sh
./nyist-connect-linux-amd64 --connect
./nyist-connect-linux-amd64 --status
./nyist-connect-linux-amd64 --disconnect
./nyist-connect-linux-amd64 --daemon
./nyist-connect-linux-amd64 --autostart enable
./nyist-connect-linux-amd64 --autostart status
./nyist-connect-linux-amd64 --autostart disable
```

动作参数互斥；均可额外指定 `--config /绝对路径/config.json`。

- 默认配置 `$XDG_CONFIG_HOME/AuthingNYIST/config.json`，通常为 `~/.config/AuthingNYIST/config.json`。
- Linux 密码为权限受限的明文文件（目录 0700、文件 0600），不是加密保险箱；不要上传或共享。
- 默认 `systemd --user`，**依赖用户登录及 user manager**。启用/移除自启动不立即启动/停止联网；立即运行用菜单后台动作，或 `systemctl --user start nyist-auth.service`。
- 需要无人登录也运行时，可由管理员明确配置 linger，或以 root 使用 `--system --config /etc/nyist-connect/config.json --autostart enable`。本程序不会自动打开 linger。
- system unit 可用 `sudo systemctl start nyist-auth.service` 即时启动；服务 `Restart=no`，避免错误密码导致无限重启。
- 没有 systemd 时仍可使用菜单、保存配置和手动后台，自启动管理会明确报错。
- 移动可执行文件后需重新添加服务；服务引用添加时的绝对路径。

## 网络与安全边界

- Windows 有无线网卡时需要已有名为所选 SSID 的 WLAN 配置（先通过系统连接过对应网络）；Linux 有无线网卡时需要 NetworkManager / `nmcli`，并预建与 SSID 同名的连接配置。程序不自动创建含 Wi-Fi 密钥的配置。
- 无无线网卡时保留当前有线网络，只进行校园网认证。切换 SSID 是显式“连接”的一部分，会影响当前网络。
- SSID 不等于账号后缀；不会自行拼接 `@nyist-tec` 或 `@nyist-stu`。按学校提供的账号输入，需要明确 realm 时保留完整 `账号@realm`。
- 默认 HTTPS 并验证服务器证书；不会自动回退 HTTP。账号/密码不写日志、不通过命令行参数传给外部程序。
- 发现其他账号在线时拒绝自动覆盖/注销，避免误断网。需先在官方页面处理现有账号。
- 自动保活最多容忍连续 5 次暂时性失败（退避 2/4/8/16 秒）后停止；认证拒绝立即停止。长时间断网恢复后需重新点击连接/重启后台。非无限重试服务。
- Windows DPAPI 不防同用户恶意进程；Linux 文件权限不防 root。不要提交真实配置。

## 从源码构建

Windows PowerShell，安装 Go 后：

```powershell
./scripts/build.ps1
./scripts/smoke-windows.ps1
```

构建脚本生成 Windows manifest/icon 资源，执行 Windows 单元测试，并输出 Windows amd64 与 Linux amd64/arm64 到 `dist`。资源生成器固定为 `github.com/akavel/rsrc@v0.10.2`；首次需要下载模块。

Linux 本机构建与测试：

```sh
go test ./...
go build -trimpath -o nyist-connect ./cmd/nyist
```

## 验证范围

- Windows 单元测试、共享认证 mock 测试、DPAPI 保存/加载测试通过。
- Windows 原生 UI 自动化已验证控件、关闭留托盘、托盘回调打开窗口、关闭后台选项后退出；自动化不点击连接/断开。
- Linux amd64/arm64 二进制与 Linux 测试交叉编译通过；systemd 管理通过 fake runner 验证引用、失败回滚、不隐式 start/stop。
- 本轮未在真实 Linux 环境运行，也未执行真实 systemctl；未用 GUI 改动当前网络、注册表启动项或真实用户服务。Linux 运行时仍需目标机器验证。
