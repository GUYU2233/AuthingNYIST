# CI/CD 与发行指南

## 触发与产物

| 平台 | 普通提交 / 合并请求 | `v*` 标签 |
| --- | --- | --- |
| GitHub | main / PR：Windows + Linux 测试、vet、格式、脚本语法、Linux race、Windows GUI 冒烟；全部架构构建 | 复用全部测试与构建，生成 Release |
| 校内 GitLab | 分支 / MR：Linux 测试、vet、格式 / 语法检查，SAST / Secret Detection，全部架构构建 | 测试和构建后上传 Generic Package Registry 并发布 Release |

构建产物：Windows x64 GUI、Linux amd64 / arm64 交互版，以及经典 CLI 的 macOS amd64 / arm64、Windows x64、Linux x64 / arm64 / arm / armv5 / armv6 / mipsle / mipsbe / ppc64le / riscv64 / loong64。Windows GUI 在 Linux 交叉编译时也嵌入图标和 manifest。

发布脚本使用明确白名单，不上传账号配置、测试程序或工作目录其他文件。发行文件包含 README、使用说明、GPL 许可证及 `SHA256SUMS.txt`。Linux 下载后执行 `chmod +x`。普通流水线产物仅用于短期下载；GitLab Release 的下载链接使用 Package Registry，不依赖会过期的构建 artifact。不要删除发行版对应的包或设置会清理它们的包保留规则。

## 发布新版本

先确保两端 main 的测试通过，选择一个**未使用过**的版本号。以下为示例，执行前替换版本：

```sh
git switch main
git status --short
git tag -a v1.2.3 -m "Release v1.2.3"
git push origin main
git push nyist main
git push origin v1.2.3
git push nyist v1.2.3
```

两端各自测试、构建和发布，不会自动互相推送标签。`v1.2.3-rc.1` 等带连字符的标签在 GitHub 标记为 prerelease；GitLab 原生 Release 无同等 prerelease 标记。不要提交真实密码。不要移动发行标签：相同标签的重跑仅用于恢复失败的发布，不应用来变更已有版本内容。

- GitHub：[Actions](https://github.com/GUYU2233/AuthingNYIST/actions)、[Releases](https://github.com/GUYU2233/AuthingNYIST/releases)。新版本先创建草稿、上传成功后公开；已存在版本重跑覆盖同名附件。
- GitLab：[Pipelines](https://git.nyist.edu.cn/cips/AuthingNYIST/-/pipelines)、[Releases](https://git.nyist.edu.cn/cips/AuthingNYIST/-/releases)。文件上传成功后再创建 / 更新 Release；下载需要项目相应的访问权限。

## 基础设施与权限

- Go 1.26，构建需要访问 Go 模块代理 / 模块来源。GitHub 使用托管 Ubuntu / Windows Runner，版本工作流只给发布 job `contents: write`；仓库 / 组织不能禁用该权限。
- GitLab 必须有可运行无标签 job 的 **Linux Docker / Kubernetes Runner**，能使用 `golang:1.26-bookworm` 并访问 Go 模块、Debian 镜像源及 GitLab API。仅 shell executor 不会提供 YAML 的容器环境；此情况需管理员调整 Runner 或显式安装相同依赖。
- GitLab 项目启用 CI/CD、Package Registry、Release API，并允许同项目 `CI_JOB_TOKEN` 上传 Generic Packages / 创建 Release。保护标签时，发布者也必须有创建标签的权限。无需把校园网账号或个人 Token 配置成 CI 变量。
- SAST / Secret Detection 使用 GitLab 自带模板，需相应 Runner、分析器镜像网络和服务器版本支持；扫描发现的告警应在安全报告中处理。
- 仓库配置不能替代 Runner 或服务器权限配置。一直 pending 时先看 Runner 是否在线 / 匹配标签、保护分支要求；401 / 403 时检查项目包注册表、job token 和发行权限。不要在日志打印 Token。

## 本地验证

```sh
go test ./...
go vet ./...
bash scripts/build-unix.sh
bash scripts/package-release.sh
(cd dist && sha256sum --check SHA256SUMS.txt)
```

Windows 也可执行 PowerShell 构建与 GUI 冒烟：

```powershell
./scripts/build.ps1
./scripts/smoke-windows.ps1
Get-FileHash ./dist/nyist-connect.exe -Algorithm SHA256
```

将哈希与发行版 `SHA256SUMS.txt` 对应行比较。校验和用于发现传输损坏，不是独立签名或来源认证。测试不会连接真实校园网或修改系统服务；Linux systemd、目标桌面与真实网络仍需在相应设备验证。
