@echo off
rem ClaudeCodePortable bootstrap (pure cmd, no system PowerShell needed).
rem Fetches bundled pwsh7 + repo skeleton onto the stick, then runs the
rem ensure-engine (shell\update.ps1) under the bundled pwsh. Called by
rem "Install or Update.bat" as:  bootstrap.cmd "<stick-root>" [update.ps1 args]
setlocal EnableExtensions EnableDelayedExpansion

set "ROOT=%~1"
if "%ROOT%"=="" set "ROOT=%~dp0"
if "%ROOT:~-1%"=="\" set "ROOT=%ROOT:~0,-1%"

rem Built-ins we rely on (Windows 10 1803+). Fail with a SPECIFIC message rather
rem than a misleading "no internet" later if they're missing.
where curl >nul 2>&1 || ( echo [ERROR] curl.exe not found ^(need Windows 10 1803+^). & goto :fail )
where tar  >nul 2>&1 || ( echo [ERROR] tar.exe not found ^(need Windows 10 1803+^). & goto :fail )

set "TMP0=%TEMP%\ccpboot_%RANDOM%%RANDOM%"
mkdir "%TMP0%" 2>nul || ( echo [ERROR] cannot create temp dir "%TMP0%" ^(is %%TEMP%% writable?^). & goto :fail )

rem === 1. ensure a WORKING bundled PowerShell 7 on the stick (cmd-only) ===
rem Don't trust mere existence of pwsh.exe: a partially-deleted pwsh\ (exe kept but
rem a runtime DLL gone) would poison every step below. Smoke-test it; re-fetch on fail.
set "PWSHOK="
if exist "%ROOT%\pwsh\pwsh.exe" (
  "%ROOT%\pwsh\pwsh.exe" -NoProfile -NoLogo -Command "exit 0" >nul 2>&1 && set "PWSHOK=1"
)
if not defined PWSHOK (
  echo [*] fetching PowerShell 7 ...
  if exist "%ROOT%\pwsh" rd /s /q "%ROOT%\pwsh"
  set "PWSHURL="
  for /f "usebackq delims=" %%u in (`curl -fsIL --connect-timeout 30 --max-time 120 -o NUL -w "%%{url_effective}" https://github.com/PowerShell/PowerShell/releases/latest`) do set "PWSHURL=%%u"
  if not defined PWSHURL goto :fail
  set "PWSHTAG=!PWSHURL:*tag/=!"
  set "PWSHVER=!PWSHTAG:v=!"
  curl -fSL --connect-timeout 30 --max-time 600 --retry 2 -o "%TMP0%\pwsh.zip" "https://github.com/PowerShell/PowerShell/releases/download/!PWSHTAG!/PowerShell-!PWSHVER!-win-x64.zip" || goto :fail
  mkdir "%ROOT%\pwsh" 2>nul
  tar -xf "%TMP0%\pwsh.zip" -C "%ROOT%\pwsh" || goto :fail
  "%ROOT%\pwsh\pwsh.exe" -NoProfile -NoLogo -Command "exit 0" >nul 2>&1 || ( echo [ERROR] fetched pwsh is not runnable. & goto :fail )
)

rem === 2. fetch repo skeleton (scripts + claude-cfg) ===
echo [*] fetching repo skeleton ...
curl -fSL --connect-timeout 30 --max-time 600 --retry 2 -o "%TMP0%\repo.zip" "https://codeload.github.com/UberMorgott/ClaudeCodePortable/zip/refs/heads/main" || goto :fail
tar -xf "%TMP0%\repo.zip" -C "%TMP0%" || goto :fail
set "SRC="
for /d %%d in ("%TMP0%\ClaudeCodePortable-*") do set "SRC=%%d"
if not defined SRC goto :fail
rem Smart sync: copy only files that differ (run from the downloaded copy so
rem the newest sync logic is used). Non-destructive: keeps user-local extras.
rem Item list passed as bare trailing tokens (NOT `-Items a b c`): pwsh -File
rem only binds the first value after a named switch, so the rest would error.
"%ROOT%\pwsh\pwsh.exe" -NoProfile -ExecutionPolicy Bypass -File "!SRC!\shell\sync-files.ps1" -Src "!SRC!" -Dest "%ROOT%" shell claude-cfg Start.bat Stop.bat
if errorlevel 1 goto :fail
rem Verify the sync actually produced the engine before we try to run it — a
rem partial/empty skeleton would otherwise fail with a confusing pwsh error.
if not exist "%ROOT%\shell\update.ps1" ( echo [ERROR] sync incomplete: shell\update.ps1 missing. & goto :fail )

rem bootstrap.cmd is no longer synced to the stick root (it's fetched fresh from
rem GitHub by the installer). Remove any stale root copy left by older versions.
rem Safe: the running bootstrap is the %TEMP% copy curl'd by the installer, not
rem %ROOT%\bootstrap.cmd, so deleting it can't pull the rug from under us.
if exist "%ROOT%\bootstrap.cmd" del /q "%ROOT%\bootstrap.cmd"

rem === 3. run the ensure-engine (install/update) under bundled pwsh ===
echo [*] running installer/updater ...
"%ROOT%\pwsh\pwsh.exe" -NoProfile -ExecutionPolicy Bypass -File "%ROOT%\shell\update.ps1" %2 %3 %4
set "RC=!ERRORLEVEL!"

rem === 4. apply staged pwsh swap (later in-place pwsh updates) ===
rem Only apply a COMPLETE pwsh.new (has pwsh.exe). A partial/stale one from an
rem aborted run is discarded, not moved over the working pwsh. If the swap fails
rem mid-move, restore pwsh.old so the stick is never left without a pwsh.
if exist "%ROOT%\pwsh.new\pwsh.exe" (
  if exist "%ROOT%\pwsh.old" rd /s /q "%ROOT%\pwsh.old"
  move "%ROOT%\pwsh" "%ROOT%\pwsh.old" >nul 2>&1
  move "%ROOT%\pwsh.new" "%ROOT%\pwsh" >nul 2>&1
  if exist "%ROOT%\pwsh\pwsh.exe" (
    rd /s /q "%ROOT%\pwsh.old" >nul 2>&1
  ) else (
    if exist "%ROOT%\pwsh.old\pwsh.exe" move "%ROOT%\pwsh.old" "%ROOT%\pwsh" >nul 2>&1
  )
) else (
  if exist "%ROOT%\pwsh.new" rd /s /q "%ROOT%\pwsh.new" >nul 2>&1
)

rd /s /q "%TMP0%" >nul 2>&1
endlocal & exit /b %RC%

:fail
echo [ERROR] bootstrap failed ^(need reachable internet / GitHub^).
rd /s /q "%TMP0%" >nul 2>&1
endlocal & exit /b 1
