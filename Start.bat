@echo off
setlocal EnableExtensions EnableDelayedExpansion
title ClaudeCodePortable launcher

rem === portable root (folder of this .bat, no trailing slash) ===
set "ROOT=%~dp0"
set "ROOT=%ROOT:~0,-1%"

set "VPNDIR=%ROOT%\Amnezia config"
set "DECODER=%ROOT%\shell\decode-vpn.ps1"
set "RUN=%ROOT%\_run"
set "AWG=%RUN%\awg.generated.conf"
set "PROXYCFG=%RUN%\proxy.generated.conf"
set "WIREPROXY=%ROOT%\wireproxy\wireproxy.exe"

rem ephemeral run dir (decoded key + proxy cfg live here only while running)
if not exist "%RUN%" mkdir "%RUN%"
set "PWSH=%ROOT%\pwsh\pwsh.exe"
set "WT=%ROOT%\wt\WindowsTerminal.exe"

rem === bundled pwsh 7 is required for decoding (ZLibStream) ===
if not exist "%PWSH%" (
  echo [ERROR] Missing portable PowerShell: %PWSH%
  pause & exit /b 1
)

rem === ensure config dir exists (auto-create on first run) ===
if not exist "%VPNDIR%" mkdir "%VPNDIR%"

rem === 1. find the Amnezia share file (*.vpn) ===
set "VPNFILE="
for %%F in ("%VPNDIR%\*.vpn") do set "VPNFILE=%%~fF"
if not defined VPNFILE (
  echo.
  echo [ERROR] No Amnezia config found in "%VPNDIR%"
  echo In the Amnezia app: your connection -^> Share -^> save the vpn://... file
  echo into "Amnezia config\" ^(any name ending .vpn^). Swap that file to change server.
  echo.
  pause & exit /b 1
)
echo Using config: "%VPNFILE%"

rem === 2. decode vpn:// -> WireGuard-format conf ===
"%PWSH%" -NoProfile -ExecutionPolicy Bypass -File "%DECODER%" -In "%VPNFILE%" -Out "%AWG%"
if errorlevel 1 ( echo [ERROR] Failed to decode the .vpn file & pause & exit /b 1 )

rem === 3. generate wireproxy config (absolute path, forward slashes) ===
set "AWGF=%AWG:\=/%"
> "%PROXYCFG%" echo WGConfig = %AWGF%
>>"%PROXYCFG%" echo.
>>"%PROXYCFG%" echo [http]
>>"%PROXYCFG%" echo BindAddress = 127.0.0.1:25345

rem === 4. validate ===
"%WIREPROXY%" -n -c "%PROXYCFG%"
if errorlevel 1 ( echo [ERROR] wireproxy rejected the config & pause & exit /b 1 )

rem === 5. start AmneziaWG userspace proxy (own minimized window = survives) ===
tasklist /fi "imagename eq wireproxy.exe" | find /i "wireproxy.exe" >nul
if errorlevel 1 (
  start "wireproxy-amnezia" /MIN "%WIREPROXY%" -s -c "%PROXYCFG%"
)

rem === 6. launch Windows Terminal -> pwsh -> dot-source profile (gives `claude`) ===
rem launch: load profile then start Claude Code; -NoExit keeps the shell alive after claude exits
rem No ';' in the WT command line: WT treats ';' as a command separator (splits
rem into extra tabs), so we can't put "; claude" here. Instead set CCP_AUTOCLAUDE
rem and let profile.ps1 auto-start claude at the end of dot-sourcing. The env var
rem is inherited by the child pwsh through start.
set "CCP_AUTOCLAUDE=1"
set "SHELL=%PWSH%"
set "LAUNCH=-NoExit -NoLogo -ExecutionPolicy Bypass -Command ". '%ROOT%\shell\profile.ps1'""
if exist "%WT%" (
  start "" "%WT%" new-tab "%SHELL%" %LAUNCH%
) else (
  start "" "%SHELL%" %LAUNCH%
)

exit /b 0
