@echo off
rem auth-nyist-deauth.bat — 双击运行登出
powershell -ExecutionPolicy Bypass -File "%~dp0oneclick-auth.ps1" -Deauth
