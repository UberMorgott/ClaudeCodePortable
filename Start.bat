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
set "BINDADDR=127.0.0.1:25345"
rem Session marker: lives OUTSIDE _run (which gets wiped), so a live tunnel can be
rem detected before we touch _run. Holds the PID of THIS session's wireproxy.exe.
set "LOCK=%ROOT%\_session.lock"
set "PWSH=%ROOT%\pwsh\pwsh.exe"
set "WT=%ROOT%\wt\WindowsTerminal.exe"

rem === guard against a double-launch from THIS stick =====================
rem If a lock exists AND the recorded wireproxy PID is still alive, a session
rem from this stick is already running. Open no 2nd window; don't wipe its _run.
if exist "%LOCK%" (
  set "LOCKPID="
  for /f "usebackq delims=" %%P in ("%LOCK%") do if not defined LOCKPID set "LOCKPID=%%P"
  if defined LOCKPID (
    tasklist /fi "imagename eq wireproxy.exe" /fi "pid eq !LOCKPID!" 2>nul | find /i "wireproxy.exe" >nul
    if not errorlevel 1 (
      echo [ERROR] A ClaudeCodePortable session from this stick is already running ^(wireproxy PID !LOCKPID!^).
      echo Close that window ^(or run "Stop.bat"^) before starting a new one.
      pause & exit /b 1
    )
  )
  rem Stale lock (no matching live wireproxy) -> safe to clear and continue.
  del /f /q "%LOCK%" >nul 2>&1
)

rem ephemeral run dir (decoded key + proxy cfg live here only while running).
rem Wipe any stale copy FIRST: a prior crash / hard window-close leaves the
rem decoded WireGuard private key on the stick, so clear it before regenerating.
rem Safe now: the live-session guard above already ran, so we only wipe a dead
rem session's leftovers.
if exist "%RUN%" rd /s /q "%RUN%" >nul 2>&1
mkdir "%RUN%"
if errorlevel 1 ( echo [ERROR] cannot create "%RUN%" & pause & exit /b 1 )

rem === bundled pwsh 7 is required for decoding (ZLibStream) ===
if not exist "%PWSH%" (
  echo [ERROR] Missing portable PowerShell: %PWSH%
  pause & exit /b 1
)

rem === wireproxy is required for the tunnel; check up front so a missing exe
rem gives a clear message instead of the misleading "rejected the config" below ===
if not exist "%WIREPROXY%" (
  echo [ERROR] Missing wireproxy.exe: %WIREPROXY%
  echo Run "Install or Update.bat" to fetch it.
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
rem Delayed expansion is toggled OFF around the capture: a '!' in a .vpn filename
rem would otherwise be eaten by !var! expansion, corrupting the path.
set "VPNFILE="
set "VPNCOUNT=0"
setlocal DisableDelayedExpansion
for /f "delims=" %%F in ('dir /b /on "%VPNDIR%\*.vpn" 2^>nul') do (
  set /a VPNCOUNT+=1
  if not defined VPNFILE set "VPNFILE=%VPNDIR%\%%F"
)
rem Carry the captured values back out of the DisableDelayedExpansion scope.
endlocal & set "VPNFILE=%VPNFILE%" & set "VPNCOUNT=%VPNCOUNT%"
if %VPNCOUNT% gtr 1 (
  echo [WARN] Found %VPNCOUNT% .vpn files in "%VPNDIR%"; using "%VPNFILE%"
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

rem === 2. decode vpn:// -> WireGuard conf AND generate wireproxy config ===
rem Both files are written by pwsh as UTF-8 WITHOUT BOM. The proxy config MUST NOT
rem be built with cmd `echo`: cmd redirects in the console OEM codepage, so a
rem non-ASCII path char (e.g. ...\тест\) is stored as OEM bytes; wireproxy reads
rem UTF-8 and fails with "cannot find path ...????...". cmd echo also mangles
rem `&`/`^`/`%` in the path. Letting the decoder emit it (forward slashes, full
rem path) keeps every byte correct and removes the separate generation step.
"%PWSH%" -NoProfile -ExecutionPolicy Bypass -File "%DECODER%" -In "%VPNFILE%" -Out "%AWG%" -ProxyOut "%PROXYCFG%" -BindAddress "%BINDADDR%"
if errorlevel 1 ( echo [ERROR] Failed to decode the .vpn file & pause & exit /b 1 )

rem === 3. validate ===
"%WIREPROXY%" -n -c "%PROXYCFG%"
if errorlevel 1 ( echo [ERROR] wireproxy rejected the config & pause & exit /b 1 )

rem === 4. start AmneziaWG userspace proxy (hidden, detached = survives) ===
rem No visible window: launched via pwsh ProcessStartInfo with CreateNoWindow.
rem The proxy is not in a kill-on-close job, so it keeps running after pwsh exits.
rem The live-session guard at the top already ruled out a second session from this
rem stick, so we always start our own proxy here. Confirm the port is free first
rem (find returns errorlevel 0 when a LISTENING line matches -> foreign collision).
set "STARTTUN=%ROOT%\shell\start-tunnel.ps1"
set "PIDFILE=%RUN%\wireproxy.pid"
netstat -ano | find "127.0.0.1:25345" | find "LISTENING" >nul
if not errorlevel 1 ( echo [ERROR] port 25345 already in use by another process & pause & exit /b 1 )
rem Start the proxy and record its PID as the session marker. start-tunnel writes the
rem PID to a FILE (not stdout): the spawned wireproxy inherits this console's stdout
rem handle, so capturing the helper's stdout via `for /f` would block on EOF until
rem wireproxy dies -> the launcher would hang here and never open a window. Calling it
rem directly (stdout to the console, not a pipe) and reading the PID file avoids that.
"%PWSH%" -NoProfile -ExecutionPolicy Bypass -File "%STARTTUN%" -Exe "%WIREPROXY%" -Config "%PROXYCFG%" -PidFile "%PIDFILE%"
if errorlevel 1 ( echo [ERROR] Failed to start the AmneziaWG proxy & pause & exit /b 1 )
set "TUNPID="
if exist "%PIDFILE%" set /p TUNPID=<"%PIDFILE%"
if not defined TUNPID ( echo [ERROR] proxy started but PID was not recorded & pause & exit /b 1 )
> "%LOCK%" echo %TUNPID%

rem === 5. launch Windows Terminal -> pwsh -> dot-source profile (gives `claude`) ===
rem launch: load profile then start Claude Code; -NoExit keeps the shell alive after claude exits
rem No ';' in the WT command line: WT treats ';' as a command separator (splits
rem into extra tabs), so we can't put "; claude" here. Instead set CCP_AUTOCLAUDE
rem and let profile.ps1 auto-start claude at the end of dot-sourcing. The env var
rem is inherited by the child pwsh through start.
set "CCP_AUTOCLAUDE=1"
rem Hand the profile the PID + lock of THIS session so its exit-handler kills only
rem our wireproxy and clears only our marker (not other sessions on the same stick).
set "CCP_TUNPID=%TUNPID%"
set "CCP_LOCK=%LOCK%"
set "LAUNCH=-NoExit -NoLogo -ExecutionPolicy Bypass -Command ". '%ROOT%\shell\profile.ps1'""
if exist "%WT%" (
  start "" "%WT%" new-tab "%PWSH%" %LAUNCH%
) else (
  start "" "%PWSH%" %LAUNCH%
)

exit /b 0
