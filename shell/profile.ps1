# Dot-sourced into the interactive pwsh session by Start.bat.
# Defines a `claude` wrapper that tunnels ONLY claude (+ its child tools)
# through the local AmneziaWG http-proxy. Everything else you type in this
# terminal goes out directly (no proxy).

$Root = Split-Path -Parent $PSScriptRoot              # shell\ -> portable root
$env:CLAUDE_CONFIG_DIR  = Join-Path $Root 'claude-cfg'

# --- Hybrid host-isolation: pin HOME/APPDATA to the stick ---
# CLAUDE_CONFIG_DIR above already pins claude's own config/auth/memory. This pins
# the generic per-user dirs too, so claude AND any child tool it spawns (node, git,
# npm) read & write their user data on the stick, never the host profile. The
# working dir stays the host owner's files (set below) so you can still fix them,
# but nothing is left behind in their profile.
$Global:CC_HostHome = $env:USERPROFILE          # remember real host home for the workdir default
$stickHome = Join-Path $Root 'home'
foreach ($d in @($stickHome, (Join-Path $stickHome 'AppData\Roaming'), (Join-Path $stickHome 'AppData\Local'))) {
    New-Item -ItemType Directory -Force -Path $d | Out-Null
}
$env:USERPROFILE  = $stickHome
$env:HOME         = $stickHome
$env:APPDATA      = Join-Path $stickHome 'AppData\Roaming'
$env:LOCALAPPDATA = Join-Path $stickHome 'AppData\Local'

$env:DISABLE_AUTOUPDATER = '1'                          # keep portable binary stable
$env:USE_BUILTIN_RIPGREP = '1'

# Bundled toolchains FIRST on PATH so claude uses the portable node/go/pwsh,
# never the host's. Order: node, go\bin, pwsh, then the host PATH.
$env:Path = (Join-Path $Root 'node') + ';' +
            (Join-Path $Root 'go\bin') + ';' +
            (Join-Path $Root 'pwsh') + ';' +
            (Join-Path $Root 'statusline') + ';' + $env:Path

# Portable Go: toolchain on the stick, mutable caches in an on-stick scratch dir
# (wiped on exit) so nothing lands in the host profile. local toolchain = no
# network auto-download.
$env:GOROOT      = Join-Path $Root 'go'
$env:GOTOOLCHAIN = 'local'
$cacheRoot       = Join-Path $Root '_cache'      # on-stick scratch, wiped on exit (host stays clean)
$env:GOPATH      = Join-Path $cacheRoot 'go\path'
$env:GOCACHE     = Join-Path $cacheRoot 'go\cache'
$env:GOMODCACHE  = Join-Path $cacheRoot 'go\mod'

# Portable npm: redirect the mutable npm cache to the on-stick scratch dir so the
# host profile stays clean; wiped on exit alongside the Go caches.
$env:npm_config_cache = Join-Path $cacheRoot 'npm'

# --- leave no trace on the host: no command history, no telemetry ---
try { Set-PSReadLineOption -HistorySaveStyle SaveNothing -ErrorAction SilentlyContinue } catch {}
$env:POWERSHELL_TELEMETRY_OPTOUT = '1'
$env:DOTNET_CLI_TELEMETRY_OPTOUT = '1'
$env:POWERSHELL_UPDATECHECK = 'Off'

$Global:CC_Root      = $Root
$Global:CC_ClaudeExe = Join-Path $Root 'bin\claude.exe'
$Global:CC_ProxyUrl  = 'http://127.0.0.1:25345'

# On window close: stop the VPN proxy and wipe the ephemeral _run dir (with the
# decoded key) so nothing temporary is left on the stick.
$null = Register-EngineEvent PowerShell.Exiting -Action {
    Get-Process wireproxy -ErrorAction SilentlyContinue | Stop-Process -Force -ErrorAction SilentlyContinue
    Remove-Item -Recurse -Force (Join-Path $Global:CC_Root '_run') -ErrorAction SilentlyContinue
    Remove-Item -Recurse -Force (Join-Path $Global:CC_Root '_cache') -ErrorAction SilentlyContinue
}

function claude {
    # Fail-closed: claude only ever talks to the local proxy. If AmneziaWG is
    # down, wireproxy can't reach upstream -> requests fail, nothing leaks out.
    $oldS = $env:HTTPS_PROXY; $oldH = $env:HTTP_PROXY
    $env:HTTPS_PROXY = $Global:CC_ProxyUrl
    $env:HTTP_PROXY  = $Global:CC_ProxyUrl

    # Config isolation: scrub host env vars that would override the stick config
    # (ANTHROPIC_*, CLAUDE_CODE_*). CLAUDE_CONFIG_DIR is kept (different prefix),
    # so all MCP/skills/rules/settings/auth come ONLY from the stick.
    $saved = @{}
    Get-ChildItem Env: | Where-Object { $_.Name -like 'ANTHROPIC_*' -or $_.Name -like 'CLAUDE_CODE_*' } | ForEach-Object {
        $saved[$_.Name] = $_.Value
        Remove-Item "Env:$($_.Name)" -ErrorAction SilentlyContinue
    }

    try   { & $Global:CC_ClaudeExe @args }
    finally {
        if ($null -ne $oldS) { $env:HTTPS_PROXY = $oldS } else { Remove-Item Env:HTTPS_PROXY -ErrorAction SilentlyContinue }
        if ($null -ne $oldH) { $env:HTTP_PROXY  = $oldH } else { Remove-Item Env:HTTP_PROXY  -ErrorAction SilentlyContinue }
        foreach ($k in $saved.Keys) { Set-Item "Env:$k" $saved[$k] }
    }
}

# Where claude starts (CWD = its "project root"). Default: the HOST user's home,
# so you work on their files, not the stick. Override: set CC_WORKDIR before
# launching, or just `cd` to the folder you're fixing.
if (-not $env:CC_WORKDIR) { $env:CC_WORKDIR = $Global:CC_HostHome }
if (Test-Path $env:CC_WORKDIR) { Set-Location $env:CC_WORKDIR }

# Warn about host managed policy (system-level, CANNOT be overridden by the stick)
$managed = @(
    "$env:ProgramFiles\ClaudeCode\managed-settings.json",
    "$env:ProgramData\ClaudeCode\managed-settings.json"
) | Where-Object { Test-Path $_ }
if ($managed) {
    Write-Host ""
    Write-Host "  [!] Host has managed Claude policy that the stick CANNOT override:" -ForegroundColor Yellow
    $managed | ForEach-Object { Write-Host "      $_" -ForegroundColor Yellow }
}

Write-Host ""
Write-Host "  ClaudeCodePortable ready." -ForegroundColor Green
Write-Host "  'claude'      -> tunneled via AmneziaWG (kill-switch on)" -ForegroundColor Gray
Write-Host "  anything else -> direct, no VPN" -ForegroundColor Gray
Write-Host ""

# Auto-launch claude when Start.bat set CCP_AUTOCLAUDE=1. Cleared first so a manual
# re-dot-source of this profile (same session) doesn't re-trigger it. `claude` is the
# wrapper function defined above; -NoExit keeps the shell after it exits.
if ($env:CCP_AUTOCLAUDE -eq '1') { $env:CCP_AUTOCLAUDE = $null; claude }
