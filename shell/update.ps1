# Updates every portable component in place. Run via Update.bat (standalone,
# NOT while a Start.bat session is open). Each component is isolated in
# try/catch so one failure doesn't abort the rest. Tool zips are swapped
# atomically (extract to temp -> replace) so a failed download never corrupts a
# working copy.
#
# Two progress bars:
#   Id 0  -> overall (component N of total)
#   Id 1  -> current tool (download %, then extract/replace)
param([switch]$ForceTools)   # -ForceTools re-installs node/go/pwsh even if same version (for testing)
$ErrorActionPreference = 'Stop'
$ProgressPreference = 'Continue'
# Constrained Language / AppLocker locks out unsigned scripts: our download +
# atomic-swap logic uses .NET types and reflection-ish calls that CLM forbids, so
# we'd fail mid-run with cryptic errors. Detect early and fail with a clear note.
if ($ExecutionContext.SessionState.LanguageMode -ne 'FullLanguage') {
    Write-Host "Host enforces Constrained Language / GPO policy -- portable install not possible" -ForegroundColor Red
    Write-Host "without code-signing our scripts. Aborting." -ForegroundColor Red
    exit 2
}
# TLS 1.2 for Windows PowerShell 5.1 (github/nodejs API need it; updater runs
# under system powershell.exe so it can replace the bundled pwsh without a lock).
try { [Net.ServicePointManager]::SecurityProtocol = [Net.ServicePointManager]::SecurityProtocol -bor [Net.SecurityProtocolType]::Tls12 } catch {}
$Root = Split-Path -Parent $PSScriptRoot
$env:CLAUDE_CONFIG_DIR = Join-Path $Root 'claude-cfg'
$env:Path = (Join-Path $Root 'node') + ';' + (Join-Path $Root 'go\bin') + ';' +
            (Join-Path $Root 'pwsh') + ';' + $env:Path

$STEPS = @('Claude Code','Plugins/Skills','MCP (npx)','Node','Go','PowerShell','Windows Terminal','wireproxy')
$total = $STEPS.Count
$script:idx = 0
function Step($name){
    $script:idx++
    Write-Progress -Id 0 -Activity "Updating ClaudeCodePortable" -Status "[$script:idx/$total] $name" -PercentComplete (($script:idx-1)*100/$total)
    Write-Host ""
    Write-Host "[$script:idx/$total] $name" -ForegroundColor White
}
function Ok($m){ Write-Host "   + $m" -ForegroundColor Green }
function Warn($m){ Write-Host "   ! $m" -ForegroundColor Yellow }
function EndTool(){ Write-Progress -Id 1 -ParentId 0 -Activity 'done' -Completed }

$tmp = Join-Path $env:TEMP ("ccupd_" + [guid]::NewGuid().ToString('N').Substring(0,8))
New-Item -ItemType Directory -Force -Path $tmp | Out-Null

# --- network with native-first, VPN-fallback -------------------------------
# Try the host's direct internet first; if it fails (censored github/nodejs, or
# just flaky), bring up our own AmneziaWG tunnel and retry every download/API
# through it. Once the VPN is up, later transfers go straight through it.
$script:VpnProxy = $null
$script:VpnStartedByUs = $false

function Ensure-Vpn {
    if ($script:VpnProxy) { return $script:VpnProxy }
    Warn "direct internet failed -> bringing up AmneziaWG VPN for downloads ..."
    $vpn = Get-ChildItem (Join-Path $Root 'AMNEZIA') -Filter *.vpn -ErrorAction SilentlyContinue | Select-Object -First 1
    if (-not $vpn) { throw "no AMNEZIA\*.vpn to fall back on" }
    $run = Join-Path $Root '_run'; New-Item -ItemType Directory -Force -Path $run | Out-Null
    $awg = Join-Path $run 'awg.update.conf'
    & (Join-Path $Root 'shell\decode-vpn.ps1') -In $vpn.FullName -Out $awg | Out-Null
    $pc = Join-Path $run 'proxy.update.conf'
    "WGConfig = $($awg -replace '\\','/')`r`n`r`n[http]`r`nBindAddress = 127.0.0.1:25345" | Set-Content $pc -Encoding ascii
    Start-Process -WindowStyle Hidden (Join-Path $Root 'wireproxy\wireproxy.exe') -ArgumentList @('-s','-c',$pc,'-i','127.0.0.1:9099')
    $script:VpnStartedByUs = $true
    $ok = $false
    for ($i=0; $i -lt 15; $i++) {
        try { if ((Invoke-WebRequest 'http://127.0.0.1:9099/readyz' -TimeoutSec 2 -UseBasicParsing).StatusCode -eq 200) { $ok=$true; break } } catch {}
        Start-Sleep -Seconds 2
    }
    if (-not $ok) { throw "VPN proxy did not become ready" }
    $script:VpnProxy = 'http://127.0.0.1:25345'
    Ok "VPN up -> downloads now go through the tunnel"
    return $script:VpnProxy
}

