# Dot-sourced into the interactive pwsh session by Start.bat.
# Defines a `claude` wrapper that tunnels ONLY claude (+ its child tools)
# through the local AmneziaWG http-proxy. Everything else you type in this
# terminal goes out directly (no proxy).

$Root = Split-Path -Parent $PSScriptRoot              # shell\ -> portable root
$env:CLAUDE_CONFIG_DIR  = Join-Path $Root 'claude-cfg'
# Stable hooks dir for settings.json. The native binary resets CLAUDE_CONFIG_DIR
# for the worker/hook subprocesses it spawns, so hook commands keyed off
# $env:CLAUDE_CONFIG_DIR resolve against $HOME\.claude instead of the stick. This
# custom var is NOT touched by claude and is inherited by hooks unchanged.
$env:CCP_HOOKS = Join-Path $env:CLAUDE_CONFIG_DIR 'hooks'

# --- Plugin state is drive-letter / host-absolute: rebase to THIS stick --------
# claude freezes ABSOLUTE installPath/installLocation into plugins\*.json. Those
# pin the drive letter the stick last ran on (D:\...) AND can carry leaked HOST
# paths (C:\Users\<user>\.claude\plugins\...) from a pre-portable install. On
# another machine the letter differs and the host paths don't exist, so the
# affected plugins fail to load. Each launch, take the tail after '\plugins\' and
# re-root it on the current stick ([IO.Path]::Combine, not Join-Path, which would
# fail drive-existence checks). Rewrite only when something changed, so a settled
# stick does no extra USB writes. try/catch: a malformed state file never blocks launch.
$plugDir    = Join-Path $env:CLAUDE_CONFIG_DIR 'plugins'
$rebasePlug = { param($p) if ($p -and $p -match '[\\/]plugins[\\/](.+)$') { [System.IO.Path]::Combine($plugDir, $Matches[1]) } else { $p } }
$kmFile = Join-Path $plugDir 'known_marketplaces.json'
if (Test-Path $kmFile) {
    try {
        $km = Get-Content $kmFile -Raw | ConvertFrom-Json; $kmChg = $false
        foreach ($n in $km.PSObject.Properties.Name) {
            $e = $km.$n
            if ($e.installLocation) { $x = & $rebasePlug $e.installLocation; if ($x -ne $e.installLocation) { $e.installLocation = $x; $kmChg = $true } }
        }
        if ($kmChg) { $km | ConvertTo-Json -Depth 10 | Set-Content $kmFile -Encoding utf8 }
    } catch {}
}
$ipFile = Join-Path $plugDir 'installed_plugins.json'
if (Test-Path $ipFile) {
    try {
        $ip = Get-Content $ipFile -Raw | ConvertFrom-Json; $ipChg = $false
        foreach ($n in $ip.plugins.PSObject.Properties.Name) {
            foreach ($e in $ip.plugins.$n) {
                if ($e.installPath) { $x = & $rebasePlug $e.installPath; if ($x -ne $e.installPath) { $e.installPath = $x; $ipChg = $true } }
            }
        }
        if ($ipChg) { $ip | ConvertTo-Json -Depth 10 | Set-Content $ipFile -Encoding utf8 }
    } catch {}
}

# Bundled MCP secrets (e.g. GITHUB_PERSONAL_ACCESS_TOKEN). Dot-sourced here so the
# vars land in the session env and are inherited by the MCP server child processes
# claude spawns. mcp-secrets.ps1 is gitignored (per-stick); see mcp-secrets.example.ps1.
$ccSecrets = Join-Path $env:CLAUDE_CONFIG_DIR 'mcp-secrets.ps1'
# try/catch: a malformed secrets file must not abort the whole session launch.
if (Test-Path $ccSecrets) { try { . $ccSecrets } catch { Write-Warning "mcp-secrets.ps1 failed to load: $($_.Exception.Message)" } }

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

# Cold `npx` MCP pulls over the VPN can exceed the default 30s connect timeout
# (esp. on a fresh stick / after the updater clears caches), so the slowest server
# (sequential-thinking) failed to register. Raise it so the bundled MCP connects.
$env:MCP_TIMEOUT            = '120000'
$env:MCP_CONNECT_TIMEOUT_MS = '120000'

# Bundled toolchains FIRST on PATH so claude uses the portable node/pwsh,
# never the host's. Order: node, pwsh, statusline, then the host PATH.
$env:Path = (Join-Path $Root 'node') + ';' +
            (Join-Path $Root 'pwsh') + ';' +
            (Join-Path $Root 'statusline') + ';' + $env:Path

# Portable npm: PERSISTENT on-stick npm cache (NOT under _cache, so it survives
# exit). Wiping it each session forced a cold `npx` pull of every MCP server over
# the VPN on every launch; the slowest blew past the MCP connect timeout and never
# registered. Persisting it = warm npx after the first run. Stays on the stick, so
# the host profile is still untouched.
$env:npm_config_cache = Join-Path $Root '_npmcache'

# --- leave no trace on the host: no command history, no telemetry ---
try { Set-PSReadLineOption -HistorySaveStyle SaveNothing -ErrorAction SilentlyContinue } catch {}
$env:POWERSHELL_TELEMETRY_OPTOUT = '1'
$env:DOTNET_CLI_TELEMETRY_OPTOUT = '1'
$env:POWERSHELL_UPDATECHECK = 'Off'

$Global:CC_Root      = $Root

