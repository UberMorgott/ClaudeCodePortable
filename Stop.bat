@echo off
setlocal EnableExtensions EnableDelayedExpansion
rem Stops THIS stick's ClaudeCodePortable session (VPN proxy + claude/node started
rem from this stick) and wipes the ephemeral run dir so the stick stays clean
rem (decoded private key + generated config are removed). Scoped to %ROOT%: it
rem NEVER touches a node.exe / wireproxy.exe the host user runs from elsewhere.
set "ROOT=%~dp0"
set "ROOT=%ROOT:~0,-1%"
set "PWSH=%ROOT%\pwsh\pwsh.exe"
set "LOCK=%ROOT%\_session.lock"

rem === stop this stick's processes (scoped by ExecutablePath under %ROOT%) ======
rem Terminate ONLY wireproxy/claude/node whose image lives under this stick, then
rem wait until wireproxy is gone so a follow-up "Install or Update.bat" doesn't
rem falsely see a live session (locked files). All scoping/waiting is done in pwsh
rem via CIM (Win32_Process.ExecutablePath) because cmd has no path-scoped taskkill.
if exist "%PWSH%" (
  "%PWSH%" -NoProfile -ExecutionPolicy Bypass -Command "$prefix=($env:ROOT.TrimEnd('\')+'\'); $f=\"Name='wireproxy.exe' OR Name='claude.exe' OR Name='node.exe'\"; $mine=Get-CimInstance Win32_Process -Filter $f -ErrorAction SilentlyContinue | Where-Object { $_.ExecutablePath -and $_.ExecutablePath.StartsWith($prefix,[System.StringComparison]::OrdinalIgnoreCase) }; foreach ($p in $mine) { try { Stop-Process -Id $p.ProcessId -Force -ErrorAction SilentlyContinue } catch {} }; $deadline=(Get-Date).AddSeconds(10); do { Start-Sleep -Milliseconds 200; $still=Get-CimInstance Win32_Process -Filter \"Name='wireproxy.exe'\" -ErrorAction SilentlyContinue | Where-Object { $_.ExecutablePath -and $_.ExecutablePath.StartsWith($prefix,[System.StringComparison]::OrdinalIgnoreCase) } } while ($still -and (Get-Date) -lt $deadline); if ($still) { Write-Host '[WARN] wireproxy still running after 10s; close its window manually.' } else { Write-Host 'AmneziaWG proxy stopped.' }"
) else (
  rem Fallback if bundled pwsh is missing: best-effort blanket kill of wireproxy
  rem (its exe ships only on the stick, so this won't hit a host process).
  taskkill /im wireproxy.exe /f >nul 2>&1
)

rem === wipe ephemeral state + session marker ===================================
if exist "%LOCK%" del /f /q "%LOCK%" >nul 2>&1
if exist "%ROOT%\_run" rd /s /q "%ROOT%\_run" >nul 2>&1
rem Match the pwsh profile's graceful-exit cleanup, which also wipes _cache.
rem home\ is intentionally NOT wiped (persistent auth lives there).
if exist "%ROOT%\_cache" rd /s /q "%ROOT%\_cache" >nul 2>&1
echo Temp files wiped.
timeout /t 1 /nobreak >nul 2>&1
exit /b 0
