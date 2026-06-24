@echo off
rem ClaudeCodePortable bootstrap (pure cmd, no system PowerShell needed).
rem Fetches bundled pwsh7 + repo skeleton onto the stick, then runs the
rem ensure-engine (shell\update.ps1) under the bundled pwsh. Called by
rem Install.bat as:  bootstrap.cmd "<stick-root>" [update.ps1 args]
setlocal EnableExtensions EnableDelayedExpansion

set "ROOT=%~1"
if "%ROOT%"=="" set "ROOT=%~dp0"
if "%ROOT:~-1%"=="\" set "ROOT=%ROOT:~0,-1%"
set "TMP0=%TEMP%\ccpboot_%RANDOM%%RANDOM%"
mkdir "%TMP0%" 2>nul

rem === 1. ensure bundled PowerShell 7 on the stick (cmd-only) ===
if not exist "%ROOT%\pwsh\pwsh.exe" (
  echo [*] fetching PowerShell 7 ...
  set "PWSHURL="
  for /f "usebackq delims=" %%u in (`curl -sIL -o NUL -w "%%{url_effective}" https://github.com/PowerShell/PowerShell/releases/latest`) do set "PWSHURL=%%u"
  if not defined PWSHURL goto :fail
  set "PWSHTAG=!PWSHURL:*tag/=!"
  set "PWSHVER=!PWSHTAG:v=!"
  curl -fSL -o "%TMP0%\pwsh.zip" "https://github.com/PowerShell/PowerShell/releases/download/!PWSHTAG!/PowerShell-!PWSHVER!-win-x64.zip" || goto :fail
  mkdir "%ROOT%\pwsh" 2>nul
  tar -xf "%TMP0%\pwsh.zip" -C "%ROOT%\pwsh" || goto :fail
)

rem === 2. fetch repo skeleton (scripts + claude-cfg) ===
echo [*] fetching repo skeleton ...
curl -fSL -o "%TMP0%\repo.zip" "https://codeload.github.com/UberMorgott/ClaudeCodePortable/zip/refs/heads/main" || goto :fail
tar -xf "%TMP0%\repo.zip" -C "%TMP0%" || goto :fail
set "SRC="
for /d %%d in ("%TMP0%\ClaudeCodePortable-*") do set "SRC=%%d"
if not defined SRC goto :fail
xcopy /e /i /y "!SRC!\shell" "%ROOT%\shell\" >nul || goto :fail
xcopy /e /i /y "!SRC!\claude-cfg" "%ROOT%\claude-cfg\" >nul || goto :fail
if exist "!SRC!\Start.bat"     copy /y "!SRC!\Start.bat"     "%ROOT%\Start.bat"     >nul
if exist "!SRC!\Stop.bat"      copy /y "!SRC!\Stop.bat"      "%ROOT%\Stop.bat"      >nul
if exist "!SRC!\Install.bat"   copy /y "!SRC!\Install.bat"   "%ROOT%\Install.bat"   >nul
if exist "!SRC!\bootstrap.cmd" copy /y "!SRC!\bootstrap.cmd" "%ROOT%\bootstrap.cmd" >nul

rem === 3. run the ensure-engine (install/update) under bundled pwsh ===
echo [*] running installer/updater ...
"%ROOT%\pwsh\pwsh.exe" -NoProfile -ExecutionPolicy Bypass -File "%ROOT%\shell\update.ps1" %2 %3 %4
set "RC=!ERRORLEVEL!"

rem === 4. apply staged pwsh swap (later in-place pwsh updates) ===
if exist "%ROOT%\pwsh.new" (
  if exist "%ROOT%\pwsh.old" rd /s /q "%ROOT%\pwsh.old"
  move "%ROOT%\pwsh" "%ROOT%\pwsh.old" >nul
  move "%ROOT%\pwsh.new" "%ROOT%\pwsh" >nul
  rd /s /q "%ROOT%\pwsh.old" >nul 2>&1
)

rd /s /q "%TMP0%" >nul 2>&1
endlocal & exit /b %RC%

:fail
echo [ERROR] bootstrap failed ^(need reachable internet / GitHub^).
rd /s /q "%TMP0%" >nul 2>&1
endlocal & exit /b 1
