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

rem ephemeral run dir (decoded key + proxy cfg live here only while running).
rem Wipe any stale copy FIRST: a prior crash / hard window-close leaves the
rem decoded WireGuard private key on the stick, so clear it before regenerating.
if exist "%RUN%" rd /s /q "%RUN%" >nul 2>&1
mkdir "%RUN%"
if errorlevel 1 ( echo [ERROR] cannot create "%RUN%" & pause & exit /b 1 )
set "PWSH=%ROOT%\pwsh\pwsh.exe"
set "WT=%ROOT%\wt\WindowsTerminal.exe"

rem === bundled pwsh 7 is required for decoding (ZLibStream) ===
if not exist "%PWSH%" (
  echo [ERROR] Missing portable PowerShell: %PWSH%
  pause & exit /b 1
)

rem === ensure config dir exists (auto-create on first run) ===
if not exist "%VPNDIR%" (
  mkdir "%VPNDIR%"
  if errorlevel 1 ( echo [ERROR] cannot create "%VPNDIR%" & pause & exit /b 1 )
)

rem === 1. find the Amnezia share file (*.vpn) ===
rem Deterministic pick: sort by name (dir /on) and take the first; this avoids
rem the non-deterministic file order of a plain for-loop when several .vpn exist.
set "VPNFILE="
set "VPNCOUNT=0"
for /f "delims=" %%F in ('dir /b /on "%VPNDIR%\*.vpn" 2^>nul') do (
  set /a VPNCOUNT+=1
  if not defined VPNFILE set "VPNFILE=%VPNDIR%\%%F"
)
if !VPNCOUNT! gtr 1 (
  echo [WARN] Found !VPNCOUNT! .vpn files in "%VPNDIR%"; using "!VPNFILE!"
  echo        ^(first in sorted order^). Remove the extras to silence this warning.
)
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

rem === 5. start AmneziaWG userspace proxy (hidden, detached = survives) ===
rem No visible window: launched via pwsh ProcessStartInfo with CreateNoWindow.
rem The proxy is not in a kill-on-close job, so it keeps running after pwsh exits.
set "STARTTUN=%ROOT%\shell\start-tunnel.ps1"
tasklist /fi "imagename eq wireproxy.exe" | find /i "wireproxy.exe" >nul
if errorlevel 1 (
  rem Our wireproxy is NOT running, so port 25345 must be free for us to bind it.
  rem find returns errorlevel 0 when a LISTENING line matches -> foreign collision.
  netstat -ano | find ":25345" | find "LISTENING" >nul
  if not errorlevel 1 ( echo [ERROR] port 25345 already in use by another process & pause & exit /b 1 )
  "%PWSH%" -NoProfile -ExecutionPolicy Bypass -File "%STARTTUN%" -Exe "%WIREPROXY%" -Config "%PROXYCFG%"
  if errorlevel 1 ( echo [ERROR] Failed to start the AmneziaWG proxy & pause & exit /b 1 )
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
