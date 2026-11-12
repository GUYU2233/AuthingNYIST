# AuthingNYIST · NYIST Connect

[![CI](https://github.com/GUYU2233/AuthingNYIST/actions/workflows/go.yml/badge.svg)](https://github.com/GUYU2233/AuthingNYIST/actions/workflows/go.yml)
[![Release](https://img.shields.io/github/v/release/GUYU2233/AuthingNYIST)](https://github.com/GUYU2233/AuthingNYIST/releases)
[![License: GPLv3](https://img.shields.io/badge/license-GPLv3-blue.svg)](LICENSE)

南阳理工学院校园网认证助手，支持 **nyist-tec / nyist-stu** 网络选择，提供 **Windows 原生图形界面、后台托盘** 与 **Linux 终端交互、systemd 自启动管理**。

> **本项目复刻自 [Palvef/AuthingNYIST](https://github.com/Palvef/AuthingNYIST)，并在其基础上继续开发。感谢 [Palvef](https://github.com/Palvef) 及上游所有贡献者在认证协议、命令行工具和持续维护方面的工作！**
>
> 本项目为非官方工具，与学校或认证服务提供方不存在隶属关系；请仅使用本人或获授权的账号，并遵守校园网管理规定。

## 功能概览

| 功能 | Windows | Linux | macOS |
| --- | --- | --- | --- |
| nyist-tec / nyist-stu 选择 | 原生窗口下拉选择 | 终端交互菜单 | 原生窗口 / 终端菜单 |
| 账号密码与配置保存 | 当前用户 DPAPI 加密密码 | 系统密钥环（Secret Service），不可用时回退到 0600 明文 | 文件 0600，密码明文，非 Keychain |
| 认证、状态检查、确认注销 | 窗口与托盘菜单 | 菜单与命令参数 | 原生窗口及 CLI |
| 自动连接与在线检查 | 可选启动自动连接 | 后台守护模式 | 连接后前台子进程保活 |
| 自启动 | 当前用户登录启动 / 经 UAC 授权的开机认证服务 | systemd user / 显式 system 服务 | 暂不支持 |
| 后台与退出 | 最小化到托盘、关闭留后台、右键打开/退出 | 托盘图标、后台 PID、systemd 服务管理 | 菜单栏后台；退出停止保活、不注销 |
| 睡眠唤醒 / 换网恢复 | 保活循环内自动恢复 | 保活循环内自动恢复 | 保活循环内自动恢复 |
| 失败通知 | 窗口状态栏与托盘提示 | 桌面通知（`--tray` 或 `--daemon`） | 窗口状态栏；终端版输出日志 |
| 诊断 | — | `--doctor` | `--doctor` |

统一使用 `nyist-connect`，应用入口为 `cmd/nyist`。经典 `auth-nyist` 及其一键脚本已移除；旧版配置不会自动迁移，请在账号设置中重新填写。

## 下载

前往 **[Releases](https://github.com/GUYU2233/AuthingNYIST/releases)** 下载对应平台程序。新版本通过 `v*` 标签触发发布；尚未发布的最新提交可从通过的 **[CI 运行](https://github.com/GUYU2233/AuthingNYIST/actions/workflows/go.yml)** 下载构建产物（需要登录 GitHub）。

| 文件 | 适用平台 |
| --- | --- |
| `nyist-connect.exe` | Windows x64 原生 GUI |
| `nyist-connect-linux-amd64` | Linux x64 终端版 |
| `nyist-connect-linux-arm64` | Linux ARM64 终端版 |

### 包管理器安装

发布流程还生成 Linux amd64 / arm64 的 **DEB（APT / dpkg）、RPM（DNF / yum / RPM / Zypper）、Pacman 包与 AUR 配方**，以及 **Flatpak、Snap、Homebrew、MacPorts、Nix** 的固定版本构建配方。提供可部署的 APT / RPM 软件源归档；不代表已上线官方软件源或商店。

安装命令、架构、签名、沙盒限制与构建步骤见 [包管理器与发行渠道](docs/PACKAGING.md)。原生包安装两个 CLI，不自动启用认证服务。Flatpak / Snap 默认发布配方，不把未构建的 manifest 当作安装包。

## Windows 快速使用

1. 双击 `nyist-connect.exe`。
2. 选择 **nyist-tec** 或 **nyist-stu**，填写账号和密码。
3. 点击 **连接**；**启动与后台** 采用纵向选项列表，按需设置登录系统时自动启动、启动时最小化到托盘、后台运行和自动连接校园网。
4. 后台运行时，关闭窗口会保留托盘；托盘左键或右键“打开窗口”可恢复界面，右键“退出”才会退出程序。

**退出程序不会注销现有认证。**“断开认证”需要确认，并停止本应用的保活。自启动仅在用户勾选并保存后启用，不会在首次打开时擅自配置。

Windows EXE 也提供后台网络/状态检测与启动版本检查；确认下载后校验 Release SHA256，保存到独立目录，退出托盘后手动替换，不静默覆盖运行中的程序或服务。详见 [网络识别与版本更新](NYIST-CONNECT.md#网络识别与版本更新)。

需要 **无人登录 Windows 也能认证** 时，在启动方式中勾选带管理员盾标的 **高优先级自启动**，保存并确认 UAC。它安装独立的 Windows 开机认证服务，不是 CPU 优先级或启动顺序保证；无线网络仍需预先配置为系统级、登录前可用。服务启用后由它统一处理认证，退出托盘不会停止服务；“登录后启动”只控制登录后的界面。开启、关闭和更新服务账号均需管理员授权，详见 [服务说明](NYIST-CONNECT.md#开机认证服务高优先级自启动)。

默认配置：`%APPDATA%\AuthingNYIST\config.json`。密码使用当前 Windows 用户 DPAPI 加密，不可直接跨账户或电脑迁移。

## macOS

面向 macOS 13 及以上系统，提供 Apple Silicon（arm64）和 Intel（amd64）两个架构的原生应用与 DMG 打包流程，界面包含网络选择、账号密码、连接、状态、确认注销以及菜单栏后台。macOS 版不支持 Windows 的高优先级开机服务，也未实现登录自启动。

- DMG：`NYIST-Connect-macos-arm64.dmg` / `NYIST-Connect-macos-amd64.dmg`；将应用拖入 Applications 后运行。Cask 配方不随 Release 分发，需要时在仓库内 `BUILD_EXTRAS=1` 运行打包脚本生成 `homebrew/Casks/nyist-connect.rb`，放进自己的 tap 后执行 `brew install --cask <tap>/nyist-connect`；官方仓库不托管公开 tap。
- 终端版：`nyist-connect-macos-arm64` / `nyist-connect-macos-amd64`。
- 密码按权限受限的明文配置存储（0600），**不是 Keychain 加密**；不要共享配置。
- Wi-Fi 需要先在系统中连接并保存对应校园网络；系统隐私/网络权限可能使 SSID 被隐藏，程序此时保守拒绝认证。仅手动连接不一定解决，仍需允许读取 SSID 或使用可验证的有线默认路由。
- 原生 Swift/DMG 必须在 macOS 上执行 `bash scripts/build-macos.sh`。GitHub CI 使用 macOS runner 打包后纳入发布附件；Windows/Linux 交叉编译只能生成 Go 内核，不能代替真实 DMG。
- 默认仅 ad-hoc 签名，未进行 Apple Developer ID 签名或公证；Gatekeeper 可能阻止运行。请先核验来源和校验和，不要全局关闭系统安全功能。

### macOS CI Runner

- **GitHub Actions**：CI 使用 GitHub 托管的 `macos-latest`，运行 Go 测试，并通过 Xcode/Swift 构建双架构 app/DMG；`macos-dmg` artifact 保留 14 天，发行包必须包含两个 DMG 后才允许发布。
- **GitLab CI**：需要自行注册 **macOS shell executor** Runner，设置标签 `macos`，安装 Go 1.26+ 和 Xcode Command Line Tools。确认 Runner 只运行可信代码后，在项目 CI/CD 变量中设置 `ENABLE_MACOS_RUNNER=true`，即可启用 `macos_package`，DMG 自动并入发行包。未启用时仅发布交叉编译的 macOS 终端版，不伪造 DMG，也不会等待不存在的 Runner。
- Runner 注册令牌请在 GitLab 设置页面填写，不要写入仓库。此处只提供流水线配置，不会在你的机器上自动安装或注册 Runner。

## Linux 快速使用

```sh
chmod +x ./nyist-connect-linux-amd64
./nyist-connect-linux-amd64
```

Linux 交互界面使用开源 **Charmbracelet Huh（基于 Bubble Tea）**，提供方向键选择、可搜索操作列表、带校验的账号表单及隐藏密码输入；支持保存、认证、只读状态检查、默认取消的登出确认、后台启动和 systemd 服务管理。Ctrl+C 取消并退出，不主动登出或停止保活；每次操作使用独立且有超时的上下文，避免一次中断导致后续操作全部失败。非终端输入不启动交互界面，请使用下述动作参数。ARM64 使用对应文件名。

```sh
./nyist-connect-linux-amd64 --connect
./nyist-connect-linux-amd64 --status
./nyist-connect-linux-amd64 --daemon
./nyist-connect-linux-amd64 --autostart enable
./nyist-connect-linux-amd64 --autostart status
./nyist-connect-linux-amd64 --autostart disable
./nyist-connect-linux-amd64 --doctor
./nyist-connect-linux-amd64 --tray
```

- 可指定 `--config /绝对路径/config.json`，动作参数互斥。
- 默认配置：`~/.config/AuthingNYIST/config.json`（遵循 `XDG_CONFIG_HOME`）。Linux 密码优先存入系统密钥环（freedesktop Secret Service，`secret-tool`），配置文件中只保留 `keyring:v1:` 引用；密钥环不可用（无桌面会话、无 Secret Service）时回退为 0600 权限的明文文件，并在保存后提示。密钥环被清空或换机后密码无法恢复，程序会保留账号并提示重新输入，不会用空密码认证。不要共享或提交配置文件。
- `--doctor` 只读诊断配置、密码是否就绪、网络选择与当前网络、默认路由、代理环境变量（只报名称不报值）、DNS、门户 TCP 可达性与保活服务状态；诊断不打印密码，不修改配置或服务。门户 TCP 连通只能算提示，TUN 可能在本地接受连接，不能据此判断在线。
- `--tray` 在系统托盘中显示状态并提供连接、断开、打开日志和退出，运行时同时执行保活。托盘用纯 Go D-Bus 实现（StatusNotifierItem），不需要 GTK 或 cgo。GNOME 自身没有托盘，需要安装 AppIndicator/KStatusNotifierItem 扩展；没有托盘宿主时命令会立即报错并建议改用 `--daemon`，不会注册一个看不见的图标。
- 保活失败与恢复会发送桌面通知（`org.freedesktop.Notifications`，缺失时退回 `notify-send`）；通知失败不影响保活本身。
- 默认 systemd **用户服务**依赖登录；启用/移除自启动不会立即启动/停止联网。立即运行可用菜单后台动作或 `systemctl --user start nyist-auth.service`。
- 管理员可明确配置 linger，或使用 root `--system` 管理系统服务；程序不会自动开启 linger。
- 没有 systemd 仍可交互使用或手动后台运行；注销前先停止其他保活实例。

完整操作、服务限制与配置说明见 **[NYIST Connect 使用说明](NYIST-CONNECT.md)**。

## CLI 自动检测、直连认证与更新检查

新版 Linux / macOS 终端入口默认采用 `auto` 网络模式：读取当前已连接的 `nyist-tec` 或 `nyist-stu`，不扫描、不创建 Wi-Fi 配置，也不擅自切换网络。有线连接保留原路由；连接其他 Wi-Fi 或无法可靠读取 SSID 时给出诊断。需要主动选择已有校园 Wi-Fi 配置时，可明确传入 `--network nyist-tec` 或 `--network nyist-stu`。自动模式不会根据 SSID 修改账号后缀。

```sh
./nyist-connect-linux-amd64 --status
./nyist-connect-linux-amd64 --status-json
./nyist-connect-linux-amd64 --network auto --connect
./nyist-connect-linux-amd64 --check-update
./nyist-connect-linux-amd64 --version
```

- 状态检查不需要密码，报告当前网络、在线账号和 IP；门户不可达或响应无效不是“确认离线”。连接及注销仍拒绝操作其他账号的会话。
- Linux 菜单与 macOS CLI 菜单增加“检查更新”，菜单配置支持自动网络模式。macOS 使用对应终端二进制和相同参数。
- 认证、门户状态与经典保活 HTTP 请求明确禁用环境代理（HTTP_PROXY / HTTPS_PROXY 等），不改变系统代理设置。新连接尽量选择非虚拟的物理网卡和源地址；Linux 进一步尝试设备绑定，无权限时退回源地址绑定。**这不是对所有 TUN 的保证**：策略路由、透明拦截、虚假 DNS 及操作系统权限仍可能影响请求。其他系统目前仅源地址绑定。遇到此类情况，需要在代理软件中将校园域名及真实认证服务器 IP 配置为直连；程序不会自动关闭 TUN、提权或修改全局路由。
- 更新检测访问 `GUYU2233/AuthingNYIST` 的 GitHub 最新稳定 Release，允许沿用环境代理以便访问 GitHub；具有超时、响应大小限制及 API 错误处理。仅提示版本及发布页面，**不自动下载、安装、执行附件**。离线或 API 限流会返回错误，不伪报“已是最新”。
- 源码构建默认版本为 `dev`，仍显示最新 Release，但不会假定开发版落后。发布脚本从 `VERSION`、`GITHUB_REF_NAME` 或 `CI_COMMIT_TAG` 注入版本；手动构建可以使用：

```sh
go build -ldflags '-X github.com/Palvef/AuthingNYIST/internal/version.Version=v1.2.3' -o nyist-connect ./cmd/nyist
```

## 网络要求与安全边界

- 有无线网卡时，Windows 需要已有与 SSID 同名的 WLAN 配置；Linux 需要 NetworkManager / `nmcli` 和同名连接配置。请先在系统中手动连接过对应校园 Wi-Fi。无无线网卡时保留有线网络，只进行认证。
- **SSID 不等于账号后缀**，程序不会自行拼接 `@nyist-tec` / `@nyist-stu`；按学校提供的完整账号填写。
- 默认 HTTPS 并验证证书，不自动降级 HTTP；密码不写日志，不作为外部命令行参数传递。
- 当前存在其他在线账号时，拒绝自动覆盖或注销，避免误断网。
- 普通界面保活每 30 秒检查一次；连续 5 次暂时性失败后停止，认证拒绝立即停止，长时间断网后需重新连接。开机认证服务为等待登录前网络就绪，暂时性失败使用指数退避（最长 5 分钟）持续重试；认证拒绝或检测到其他账号立即暂停。

## 上游问题同步

- [上游 #4：TUN 下保活误判在线](https://github.com/Palvef/AuthingNYIST/issues/4)：采纳应用层验证思路，不能仅凭 TCP 握手判断在线。当前统一客户端使用门户认证状态检查。
- [上游安全依赖升级 PR #3](https://github.com/Palvef/AuthingNYIST/pull/3)：升级 `golang.org/x/crypto` 至 v0.45.0，并同步兼容的 `x/sys`、`x/term`；最低 Go 版本为 1.24。

## 从源码构建

需要 Go 1.24 或更高版本（安全依赖升级的最低要求）；建议使用受支持的 Go 稳定版，CI 使用 Go 1.26。原模块路径保留 `github.com/Palvef/AuthingNYIST`，以兼容已有内部导入和上游历史，不代表本仓库归属。

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

## CI 与发布

- **GitHub Actions**：main 提交 / PR 自动运行 Windows / Linux 测试、`go vet`、格式和脚本语法检查，Linux 共享核心 race 测试，Windows 原生 GUI 冒烟测试；通过后统一交叉编译全部发行二进制。日常构建产物 `release-bundle` 保留 14 天。
- **校内 GitLab CI**：分支 / MR / 标签自动运行 Linux 测试、校验和全部平台交叉编译；保留 SAST 与 Secret Detection。Windows GUI 交叉编译不需要 Windows Runner，但真实 GUI 冒烟测试由 GitHub Windows Runner 执行。
- **自动发行**：两端均在推送 `v*` 标签后发布 8 个附件：Windows GUI、macOS 双架构 DMG、Linux amd64 / arm64 的 `.deb` 与 `.rpm`，以及覆盖全部文件的 `SHA256SUMS.txt`。AUR / Flatpak / Snap / Homebrew / MacPorts / Nix 配方、Arch 包、仓库元数据与 shell 补全仍可在仓库内用 `BUILD_EXTRAS=1` 生成，但不再作为发行附件；`scripts/check-release-assets.sh` 在 CI 中强制这份 8 项白名单。GitHub 使用内置 `GITHUB_TOKEN`；GitLab 使用内置 `CI_JOB_TOKEN` 将文件存入 Generic Package Registry，再创建永久下载链接的 Release，无需个人访问令牌。失败可重跑同一标签流水线；请勿移动或复用已公开版本标签。
- 详细触发命令、Runner / 权限要求、校验和及失败排查见 [CI/CD 与发行指南](docs/CI-CD.md)。
- 本地已验证 Windows GUI 与单元测试；Linux 运行时由 CI 的 Ubuntu 测试覆盖，**真实校园网络、桌面环境与 systemd 服务仍需在目标机器验证**。测试使用 mock / fake runner，不执行真实认证或服务变更。

## 来源、致谢与许可证

**该项目复刻自 [Palvef/AuthingNYIST](https://github.com/Palvef/AuthingNYIST)**。感谢原作者 **Palvef**、上游维护者及所有贡献者提供协议实现、问题修复、测试和文档。

同时保留上游对 [z4yx/GoAuthing](https://github.com/z4yx/GoAuthing) 的致谢，感谢其带来的启发。

本分支新增共享认证核心、Windows GUI / 托盘、Linux 交互菜单与服务管理，以及对应配置保护、测试和构建流程；保留上游历史和版权声明。

遵循 **GNU GPL v3**，详见 [LICENSE](LICENSE)。再分发与修改时请保留原有版权、许可和来源说明。