# One streaming download attempt-set (retries) against a given proxy ($null=direct)
function Download-Try($url, $out, $label, $proxy, $attempts){
    for ($try = 1; $try -le $attempts; $try++) {
        try {
            $req = [System.Net.HttpWebRequest]::Create($url)
            $req.UserAgent = 'ccportable-updater'
            $req.Timeout = 30000; $req.ReadWriteTimeout = 60000
            if ($proxy) { $req.Proxy = New-Object System.Net.WebProxy($proxy) } else { $req.Proxy = $null }
            $resp = $req.GetResponse(); $tot = $resp.ContentLength
            $in = $resp.GetResponseStream(); $fs = [System.IO.File]::Create($out)
            $buf = New-Object byte[] 1048576; $sum = 0; $r = 0
            $via = if ($proxy) { ' via VPN' } else { '' }
            try {
                while (($r = $in.Read($buf,0,$buf.Length)) -gt 0) {
                    $fs.Write($buf,0,$r); $sum += $r
                    if ($tot -gt 0) {
                        $pct = [math]::Min(100, [int]($sum*100/$tot))
                        $sfx = if ($try -gt 1) { " (retry $try)" } else { "" }
                        Write-Progress -Id 1 -ParentId 0 -Activity "$label$via$sfx" -Status ("{0:N1}/{1:N1} MB" -f ($sum/1MB),($tot/1MB)) -PercentComplete $pct
                    }
                }
            } finally { $fs.Close(); $in.Close(); $resp.Close() }
            return
        } catch {
            Remove-Item $out -Force -ErrorAction SilentlyContinue
            if ($try -eq $attempts) { throw }
            Start-Sleep -Seconds ([math]::Min(10, $try * 2))
        }
    }
}

# Download with native-first then VPN-fallback
function Download($url, $out, $label){
    if ($script:VpnProxy) { Download-Try $url $out $label $script:VpnProxy 4; return }
    try { Download-Try $url $out $label $null 2; return } catch {}
    Download-Try $url $out $label (Ensure-Vpn) 4
}

# Version/API JSON fetch: retry, then native-first -> VPN-fallback like Download
function Get-Json-Try($url, $proxy, $attempts){
    for ($t = 1; $t -le $attempts; $t++) {
        try {
            if ($proxy) { return Invoke-RestMethod -Uri $url -Headers @{ 'User-Agent'='ccportable-updater' } -TimeoutSec 30 -Proxy $proxy }
            else        { return Invoke-RestMethod -Uri $url -Headers @{ 'User-Agent'='ccportable-updater' } -TimeoutSec 30 }
        } catch { if ($t -eq $attempts) { throw }; Start-Sleep -Seconds ([math]::Min(6, $t*2)) }
    }
}
function Get-Json($url){
    if ($script:VpnProxy) { return Get-Json-Try $url $script:VpnProxy 3 }
    try { return Get-Json-Try $url $null 2 } catch {}
    return Get-Json-Try $url (Ensure-Vpn) 3
}

function Get-Zip($url, $name){
    $zip = Join-Path $tmp "$name.zip"
    Download $url $zip "downloading $name"
    Write-Progress -Id 1 -ParentId 0 -Activity "extracting $name" -Status 'please wait' -PercentComplete 100
    $ex = Join-Path $tmp $name
    Expand-Archive -Path $zip -DestinationPath $ex -Force
    return $ex
}