# --- Native-install layout on the stick ----------------------------------
# claude.exe is a self-managing launcher: it expects to live at
# $HOME\.local\bin\claude.exe with a matching copy under
# $HOME\.local\share\claude\versions\<ver>. Run from anywhere else it (a) warns
# "claude command ... missing or broken, run claude install", and (b) re-execs a
# worker that loses CLAUDE_CONFIG_DIR, so SessionStart/* hooks resolve against
# $HOME\.claude. `claude install` would fix this but writes the HOST registry (a
# `Claude` value + user PATH), so we replicate the layout by hand here -- HOME is
# pinned to the stick above, so everything stays on the stick, host untouched.
# Run a native exe with a HARD timeout; returns trimmed stdout, or $null on
# non-zero exit / timeout / launch failure. Mirrors update.ps1's Invoke-Timed:
# without it a present-but-broken claude.exe (missing runtime DLLs, hung probe)
# would block `--version` forever and freeze the whole session launch.
function Invoke-Timed($exe, [string[]]$cliArgs, [int]$timeoutMs = 10000) {
    try {
        $psi = [System.Diagnostics.ProcessStartInfo]::new()
        $psi.FileName = $exe
        foreach ($a in $cliArgs) { $psi.ArgumentList.Add($a) }
        $psi.UseShellExecute = $false; $psi.CreateNoWindow = $true
        $psi.RedirectStandardOutput = $true; $psi.RedirectStandardError = $true
        $p  = [System.Diagnostics.Process]::Start($psi)
        $so = $p.StandardOutput.ReadToEndAsync(); $null = $p.StandardError.ReadToEndAsync()
        if (-not $p.WaitForExit($timeoutMs)) { try { $p.Kill($true) } catch {}; return $null }
        if ($p.ExitCode -ne 0) { return $null }
        return $so.Result.Trim()
    } catch { return $null }
}

$srcExe     = Join-Path $Root 'bin\claude.exe'
$installExe = Join-Path $stickHome '.local\bin\claude.exe'
if (Test-Path $srcExe) {
    # Copy-Item preserves LastWriteTime, so equal timestamps => already in sync.
    $stale = (-not (Test-Path $installExe)) -or
             ((Get-Item $srcExe).LastWriteTimeUtc -ne (Get-Item $installExe).LastWriteTimeUtc)
    if ($stale) {
        New-Item -ItemType Directory -Force -Path (Split-Path $installExe) | Out-Null
        Copy-Item $srcExe $installExe -Force
        $ver = Invoke-Timed $srcExe @('--version') 10000
        if ($ver -match '\d+\.\d+\.\d+') {
            $vStore = Join-Path $stickHome ".local\share\claude\versions\$($Matches[0])"
            New-Item -ItemType Directory -Force -Path (Split-Path $vStore) | Out-Null
            Copy-Item $srcExe $vStore -Force
        }
    }
}
$Global:CC_ClaudeExe = (Test-Path $installExe) ? $installExe : $srcExe
$Global:CC_ProxyUrl  = 'http://127.0.0.1:25345'

# On window close: stop ONLY this session's VPN proxy and wipe the ephemeral _run
# dir (with the decoded key) so nothing temporary is left on the stick. We target
# the exact PID Start.bat recorded (CCP_TUNPID) instead of every wireproxy, so a
# second ClaudeCodePortable session running off the same stick is left untouched.
$Global:CC_TunPid = if ($env:CCP_TUNPID -match '^\d+$') { [int]$env:CCP_TUNPID } else { $null }
$Global:CC_Lock   = $env:CCP_LOCK
$null = Register-EngineEvent PowerShell.Exiting -Action {
    if ($Global:CC_TunPid) {
        # Stop-Process by Id, but only if it's still our wireproxy (PIDs get reused).
        $p = Get-Process -Id $Global:CC_TunPid -ErrorAction SilentlyContinue
        if ($p -and $p.ProcessName -eq 'wireproxy') { $p | Stop-Process -Force -ErrorAction SilentlyContinue }
    }
    if ($Global:CC_Lock) { Remove-Item -Force -LiteralPath $Global:CC_Lock -ErrorAction SilentlyContinue }
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

    # Load ONLY user settings (the stick's claude-cfg). The default also pulls
    # PROJECT/LOCAL settings from <cwd>\.claude — and since the workdir is the
    # HOST owner's home, that would execute the host's own ~/.claude hooks (e.g.
    # `$env:USERPROFILE\.claude\hooks\...`, which break because USERPROFILE is
    # repinned to the stick) and otherwise leak host config into this session.
    # --strict-mcp-config + --mcp-config pins the bundled MCP servers to the stick
    # file and ignores host/project MCP (.claude.json / cwd .mcp.json) for the same
    # isolation reason. Skip the MCP flags if the file is missing (no servers then).
    $pre = @('--setting-sources', 'user')
    $mcpCfg = Join-Path $env:CLAUDE_CONFIG_DIR 'mcp-servers.json'
    if (Test-Path $mcpCfg) { $pre += @('--strict-mcp-config', '--mcp-config', $mcpCfg) }
    try   { & $Global:CC_ClaudeExe @pre @args }
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
if ($env:CCP_AUTOCLAUDE -eq '1') {
    $env:CCP_AUTOCLAUDE = $null
    if (Test-Path $Global:CC_ClaudeExe) { claude }
    else { Write-Host "  [!] claude.exe missing ($Global:CC_ClaudeExe) - run 'Install or Update.bat'" -ForegroundColor Red }
}
