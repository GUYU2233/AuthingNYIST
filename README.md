# AuthingNYIST · NYIST Connect

[![CI](https://github.com/GUYU2233/AuthingNYIST/actions/workflows/go.yml/badge.svg)](https://github.com/GUYU2233/AuthingNYIST/actions/workflows/go.yml)
[![Release](https://img.shields.io/github/v/release/GUYU2233/AuthingNYIST)](https://github.com/GUYU2233/AuthingNYIST/releases)
[![License: GPLv3](https://img.shields.io/badge/license-GPLv3-blue.svg)](LICENSE)

南阳理工学院校园网认证助手，支持 **nyist-tec / nyist-stu** 网络选择，提供 **Windows 原生图形界面、后台托盘** 与 **Linux 终端交互、systemd 自启动管理**。

> **本项目复刻自 [Palvef/AuthingNYIST](https://github.com/Palvef/AuthingNYIST)，并在其基础上继续开发。感谢 [Palvef](https://github.com/Palvef) 及上游所有贡献者在认证协议、命令行工具和持续维护方面的工作！**
>
> 本项目为非官方工具，与学校或认证服务提供方不存在隶属关系；请仅使用本人或获授权的账号，并遵守校园网管理规定。

## 功能概览

| 功能 | Windows | Linux |
| --- | --- | --- |
| nyist-tec / nyist-stu 选择 | 原生窗口下拉选择 | 终端交互菜单 |
| 账号密码与配置保存 | 当前用户 DPAPI 加密密码 | 目录 0700 / 文件 0600，密码明文 |
| 认证、状态检查、确认注销 | 窗口与托盘菜单 | 菜单与命令参数 |
| 自动连接与在线检查 | 可选启动自动连接 | 后台守护模式 |
| 自启动 | 当前用户登录启动 | systemd user / 显式 system 服务 |
| 后台与退出 | 最小化到托盘、关闭留后台、右键打开/退出 | 后台 PID、systemd 服务管理 |

原有 `auth-nyist` 命令行程序及一键脚本保留；新增应用入口为 `cmd/nyist`，两者配置独立。

## 下载

前往 **[Releases](https://github.com/GUYU2233/AuthingNYIST/releases)** 下载对应平台程序。新版本通过 `v*` 标签触发发布；尚未发布的最新提交可从通过的 **[CI 运行](https://github.com/GUYU2233/AuthingNYIST/actions/workflows/go.yml)** 下载构建产物（需要登录 GitHub）。

| 文件 | 适用平台 |
| --- | --- |
| `nyist-connect.exe` | Windows x64 原生 GUI |
| `nyist-connect-linux-amd64` | Linux x64 终端版 |
| `nyist-connect-linux-arm64` | Linux ARM64 终端版 |
| `auth-nyist.*` | 保留的经典 CLI，多架构构建 |

## Windows 快速使用

1. 双击 `nyist-connect.exe`。
2. 选择 **nyist-tec** 或 **nyist-stu**，填写账号和密码。
3. 点击 **连接**；按需勾选登录自启动、最小化启动、后台运行、启动后自动连接。
4. 后台运行时，关闭窗口会保留托盘；托盘左键或右键“打开窗口”可恢复界面，右键“退出”才会退出程序。

**退出程序不会注销现有认证。**“断开认证”需要确认，并停止本应用的保活。自启动仅在用户勾选并保存后启用，不会在首次打开时擅自配置。

默认配置：`%APPDATA%\AuthingNYIST\config.json`。密码使用当前 Windows 用户 DPAPI 加密，不可直接跨账户或电脑迁移。

## Linux 快速使用

```sh
chmod +x ./nyist-connect-linux-amd64
./nyist-connect-linux-amd64
```

菜单支持选择网络、隐藏密码输入、保存、认证、状态检查、确认注销、后台启动和 systemd 服务管理。ARM64 使用对应文件名。

```sh
./nyist-connect-linux-amd64 --connect
./nyist-connect-linux-amd64 --status
./nyist-connect-linux-amd64 --daemon
./nyist-connect-linux-amd64 --autostart enable
./nyist-connect-linux-amd64 --autostart status
./nyist-connect-linux-amd64 --autostart disable
```

- 可指定 `--config /绝对路径/config.json`，动作参数互斥。
- 默认配置：`~/.config/AuthingNYIST/config.json`（遵循 `XDG_CONFIG_HOME`）。Linux 密码为权限受限的明文，不要共享或提交。
- 默认 systemd **用户服务**依赖登录；启用/移除自启动不会立即启动/停止联网。立即运行可用菜单后台动作或 `systemctl --user start nyist-auth.service`。
- 管理员可明确配置 linger，或使用 root `--system` 管理系统服务；程序不会自动开启 linger。
- 没有 systemd 仍可交互使用或手动后台运行；注销前先停止其他保活实例。

完整操作、服务限制与配置说明见 **[NYIST Connect 使用说明](NYIST-CONNECT.md)**。

## 网络要求与安全边界

- 有无线网卡时，Windows 需要已有与 SSID 同名的 WLAN 配置；Linux 需要 NetworkManager / `nmcli` 和同名连接配置。请先在系统中手动连接过对应校园 Wi-Fi。无无线网卡时保留有线网络，只进行认证。
- **SSID 不等于账号后缀**，程序不会自行拼接 `@nyist-tec` / `@nyist-stu`；按学校提供的完整账号填写。
- 默认 HTTPS 并验证证书，不自动降级 HTTP；密码不写日志，不作为外部命令行参数传递。
- 当前存在其他在线账号时，拒绝自动覆盖或注销，避免误断网。
- 保活每 30 秒检查一次；连续 5 次暂时性失败后停止，认证拒绝立即停止，避免无限请求。长时间断网后需重新连接或重启后台。

## 从源码构建

建议使用受支持的 Go 稳定版，CI 使用 Go 1.26。原模块路径保留 `github.com/Palvef/AuthingNYIST`，以兼容已有内部导入和上游历史，不代表本仓库归属。

### Windows

```powershell
./scripts/build.ps1
./scripts/smoke-windows.ps1
```

构建脚本生成 manifest / icon 资源，运行测试，输出 Windows x64、Linux amd64 / arm64 到 `dist/`。首次构建需要联网下载依赖及固定版本资源生成器。

### Linux

```sh
go mod download
go test ./...
go vet ./...
go build -trimpath -o nyist-connect ./cmd/nyist
```

经典 CLI：

```sh
go build -o auth-nyist ./cli
./auth-nyist --help
```

一键脚本参考 [oneclick 使用说明](oneclick/README.md)，务必自行创建真实配置，**不要提交账号密码**。

## CI 与发布

- `CI`：Windows / Linux 原生测试与 `go vet`；Windows GUI 资源构建和 UI 冒烟测试；Linux amd64 / arm64 及经典 CLI 多架构构建；上传产物。
- `Release`：推送 `v*` 标签后复用相同测试与构建，再发布二进制、使用说明和 SHA-256 校验文件。
- 本地已验证 Windows GUI 与单元测试；Linux 运行时由 CI 的 Ubuntu 测试覆盖，**真实校园网络、桌面环境与 systemd 服务仍需在目标机器验证**。测试使用 mock / fake runner，不执行真实认证或服务变更。

## 来源、致谢与许可证

**该项目复刻自 [Palvef/AuthingNYIST](https://github.com/Palvef/AuthingNYIST)**。感谢原作者 **Palvef**、上游维护者及所有贡献者提供协议实现、问题修复、测试和文档。

同时保留上游对 [z4yx/GoAuthing](https://github.com/z4yx/GoAuthing) 的致谢，感谢其带来的启发。

本分支新增共享认证核心、Windows GUI / 托盘、Linux 交互菜单与服务管理，以及对应配置保护、测试和构建流程；保留上游历史和版权声明。

遵循 **GNU GPL v3**，详见 [LICENSE](LICENSE)。再分发与修改时请保留原有版权、许可和来源说明。