# Atomically replace $dest with $srcDir, preserving $keep entries (e.g. wt\.portable)
function Swap-Dir($srcDir, $dest, $keep){
    $stash = @{}
    foreach($k in $keep){ $p = Join-Path $dest $k; if(Test-Path $p){ $s=Join-Path $tmp "keep_$k"; Copy-Item $p $s -Recurse -Force; $stash[$k]=$s } }
    $bak = "$dest.old"
    if (Test-Path $bak) { Remove-Item -Recurse -Force $bak }
    # Fresh install: no existing $dest to stash aside. Update: move it to .old.
    $hadDest = Test-Path $dest
    if ($hadDest) { Rename-Item $dest $bak }
    try {
        New-Item -ItemType Directory -Force -Path $dest | Out-Null
        Copy-Item (Join-Path $srcDir '*') $dest -Recurse -Force
        foreach($k in $stash.Keys){ Copy-Item $stash[$k] (Join-Path $dest $k) -Recurse -Force }
        if ($hadDest) { Remove-Item -Recurse -Force $bak }
    } catch {
        # rollback: drop the partial new dir; restore the old one if there was one
        if (Test-Path $dest) { Remove-Item -Recurse -Force $dest }
        if ($hadDest) { Rename-Item $bak $dest }
        throw
    }
}

# claude.exe via verified manifest download (no `claude.exe install`, which edits
# host PATH/registry/.claude). Skip if already current; on checksum mismatch leave
# no half-written exe behind. Reuses Download/Get-Json/$tmp from above.
function Ensure-Claude($Root,$bin){
    $base = 'https://downloads.claude.ai/claude-code-releases'
    $latest = (Get-Json "$base/latest"); if ($latest -isnot [string]) { $latest = "$latest" }
    $latest = $latest.Trim(); if ($latest -notmatch '^\d+\.\d+\.\d+') { throw "bad latest: $latest" }
    $cur = '(none)'; if (Test-Path $bin) { try { $cur = ((& $bin --version) -split ' ')[0] } catch {} }
    if ($cur -eq $latest) { Ok "up to date ($cur)"; return }
    $man = Get-Json "$base/$latest/manifest.json"
    $sum = $man.platforms.'win32-x64'.checksum
    if (-not $sum) { throw "no win32-x64 checksum in manifest" }
    $tmpExe = Join-Path $tmp 'claude.exe'
    Download "$base/$latest/win32-x64/claude.exe" $tmpExe "downloading claude $latest"
    $got = (Get-FileHash -Path $tmpExe -Algorithm SHA256).Hash.ToLower()
    if ($got -ne $sum.ToLower()) { Remove-Item $tmpExe -Force -ErrorAction SilentlyContinue; throw "claude checksum mismatch" }
    New-Item -ItemType Directory -Force -Path (Split-Path $bin) | Out-Null
    Copy-Item $tmpExe $bin -Force
    Ok "claude $cur -> $latest"
}

Write-Host "=== ClaudeCodePortable updater ===" -ForegroundColor White
$claude = Join-Path $Root 'bin\claude.exe'
# Pre-create bin\ so a fresh stick (no claude.exe yet) has somewhere to land it.
New-Item -ItemType Directory -Force -Path (Join-Path $Root 'bin') | Out-Null

# Transport decision: native first. If the host's direct internet can't reach
# github, route the WHOLE update (claude + npm included) through our AmneziaWG
# VPN by exporting HTTPS_PROXY and priming the proxy for Download/Get-Json.
try {
    Invoke-WebRequest 'https://api.github.com' -TimeoutSec 8 -UseBasicParsing -ErrorAction Stop | Out-Null
    Write-Host "  direct internet OK" -ForegroundColor DarkGray
} catch {
    try {
        Ensure-Vpn | Out-Null
        $env:HTTPS_PROXY = $script:VpnProxy; $env:HTTP_PROXY = $script:VpnProxy
        Write-Host "  no direct internet -> whole update routed via VPN" -ForegroundColor DarkGray
    } catch { Warn "no direct net and VPN unavailable: $($_.Exception.Message)" }
}

# 1) Claude Code (verified manifest download; never `claude update`/`install`)
Step 'Claude Code'
try { Ensure-Claude $Root $claude } catch { Warn "failed: $($_.Exception.Message)" }

