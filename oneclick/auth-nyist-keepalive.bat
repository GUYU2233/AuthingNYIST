@echo off
rem auth-nyist-keepalive.bat — 登录并进入保活循环(断网自动重认证)
rem 作为常驻进程运行;关闭窗口即停止
powershell -ExecutionPolicy Bypass -File "%~dp0oneclick-auth.ps1" -KeepOnline
