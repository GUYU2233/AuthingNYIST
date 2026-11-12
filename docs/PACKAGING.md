# 包管理器与发行渠道

发行仓库：<https://github.com/GUYU2233/AuthingNYIST/releases>。以下是**本项目提供的包或配方**，不表示已经进入发行版官方软件源、AUR、Flathub、Snap Store、Homebrew core、MacPorts 或 nixpkgs。

## 支持范围

| 管理器 | 产物 / 使用方式 | 架构 |
| --- | --- | --- |
| APT、dpkg | `.deb`；可部署的 APT 仓库归档 | Linux amd64、arm64 |
| DNF、yum、RPM、Zypper | `.rpm`；可部署的 RPM repodata 仓库归档 | Linux x86_64、aarch64 |
| Pacman | `.pkg.tar.zst` | Linux x86_64、aarch64 |
| AUR | 固定版本与校验和的 PKGBUILD / .SRCINFO 配方归档；需维护者单独提交 AUR | 同上；AUR 官方主要面向 x86_64 |
| Flatpak | 固定版本 manifest；本地构建 bundle（见生成的说明） | Linux x86_64、aarch64 |
| Snap | 固定版本 snapcraft 配方；本地构建 snap（见生成的说明） | Linux amd64、arm64 |
| Homebrew（Formula） | 固定版本 Formula，安装 CLI `nyist-connect` | Linux / macOS Intel、ARM64 |
| Homebrew（Cask） | 固定版本 Cask，安装 macOS GUI `NYIST Connect.app` | macOS Intel、ARM64；需同时提供两个 DMG |
| MacPorts | 本地 Portfile | macOS x86_64、arm64 |
| Nix | 固定哈希 binary derivation | Linux / macOS x86_64、aarch64 |
| Shell 补全 | bash / zsh / fish / PowerShell 补全脚本 | 与 CLI 相同 |

这些包安装 `nyist-connect`，**不自动登录、不写账号、不自动启用服务**。macOS GUI / DMG 与 Windows GUI 仍由原有流程发布；本页的包管理器配方面向 CLI。经典 CLI 已移除。

## 发行附件布局

Release 页面只放安装程序与校验和，共 8 个附件：

| 附件 | 内容 |
| --- | --- |
| `nyist-connect.exe` | Windows GUI 与自动更新载荷 |
| `NYIST-Connect-macos-amd64.dmg`、`NYIST-Connect-macos-arm64.dmg` | macOS 应用 |
| `nyist-connect_<版本>_amd64.deb`、`..._arm64.deb` | Debian / Ubuntu |
| `nyist-connect-<版本>-1.x86_64.rpm`、`...aarch64.rpm` | Fedora / RHEL / openSUSE |
| `SHA256SUMS.txt` | 覆盖以上全部文件 |

Arch 包、AUR 的 PKGBUILD、Flatpak / Snap 清单、仓库元数据、Homebrew / MacPorts / Nix 配方和 shell 补全仍由 `scripts/package-distributions.sh` 在 `BUILD_EXTRAS=1` 时生成，供开发者本地使用，**不再随 Release 分发**。它们固定的下载地址指向不再上传的 CLI 归档，因此不要把旧配方直接当成可用发行物。`scripts/check-release-assets.sh` 在 CI 中强制上述 8 项白名单，旧布局产物或临时文件不会混入发行。

## 下载与本地安装

从同一 Release 下载对应 CPU 的文件及 `SHA256SUMS.txt`，先验证校验和；哈希只防传输损坏，不等同于独立来源签名。以下文件名中的版本和架构请替换为实际下载的名称：

```sh
# Debian / Ubuntu，apt 自动解决依赖；dpkg 不负责依赖解析
sudo apt install ./nyist-connect_VERSION_amd64.deb
# 或 sudo dpkg -i ./nyist-connect_VERSION_amd64.deb

# Fedora / RHEL / openSUSE（三选一）
sudo dnf install ./nyist-connect-VERSION-1.x86_64.rpm
sudo yum install ./nyist-connect-VERSION-1.x86_64.rpm
sudo zypper install ./nyist-connect-VERSION-1.x86_64.rpm
# 底层接口：sudo rpm -Uvh ./nyist-connect-VERSION-1.x86_64.rpm
```

除 deb 与 rpm 外的其他格式（Arch 包、AUR、Flatpak / Snap、Homebrew / MacPorts / Nix 配方、软件源元数据）不再随 Release 提供，需要时在仓库内用 `BUILD_EXTRAS=1` 自行生成，详见下文“维护者构建”。

现代 CLI 的 Wi-Fi 检测/切换需要宿主提供 NetworkManager 的 CLI（原生包不强制替换或安装网络管理器）；不同桌面或非 NetworkManager 系统请先确认兼容性。不要为安装而停用已有网络管理服务。配置按原程序保存于用户目录，不随卸载自动删除，避免丢失账号配置。

## Homebrew、MacPorts、Nix

这些配方不再随 Release 发布。在仓库内用 `BUILD_EXTRAS=1 VERSION=<tag> bash scripts/package-distributions.sh` 生成后，路径都相对 `dist/package-recipes/`：