# 2) Plugins / skills (install-if-missing + update). enabledPlugins in
#    settings.json alone does NOT install; each plugin's marketplace must be
#    `marketplace add`ed first (even claude-plugins-official, for a scripted
#    pre-first-launch install), then `plugin install`. Marketplace name->repo
#    isn't derivable from the name@marketplace strings, so it's mapped here.
Step 'Plugins/Skills'
$MarketRepo = @{
    'claude-plugins-official' = 'anthropics/claude-plugins-official'
    'caveman'                 = 'JuliusBrussee/caveman'
    'impeccable'              = 'pbakaus/impeccable'
}
try {
    $st = Get-Content (Join-Path $env:CLAUDE_CONFIG_DIR 'settings.json') -Raw | ConvertFrom-Json
    $enabled = @($st.enabledPlugins.PSObject.Properties.Name)
    # add each referenced marketplace (idempotent)
    foreach($m in ($enabled | ForEach-Object { ($_ -split '@')[-1] } | Select-Object -Unique)){
        if ($MarketRepo[$m]) { try { & $claude plugin marketplace add $MarketRepo[$m] *> $null } catch {} }
        else { Warn "unknown marketplace '$m' - add name->repo to `$MarketRepo" }
    }
    try { & $claude plugin marketplace update *> $null } catch {}
    # install (covers fresh) then update (refresh existing)
    foreach($p in $enabled){
        try { & $claude plugin install $p *> $null } catch {}
        try { & $claude plugin update  $p *> $null; Ok "ensured $p" } catch { Warn "$p : $($_.Exception.Message)" }
    }
} catch { Warn "failed: $($_.Exception.Message)" }

