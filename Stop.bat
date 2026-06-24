@echo off
rem Kills the AmneziaWG proxy (VPN off) and wipes the ephemeral run dir so the
rem stick stays clean (decoded private key + generated config are removed).
set "ROOT=%~dp0"
set "ROOT=%ROOT:~0,-1%"
taskkill /im wireproxy.exe /f >nul 2>&1
if exist "%ROOT%\_run" rd /s /q "%ROOT%\_run" >nul 2>&1
echo AmneziaWG proxy stopped, temp files wiped.
timeout /t 1 >nul