- **Homebrew Formula（CLI）**：把 `homebrew/authingnyist.rb` 放入你自己创建的本地 / Git tap 的 `Formula/`，执行 `brew install <你的tap>/authingnyist`。没有预设公开 tap，也不要假定 `brew install authingnyist` 已能从 core 安装。
- **Homebrew Cask（macOS GUI）**：把 `homebrew/Casks/nyist-connect.rb` 放入 tap 的 `Casks/`，执行 `brew install --cask <你的tap>/nyist-connect`。Cask 只为同时生成了 amd64 与 arm64 两个 DMG 的正式版本生成，安装的是 `.app`，并把其中的 `nyist-connect` 暴露到 PATH；DMG 为 ad-hoc 签名、未公证，Gatekeeper 仍可能拦截，需要在“系统设置 → 隐私与安全性”中由用户自行确认。
- **MacPorts**：将 `package-recipes/macports/Portfile` 放入本地 ports tree 的 `net/authingnyist/`，在 tree 根目录运行 `portindex`，按 MacPorts 本地源方式加入 sources 配置，再执行 `sudo port install authingnyist`。没有向官方 ports tree 提交。
- **Nix**：在 `package-recipes/nix/` 运行 `nix-build default.nix`；用 `./result/bin/nyist-connect --version` 验证。调用方需提供可用的 nixpkgs（`NIX_PATH` 或 `--arg pkgs`）；这不是承诺已合并 nixpkgs，也不是锁定整个 nixpkgs 依赖树的 flake。

这些配方使用对应 GitHub Release 的**精确 tag 和二进制 SHA256**，不能用开发构建生成配方再冒充正式 tag。`0.0.0-dev` 仅供 CI 验证，不能发布给用户。

## APT / RPM 软件源

仓库归档是可部署的静态目录，**没有自动上线的域名或 Pages 地址**。维护者解压到 HTTPS 服务器后，才有软件源 URL。构建脚本支持用 `REPOSITORY_SIGNING_KEY` 指定 GPG 私钥指纹：签名的 APT Release 和 RPM metadata 以及独立公钥应通过可信渠道分发。

- APT 客户端应使用专属 keyring 和 `signed-by=`，不要使用全局 `apt-key`，不要用 `trusted=yes` 绕过签名。
- DNF/yum/Zypper 根据实际包签名和 repodata 签名设置 `gpgcheck` / `repo_gpgcheck`；metadata 签名不等于 RPM 包签名。未签名包应先做独立校验，生产源需要维护者额外签包。
- 未提供私钥时生成的是**未签名的部署素材**，不应宣称默认满足客户端信任策略。私钥不提交仓库，不打印到 CI 日志。

## Flatpak / Snap 与权限边界

源码归档内的说明给出了构建、安装命令和权限要求。沙盒不能保证看见宿主 Wi-Fi、调用宿主 NetworkManager / systemd 或绑定物理网卡；**不能承诺与原生包等效的 TUN 绕过和服务管理能力**。认证需要网络权限，配置可能保存校园网凭证，安装前请审阅权限。优先使用原生包获得完整宿主网络集成。

生成 manifest 不等于生成安装包；Release 默认提供固定版本的构建配方，只有实际运行对应构建工具后才有 `.flatpak` / `.snap`。Flathub / Snap Store 上架需独立维护、审核、账号与发布凭证，不会在本流程自动申请或上传。

## Shell 补全

仓库内的 `completions/` 提供 bash、zsh、fish 和 PowerShell 补全（同样不随 Release 分发），由 `scripts/generate-completions.py` 从 CLI 源码的标志定义生成，CI 用 `--check` 阻止补全与源码漂移。

```sh
# bash（当前会话）
source completions/nyist-connect.bash
# zsh：把 completions/_nyist-connect 放入 $fpath 后重开 shell
# fish
cp completions/nyist-connect.fish ~/.config/fish/completions/
# PowerShell：在 $PROFILE 中点源 completions/nyist-connect.ps1
```

补全只覆盖参数名，不执行认证，也不读取配置文件。

## 维护者构建

依赖：Python 3.9+、dpkg-deb、rpmbuild、zstd、bsdtar、apt-ftparchive、createrepo_c、tar。Debian/Ubuntu 可安装 `python3 rpm zstd libarchive-tools apt-utils createrepo-c`。

```sh
VERSION=v1.2.3 bash scripts/build-unix.sh
# 默认只生成随发行提供的 deb 与 rpm
VERSION=v1.2.3 bash scripts/package-distributions.sh
# 需要 Arch/AUR/Flatpak/Snap/配方/仓库时追加
BUILD_EXTRAS=1 VERSION=v1.2.3 bash scripts/package-distributions.sh
bash scripts/package-release.sh
bash scripts/check-release-assets.sh dist
(cd dist && sha256sum --check SHA256SUMS.txt)
python3 -m unittest discover -s packaging/recipes -v
python3 -m unittest discover -s packaging/completions -v
python3 -m unittest discover -s packaging/homebrew -v
```

完整发行构建还需要原有 Windows / macOS 产物。GitHub 和 GitLab 的标签流水线默认只生成随发行提供的 deb / rpm 并据此校验和；需要额外格式时在本地加 `BUILD_EXTRAS=1`。尚未运行的商店 / macOS 本地包管理器安装测试应在目标系统继续验证。
