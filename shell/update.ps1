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
# pwsh 7.6 (.NET Core) negotiates TLS 1.2/1.3 from the OS by default — the old
# 5.1-era ServicePointManager hack was dead code (and pinning Tls12 would even
# block TLS 1.3), so it's gone. This engine only ever runs under bundled pwsh.
$Root = Split-Path -Parent $PSScriptRoot
$env:CLAUDE_CONFIG_DIR = Join-Path $Root 'claude-cfg'
$env:Path = (Join-Path $Root 'node') + ';' +
            (Join-Path $Root 'pwsh') + ';' + $env:Path

$STEPS = @('Claude Code','Node','Plugins/Skills','MCP (npx)','PowerShell','Windows Terminal','wireproxy','Statusline')
$total = $STEPS.Count
$script:idx = 0
function EndTool(){ Write-Progress -Id 1 -ParentId 0 -Activity 'done' -Completed }
function Step($name){
    # Clear any leftover child (download/extract) bar from the previous step so a
    # finished "NNN/NNN MB" line can't linger into this (possibly long) step, and
    # refresh the parent bar so its "[N/total] <name>" status always advances.
    EndTool
    $script:idx++
    Write-Progress -Id 0 -Activity "Updating ClaudeCodePortable" -Status "[$script:idx/$total] $name" -PercentComplete (($script:idx-1)*100/$total)
    Write-Host ""
    Write-Host "[$script:idx/$total] $name" -ForegroundColor White
}
function Ok($m){ Write-Host "   + $m" -ForegroundColor Green }
function Warn($m){ Write-Host "   ! $m" -ForegroundColor Yellow }

$tmp = Join-Path $env:TEMP ("ccupd_" + [guid]::NewGuid().ToString('N').Substring(0,8))
New-Item -ItemType Directory -Force -Path $tmp | Out-Null

# --- network: direct downloads with retries --------------------------------
# The updater uses the host's DIRECT internet only — prepare the stick on an open
# network. (The AmneziaWG VPN is solely for the `claude` binary at RUNTIME, set up
# by Start.bat / start-tunnel.ps1 / profile.ps1 — never for fetching toolchains.)

