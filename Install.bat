@echo off
rem ClaudeCodePortable — single-click install AND update.
rem The ONLY file that needs to be on a blank stick (plus your AMNEZIA\*.vpn and
rem Claude creds). Fetches the latest bootstrap from GitHub and runs it; bootstrap
rem pulls pwsh7 + repo skeleton onto the stick, then installs/updates every
rem component. Pure cmd + curl + tar (Win10 1803+ built-ins) — no system
rem PowerShell needed, so host PS execution policy can't block it.
setlocal EnableExtensions
title ClaudeCodePortable installer/updater

set "ROOT=%~dp0"
set "ROOT=%ROOT:~0,-1%"

rem Don't run while a portable session is live (files would be locked).
tasklist /fi "imagename eq wireproxy.exe" | find /i "wireproxy.exe" >nul
if not errorlevel 1 (
  echo [!] A portable session looks active ^(wireproxy.exe running^).
  echo     Close it / run Stop.bat first, then re-run Install.bat.
  pause & exit /b 1
)

set "BOOT=%TEMP%\ccp_bootstrap_%RANDOM%%RANDOM%.cmd"
echo [*] fetching bootstrap ...
curl -fSL -o "%BOOT%" "https://raw.githubusercontent.com/UberMorgott/ClaudeCodePortable/main/bootstrap.cmd"
if errorlevel 1 (
  echo [ERROR] cannot fetch bootstrap.cmd ^(need internet / GitHub reachable^).
  del "%BOOT%" >nul 2>&1
  pause & exit /b 1
)

call "%BOOT%" "%ROOT%" %*
set "RC=%ERRORLEVEL%"
del "%BOOT%" >nul 2>&1

echo.
if "%RC%"=="0" ( echo [OK] done. ) else ( echo [!] finished with code %RC%. )
pause
exit /b %RC%
