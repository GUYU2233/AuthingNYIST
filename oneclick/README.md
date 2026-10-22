# 一键校园网认证入口

脚本包装同仓库的 `auth-nyist` CLI。Windows 脚本读取同目录 `config.json`，使用独立的临时配置文件并在退出时清理，不覆盖用户主目录配置。

## 目录结构

- `config.json.example` 配置模板
- `config.json` 用户自行创建的账号文件，含密码，已加入 Git 忽略规则
- `oneclick-auth.ps1` Windows 双击/PowerShell 入口
- `oneclick-auth.sh` Linux/macOS shell 入口
- `auth-nyist.bat` Windows 双击快捷方式（可选）

## 快速开始（Windows）

1. 在仓库根目录构建可执行文件：

   ```powershell
   cd C:\path\to\AuthingNYIST
   go build -o auth-nyist.exe .\cli
   ```

2. 复制模板并填写自己的账号密码。真实配置已加入 Git 忽略规则，请勿分享：

   ```powershell
   Copy-Item oneclick\config.json.example oneclick\config.json
   notepad oneclick\config.json
   ```

3. 运行：

   首选 PowerShell 一键命令（双击 `.bat` 亦可，下面 3 个 `.bat` 已生成）：

   ```powershell
   powershell -ExecutionPolicy Bypass -File oneclick\oneclick-auth.ps1 -Once          # 单次登录
   powershell -ExecutionPolicy Bypass -File oneclick\oneclick-auth.ps1 -KeepOnline   # 登录并保活
   powershell -ExecutionPolicy Bypass -File oneclick\oneclick-auth.ps1 -Deauth       # 登出
   ```

   Windows 双击：

   - 普通登录：双击 `oneclick\auth-nyist.bat`
   - 登录并保活（断网自动重新认证）：双击 `oneclick\auth-nyist-keepalive.bat`
   - 登出：双击 `oneclick\auth-nyist-deauth.bat`

## 快速开始（Linux / macOS）

```bash
cd /path/to/AuthingNYIST
go build -o auth-nyist ./cli
cp oneclick/config.json.example oneclick/config.json
$EDITOR oneclick/config.json   # 填 username/password
chmod +x oneclick/oneclick-auth.sh

./oneclick/oneclick-auth.sh                  # 单次登录
./oneclick/oneclick-auth.sh --keep-online     # 登录并保活（后台需 nohup/systemd 守护）
./oneclick/oneclick-auth.sh --deauth          # 登出
```

## 注意事项

- Linux/macOS 脚本在 `oneclick` 目录创建权限为 `600` 的独立临时配置，通过绝对路径传给 CLI，退出时仅清理该临时文件；不会读取、覆盖或恢复 `~/.auth-nyist`。脚本目录需可写。
- `--daemonize` 模式不会从标准输入读账号密码；密码从 `config.json` 进入临时配置。
- `keepOnline` 会进入死循环保活，建议用于常驻服务而非临时双击；Windows 下常驻可用 `auth-nyist-keepalive.bat` 作为计划任务/服务启动。
- 首次请手动打开 PowerShell 运行 `oneclick-auth.ps1`，确认成功后再加常驻。