# One streaming download with retries + a byte-level progress bar (Id 1).
function Download($url, $out, $label){
    $attempts = 4
    for ($try = 1; $try -le $attempts; $try++) {
        try {
            $req = [System.Net.HttpWebRequest]::Create($url)
            $req.UserAgent = 'ccportable-updater'
            $req.Timeout = 30000; $req.ReadWriteTimeout = 60000
            $req.Proxy = $null   # ignore any host system proxy; go straight out
            $resp = $req.GetResponse(); $tot = $resp.ContentLength
            $in = $resp.GetResponseStream(); $fs = [System.IO.File]::Create($out)
            $buf = New-Object byte[] 1048576; $sum = 0; $r = 0
            try {
                while (($r = $in.Read($buf,0,$buf.Length)) -gt 0) {
                    $fs.Write($buf,0,$r); $sum += $r
                    if ($tot -gt 0) {
                        $pct = [math]::Min(100, [int]($sum*100/$tot))
                        $sfx = if ($try -gt 1) { " (retry $try)" } else { "" }
                        Write-Progress -Id 1 -ParentId 0 -Activity "$label$sfx" -Status ("{0:N1}/{1:N1} MB" -f ($sum/1MB),($tot/1MB)) -PercentComplete $pct
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

# Version/API JSON fetch with retries (direct).
function Get-Json($url){
    $attempts = 3
    for ($t = 1; $t -le $attempts; $t++) {
        try {
            return Invoke-RestMethod -Uri $url -Headers @{ 'User-Agent'='ccportable-updater' } -TimeoutSec 30 -ProgressAction SilentlyContinue
        } catch { if ($t -eq $attempts) { throw }; Start-Sleep -Seconds ([math]::Min(6, $t*2)) }
    }
}

# Extract a zip with a MOVING per-file bar. Expand-Archive emits no usable progress,
# so we pinned a static 100% bar that looked frozen for the ~minute it takes to
# unpack the ~10k-file Go/pwsh distros. Iterate entries via System.IO.Compression
# and drive Id 1 ourselves, throttled to redraw only when the integer percent
# changes (10k entries would otherwise flood the host).
function Expand-WithProgress($zip, $dest, $label){
    $arch = [System.IO.Compression.ZipFile]::OpenRead($zip)
    try {
        $destFull = [System.IO.Path]::GetFullPath($dest)
        [System.IO.Directory]::CreateDirectory($destFull) | Out-Null
        $n = $arch.Entries.Count; $i = 0; $lastPct = -1
        foreach($e in $arch.Entries){
            $i++
            $target = [System.IO.Path]::GetFullPath([System.IO.Path]::Combine($destFull, $e.FullName))
            # zip-slip guard: trusted sources, but never let an entry escape $dest.
            if (-not $target.StartsWith($destFull, [System.StringComparison]::OrdinalIgnoreCase)) { throw "zip entry escapes dest: $($e.FullName)" }
            if ($e.FullName.EndsWith('/')) {
                [System.IO.Directory]::CreateDirectory($target) | Out-Null
            } else {
                [System.IO.Directory]::CreateDirectory([System.IO.Path]::GetDirectoryName($target)) | Out-Null
                [System.IO.Compression.ZipFileExtensions]::ExtractToFile($e, $target, $true)
            }
            $pct = [int]($i*100/[math]::Max(1,$n))
            if ($pct -ne $lastPct) {
                Write-Progress -Id 1 -ParentId 0 -Activity "extracting $label" -Status "$i/$n files" -PercentComplete $pct
                $lastPct = $pct
            }
        }
    } finally { $arch.Dispose() }
}

function Get-Zip($url, $name){
    $zip = Join-Path $tmp "$name.zip"
    Download $url $zip "downloading $name"
    $ex = Join-Path $tmp $name
    Expand-WithProgress $zip $ex $name
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
        # Copy the tree FILE-BY-FILE so the USB bar moves continuously. The old
        # per-top-level Copy-Item -Recurse pinned the bar on the huge go\src / go\pkg
        # entry for minutes (looked frozen mid-copy). Progress is byte-weighted (file
        # sizes vary wildly), throttled to integer-percent changes. GetRelativePath is
        # 8.3 short/long-form agnostic (see sync-files.ps1) so subdirs are preserved
        # instead of silently flattened; -Force enumeration keeps hidden/dot-files.
        $srcRoot = (Resolve-Path -LiteralPath $srcDir).Path
        # dirs first: cheap (local TEMP), preserves empty dirs, guarantees each file's
        # parent exists so the hot copy loop needs no per-file Test-Path on slow USB.
        foreach($d in (Get-ChildItem -LiteralPath $srcRoot -Recurse -Directory -Force)){
            $rel = [System.IO.Path]::GetRelativePath($srcRoot, $d.FullName)
            [System.IO.Directory]::CreateDirectory((Join-Path $dest $rel)) | Out-Null
        }
        $files = Get-ChildItem -LiteralPath $srcRoot -Recurse -File -Force
        $n = $files.Count
        $bytes = [int64](($files | Measure-Object -Property Length -Sum).Sum); if ($bytes -le 0) { $bytes = 1 }
        $i = 0; $done = [int64]0; $lastPct = -1
        foreach($f in $files){
            $i++
            $rel = [System.IO.Path]::GetRelativePath($srcRoot, $f.FullName)
            if ([System.IO.Path]::IsPathRooted($rel) -or $rel.StartsWith('..')) { throw "cannot compute relative path for '$($f.FullName)' under '$srcRoot'" }
            [System.IO.File]::Copy($f.FullName, (Join-Path $dest $rel), $true)
            $done += $f.Length
            $pct = [int]($done*100/$bytes)
            if ($pct -ne $lastPct) {
                Write-Progress -Id 1 -ParentId 0 -Activity "copying to stick" -Status ("{0}/{1} files  {2:N0}/{3:N0} MB" -f $i,$n,($done/1MB),($bytes/1MB)) -PercentComplete $pct
                $lastPct = $pct
            }
        }
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
    # Probe the INSTALLED claude version so we can skip the (large) re-download when
    # already current. `claude --version` prints e.g. "2.1.190 (Claude Code)"; grab the
    # leading semver. Redirect stderr (2>$null) so a noisy exe can't trip the global
    # ErrorActionPreference='Stop'. A missing/broken exe leaves $installed=$null, which
    # never equals $latest -> we fall through and (re)install.
    $installed = $null
    if (Test-Path $bin) {
        try {
            $verOut = (& $bin --version 2>$null | Out-String)
            if ($verOut -match '\d+\.\d+\.\d+') { $installed = $Matches[0] }
        } catch { $installed = $null }
    }
    $cur = if ($installed) { $installed } else { '(none)' }
    if ($installed -eq $latest) { Ok "Claude Code $cur up to date"; return }
    $man = Get-Json "$base/$latest/manifest.json"
    $sum = $man.platforms.'win32-x64'.checksum
    if (-not $sum) { throw "no win32-x64 checksum in manifest" }
    $tmpExe = Join-Path $tmp 'claude.exe'
    Download "$base/$latest/win32-x64/claude.exe" $tmpExe "downloading claude $latest"
    EndTool   # download done -> clear the child bar so it doesn't linger into the next step
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
# Pre-create the VPN config dir so a fresh stick has an obvious drop spot.
New-Item -ItemType Directory -Force -Path (Join-Path $Root 'Amnezia config') | Out-Null

# Wrap the whole component sequence so the progress-bar cleanup in finally ALWAYS
# runs, even if a step throws past its own try/catch.
try {

# 1) Claude Code (verified manifest download; never `claude update`/`install`)
Step 'Claude Code'
try { Ensure-Claude $Root $claude } catch { Warn "failed: $($_.Exception.Message)" }

# 2) Node — bundled toolchain FIRST after claude, so the MCP step (npx) and any
#    plugin tooling have node available on a fresh stick (was step 4, ran AFTER
#    MCP -> npm.cmd missing -> "node not present yet - skipping").
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

# 3) Plugins / skills (install-if-missing + update). enabledPlugins in
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
    # Local count — do NOT reuse $total (that's the script-scope STEPS count used by
    # Step()'s "[idx/total]" overall bar; clobbering it garbles steps 3-9).
    $plugTotal = [math]::Max(1, $enabled.Count)

    # Show the child bar (with count) BEFORE the two cold-start `--json` snapshot
    # calls below — otherwise step 2 sits with no Id 1 bar for the ~2-4s those
    # launches take, so the bar appears to "show up late". PercentComplete 0 keeps
    # it determinate (never the static-full indeterminate -1).
    Write-Progress -Id 1 -ParentId 0 -Activity "Plugins/Skills (0/$plugTotal)" -Status 'reading marketplace + plugin lists' -PercentComplete 0

    # One-shot snapshots so we run a SINGLE claude.exe per plugin (install XOR
    # update) instead of both. Each claude launch is a ~1-2s cold start, so for 9
    # plugins this is 9 launches instead of 18 (plus we skip marketplace adds that
    # already exist). If a list call fails we fall back to the old install+update
    # so updates are never silently skipped.
    $haveMkt = @{}
    try { & $claude plugin marketplace list --json 2>$null | ConvertFrom-Json | ForEach-Object { $haveMkt[$_.name] = $true } } catch {}
    $havePlug = @{}; $plugListOk = $false
    try { & $claude plugin list --json 2>$null | ConvertFrom-Json | ForEach-Object { $havePlug[$_.id] = $true }; $plugListOk = $true } catch {}

    # add only marketplaces that aren't configured yet
    foreach($m in ($enabled | ForEach-Object { ($_ -split '@')[-1] } | Select-Object -Unique)){
        if (-not $MarketRepo[$m]) { Warn "unknown marketplace '$m' - add name->repo to `$MarketRepo"; continue }
        if ($haveMkt[$m]) { continue }
        Write-Progress -Id 1 -ParentId 0 -Activity "Plugins/Skills (0/$plugTotal)" -Status "adding marketplace $m" -PercentComplete 0
        Write-Host "   ... adding marketplace $m" -ForegroundColor DarkGray
        try { & $claude plugin marketplace add $MarketRepo[$m] *> $null; $haveMkt[$m] = $true } catch {}
    }
    # Refresh marketplace indices once so updates see the latest versions. This fetches
    # every marketplace over the network and, with no TTY + output swallowed, can hang
    # (slow VPN / unreachable repo). Cap it at 90s via a real Process so a stuck refresh
    # can't freeze step 2 — on timeout we continue with the cached indices.
    Write-Progress -Id 1 -ParentId 0 -Activity "Plugins/Skills (0/$plugTotal)" -Status 'refreshing marketplaces (network, up to 90s)' -PercentComplete 0
    Write-Host "   ... refreshing marketplaces" -ForegroundColor DarkGray
    try {
        $psi = [System.Diagnostics.ProcessStartInfo]::new()
        $psi.FileName = $claude
        foreach($a in @('plugin','marketplace','update')) { $psi.ArgumentList.Add($a) }
        $psi.UseShellExecute = $false; $psi.CreateNoWindow = $true
        $psi.RedirectStandardOutput = $true; $psi.RedirectStandardError = $true
        $rp = [System.Diagnostics.Process]::Start($psi)
        $null = $rp.StandardOutput.ReadToEndAsync(); $null = $rp.StandardError.ReadToEndAsync()  # drain pipes (a full buffer would deadlock)
        if (-not $rp.WaitForExit(90000)) { try { $rp.Kill($true) } catch {}; Warn "marketplace refresh timed out (90s) - continuing with cached indices" }
    } catch { Warn "marketplace refresh failed: $($_.Exception.Message)" }

    # install missing / update existing — ONE launch each, real N-of-total bar
    $i = 0
    foreach($p in $enabled){
        $i++
        $act = if (-not $plugListOk) { 'ensuring' } elseif ($havePlug[$p]) { 'updating' } else { 'installing' }
        Write-Progress -Id 1 -ParentId 0 -Activity "Plugins/Skills ($i/$plugTotal)" -Status "$act $p" -PercentComplete ([int]($i*100/$plugTotal))
        Write-Host "   ... [$i/$plugTotal] $act $p" -ForegroundColor DarkGray
        try {
            if (-not $plugListOk)      { & $claude plugin install $p *> $null; & $claude plugin update $p *> $null }
            elseif ($havePlug[$p])     { & $claude plugin update  $p *> $null }
            else                       { & $claude plugin install $p *> $null }
            Ok "ensured $p ($i/$plugTotal)"
        } catch { Warn "$p : $($_.Exception.Message)" }
    }
} catch { Warn "failed: $($_.Exception.Message)" }
EndTool   # clear the child bar so the indeterminate Plugins bar can't linger full into the next step

# 4) MCP npx cache -> next launch pulls latest.
#    Via cmd /c so npm's "--force" stderr warning isn't treated as a PS error.
Step 'MCP (npx)'
try {
    $npm = Join-Path $Root 'node\npm.cmd'
    if (-not (Test-Path $npm)) { Ok "node not present yet - skipping (npx pulls latest on first use)" }
    else {
    Write-Progress -Id 1 -ParentId 0 -Activity 'MCP (npx)' -Status 'clearing npm cache' -PercentComplete -1
    cmd /c "`"$npm`" cache clean --force >nul 2>nul"
    if ($LASTEXITCODE -eq 0) { Ok "npm cache cleared (npx MCP pull latest next run)" }
    else { Warn "npm cache clean exit $LASTEXITCODE" }
    }
} catch { Warn "failed: $($_.Exception.Message)" }
EndTool   # clear the child bar before the next step

# 5) PowerShell 7 — STAGED. The updater itself runs under the bundled pwsh, so it
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

# 6) Windows Terminal (no exe --version; track installed tag in wt\.wtversion)
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
        # Swap-Dir only *preserves* .portable; create it if absent so WT stays portable.
        $portMark = Join-Path $Root 'wt\.portable'
        if (-not (Test-Path $portMark)) { New-Item -ItemType File -Path $portMark -Force | Out-Null }
        Set-Content -Path (Join-Path $Root 'wt\.wtversion') -Value $rel.tag_name -NoNewline
        Ok "Windows Terminal $cur -> $($rel.tag_name)"
    } else { Ok "up to date ($cur)" }
} catch { Warn "failed: $($_.Exception.Message)" }
EndTool

# 7) wireproxy-awg (exe --version vs latest release tag)
Step 'wireproxy'
try {
    $rel = Get-Json 'https://api.github.com/repos/artem-russkikh/wireproxy-awg/releases/latest'
    # The fork's exe --version prints the upstream version, not this tag, so it
    # never matches and re-downloads every run. Track the installed tag in a stamp.
    $stampF = Join-Path $Root 'wireproxy\.wpversion'
    $cur = if (Test-Path $stampF) { (Get-Content $stampF -Raw).Trim() } else { '(none)' }
    if ($ForceTools -or $cur -ne $rel.tag_name) {
        $asset = ($rel.assets | Where-Object { $_.name -eq 'wireproxy_windows_amd64.tar.gz' }).browser_download_url
        $tgz = Join-Path $tmp 'wp.tar.gz'
        Download $asset $tgz 'downloading wireproxy'
        New-Item -ItemType Directory -Force -Path (Join-Path $Root 'wireproxy') | Out-Null
        # tar is a native exe (no PS Write-Progress to leak), but extracting still
        # takes a beat on USB — drive an indeterminate child bar so this step matches
        # the zip-based steps' "extracting" phase instead of going bar-less.
        Write-Progress -Id 1 -ParentId 0 -Activity 'extracting wireproxy' -Status 'unpacking' -PercentComplete -1
        & tar -xzf $tgz -C (Join-Path $Root 'wireproxy')
        # Don't stamp a broken/partial extract as "installed": a failed tar (or a
        # zip whose layout changed so wireproxy.exe isn't where we expect) would
        # otherwise be recorded as current and never re-tried.
        $wpExe = Join-Path $Root 'wireproxy\wireproxy.exe'
        if ($LASTEXITCODE -ne 0 -or -not (Test-Path $wpExe)) { throw "wireproxy extract failed (tar exit $LASTEXITCODE, exe present: $(Test-Path $wpExe))" }
        Set-Content -Path $stampF -Value $rel.tag_name -NoNewline
        Ok "wireproxy $cur -> $($rel.tag_name)"
    } else { Ok "up to date ($cur)" }
} catch { Warn "failed: $($_.Exception.Message)" }
EndTool

# 8) Statusline (morgott-statusline) — bundled on the stick. Single zero-dep ESM
#    bundle from the repo, saved as .mjs so node runs it as ESM (no package.json
#    needed). A PATH-resolved .cmd wrapper (drive-letter independent via %~dp0,
#    survives node upgrades) lets settings.json use the bare command name.
Step 'Statusline'
try {
    $slDir = Join-Path $Root 'statusline'
    New-Item -ItemType Directory -Force -Path $slDir | Out-Null
    Download 'https://raw.githubusercontent.com/UberMorgott/MorgottStatusLine/master/dist/index.js' (Join-Path $slDir 'index.mjs') 'downloading statusline'
    $wrap = '@"%~dp0..\node\node.exe" "%~dp0index.mjs" %*' + "`r`n"
    Set-Content -Path (Join-Path $slDir 'morgott-statusline.cmd') -Value $wrap -Encoding ascii -NoNewline
    Ok "statusline bundled"
} catch { Warn "failed: $($_.Exception.Message)" }
EndTool

Write-Progress -Id 0 -Activity 'Updating ClaudeCodePortable' -Completed
# Best-effort with retries: a just-finished extraction can briefly hold a handle,
# leaving an empty temp dir behind. Retry so %TEMP% is left clean (no trace).
for ($i = 0; $i -lt 5 -and (Test-Path $tmp); $i++) {
    Remove-Item -Recurse -Force $tmp -ErrorAction SilentlyContinue
    if (Test-Path $tmp) { Start-Sleep -Milliseconds 400 }
}

} finally {
# Clear BOTH bars on EVERY exit path (normal end AND any uncaught throw): the
# success path already completes Id 0 above, but a mid-run throw would skip it and
# leave a stale parent/child bar on screen. Idempotent if already completed.
EndTool
Write-Progress -Id 0 -Activity 'Updating ClaudeCodePortable' -Completed
}

Write-Host ""
Write-Host "=== update done ===" -ForegroundColor White
# Per-component failures are warnings, not a run failure; don't leak a stray
# non-zero $LASTEXITCODE (e.g. from a guarded cmd /c) to the bootstrap caller.
exit 0