# 3) MCP npx cache -> next launch pulls latest.
#    Via cmd /c so npm's "--force" stderr warning isn't treated as a PS error.
Step 'MCP (npx)'
try {
    $npm = Join-Path $Root 'node\npm.cmd'
    if (-not (Test-Path $npm)) { Ok "node not present yet - skipping (npx pulls latest on first use)" }
    else {
    cmd /c "`"$npm`" cache clean --force >nul 2>nul"
    if ($LASTEXITCODE -eq 0) { Ok "npm cache cleared (npx MCP pull latest next run)" }
    else { Warn "npm cache clean exit $LASTEXITCODE" }
    }
} catch { Warn "failed: $($_.Exception.Message)" }

# 4) Node
Step 'Node'
try {
    $nodeExe = Join-Path $Root 'node\node.exe'
    $cur = if (Test-Path $nodeExe) { (& $nodeExe --version).Trim() } else { '(none)' }
    $lts = ((Get-Json 'https://nodejs.org/dist/index.json') | Where-Object { $_.lts } | Select-Object -First 1).version
    if ($ForceTools -or $cur -ne $lts) {
        $ex = Get-Zip "https://nodejs.org/dist/$lts/node-$lts-win-x64.zip" 'node'
        Swap-Dir (Join-Path $ex "node-$lts-win-x64") (Join-Path $Root 'node') @()
        Ok "node $cur -> $lts"
    } else { Ok "up to date ($cur)" }
} catch { Warn "failed: $($_.Exception.Message)" }
EndTool

# 5) Go
Step 'Go'
try {
    $goExe = Join-Path $Root 'go\bin\go.exe'
    $cur = if (Test-Path $goExe) { ((& $goExe version) -split ' ')[2] } else { '(none)' }
    $latest = ((Get-Json 'https://go.dev/dl/?mode=json') | Where-Object { $_.stable } | Select-Object -First 1).version
    if ($ForceTools -or $cur -ne $latest) {
        $ex = Get-Zip "https://go.dev/dl/$latest.windows-amd64.zip" 'go'
        Swap-Dir (Join-Path $ex 'go') (Join-Path $Root 'go') @()
        Ok "go $cur -> $latest"
    } else { Ok "up to date ($cur)" }
} catch { Warn "failed: $($_.Exception.Message)" }
EndTool

# 6) PowerShell 7 — STAGED. The updater itself runs under the bundled pwsh, so it
#    can't replace its own folder live. We extract to pwsh.new; Update.bat swaps
#    it in via cmd after this script exits (cmd doesn't lock pwsh).
Step 'PowerShell'
try {
    $pwshExe = Join-Path $Root 'pwsh\pwsh.exe'
    $cur = if (Test-Path $pwshExe) { ((& $pwshExe --version) -replace 'PowerShell ','').Trim() } else { '(none)' }
    $rel = Get-Json 'https://api.github.com/repos/PowerShell/PowerShell/releases/latest'
    $latest = $rel.tag_name.TrimStart('v')
    if ($ForceTools -or $cur -ne $latest) {
        $asset = ($rel.assets | Where-Object { $_.name -eq "PowerShell-$latest-win-x64.zip" }).browser_download_url
        $ex = Get-Zip $asset 'pwsh'
        if (Test-Path $pwshExe) {
            # in-place replace: the running pwsh locks its own folder, so stage it
            # and let Update.bat swap pwsh.new -> pwsh via cmd after we exit.
            $stage = Join-Path $Root 'pwsh.new'
            if (Test-Path $stage) { Remove-Item -Recurse -Force $stage }
            Copy-Item $ex $stage -Recurse -Force
            Ok "pwsh $cur -> $latest (staged; Update.bat applies it on exit)"
        } else {
            # fresh install: nothing locked, drop it straight into pwsh\.
            Copy-Item $ex (Join-Path $Root 'pwsh') -Recurse -Force
            Ok "pwsh $cur -> $latest"
        }
    } else { Ok "up to date ($cur)" }
} catch { Warn "failed: $($_.Exception.Message)" }
EndTool

# 7) Windows Terminal (no exe --version; track installed tag in wt\.wtversion)
Step 'Windows Terminal'
try {
    $rel = Get-Json 'https://api.github.com/repos/microsoft/terminal/releases/latest'
    $stampF = Join-Path $Root 'wt\.wtversion'
    $cur = if (Test-Path $stampF) { (Get-Content $stampF -Raw).Trim() } else { '(none)' }
    if ($ForceTools -or $cur -ne $rel.tag_name) {
        $asset = ($rel.assets | Where-Object { $_.name -like 'Microsoft.WindowsTerminal_*_x64.zip' -and $_.name -notlike '*PreinstallKit*' } | Select-Object -First 1)
        $ex = Get-Zip $asset.browser_download_url 'wt'
        $inner = Get-ChildItem $ex -Directory | Where-Object { $_.Name -like 'terminal-*' } | Select-Object -First 1
        Swap-Dir $inner.FullName (Join-Path $Root 'wt') @('.portable')
        Set-Content -Path (Join-Path $Root 'wt\.wtversion') -Value $rel.tag_name -NoNewline
        Ok "Windows Terminal $cur -> $($rel.tag_name)"
    } else { Ok "up to date ($cur)" }
} catch { Warn "failed: $($_.Exception.Message)" }
EndTool

# 8) wireproxy-awg (exe --version vs latest release tag)
Step 'wireproxy'
try {
    $rel = Get-Json 'https://api.github.com/repos/artem-russkikh/wireproxy-awg/releases/latest'
    $wpExe = Join-Path $Root 'wireproxy\wireproxy.exe'
    $cur = if (Test-Path $wpExe) { (((& $wpExe --version) -split 'version ')[-1]).Trim() } else { '(none)' }
    if ($ForceTools -or $cur -ne $rel.tag_name) {
        $asset = ($rel.assets | Where-Object { $_.name -eq 'wireproxy_windows_amd64.tar.gz' }).browser_download_url
        $tgz = Join-Path $tmp 'wp.tar.gz'
        Download $asset $tgz 'downloading wireproxy'
        New-Item -ItemType Directory -Force -Path (Join-Path $Root 'wireproxy') | Out-Null
        & tar -xzf $tgz -C (Join-Path $Root 'wireproxy')
        Ok "wireproxy $cur -> $($rel.tag_name)"
    } else { Ok "up to date ($cur)" }
} catch { Warn "failed: $($_.Exception.Message)" }
EndTool

Write-Progress -Id 0 -Activity 'Updating ClaudeCodePortable' -Completed
# Best-effort with retries: a just-finished extraction can briefly hold a handle,
# leaving an empty temp dir behind. Retry so %TEMP% is left clean (no trace).
for ($i = 0; $i -lt 5 -and (Test-Path $tmp); $i++) {
    Remove-Item -Recurse -Force $tmp -ErrorAction SilentlyContinue
    if (Test-Path $tmp) { Start-Sleep -Milliseconds 400 }
}

# Tear down the fallback VPN if WE started it (keep the stick clean, no leftover
# proxy env or _run with the decoded key).
if ($script:VpnStartedByUs) {
    Get-Process wireproxy -ErrorAction SilentlyContinue | Stop-Process -Force -ErrorAction SilentlyContinue
    Remove-Item Env:HTTPS_PROXY, Env:HTTP_PROXY -ErrorAction SilentlyContinue
    Remove-Item -Recurse -Force (Join-Path $Root '_run') -ErrorAction SilentlyContinue
    Write-Host "  fallback VPN stopped, temp wiped" -ForegroundColor DarkGray
}

Write-Host ""
Write-Host "=== update done ===" -ForegroundColor White
# Per-component failures are warnings, not a run failure; don't leak a stray
# non-zero $LASTEXITCODE (e.g. from a guarded cmd /c) to the bootstrap caller.
exit 0
