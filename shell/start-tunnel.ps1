<#
  start-tunnel.ps1 — launch the wireproxy AmneziaWG userspace proxy fully hidden.

  Why a helper instead of `start /MIN`:
    `start "" /MIN wireproxy.exe` still creates a console window that lives in the
    taskbar. We want zero visible window. ProcessStartInfo with CreateNoWindow=$true
    spawns the proxy with no console at all, and because the process is NOT placed in
    a kill-on-close job object it keeps running after this pwsh (and the launcher)
    exit — exactly the "survives" behaviour Start.bat relied on.

  ArgumentList is used so paths containing spaces are quoted correctly by .NET.

  On success the proxy's PID is written to stdout (last line) so the caller can
  record it as the session marker and later kill ONLY this session's wireproxy.
#>
[CmdletBinding()]
param(
    [Parameter(Mandatory)][string]$Exe,
    [Parameter(Mandatory)][string]$Config,
    [string]$PidFile
)

if (-not (Test-Path -LiteralPath $Exe))    { Write-Error "wireproxy not found: $Exe";    exit 1 }
if (-not (Test-Path -LiteralPath $Config)) { Write-Error "proxy config not found: $Config"; exit 1 }

$psi = [System.Diagnostics.ProcessStartInfo]::new()
$psi.FileName        = $Exe
$psi.ArgumentList.Add('-s')
$psi.ArgumentList.Add('-c')
$psi.ArgumentList.Add($Config)
$psi.UseShellExecute  = $false   # required for CreateNoWindow to take effect
$psi.CreateNoWindow   = $true    # no console window, ever
# NOTE: WindowStyle is intentionally left at its default (Normal): with
# UseShellExecute=$false it has no effect anyway, and CreateNoWindow is what
# actually suppresses the console window.

try {
    $p = [System.Diagnostics.Process]::Start($psi)
} catch {
    Write-Error "Failed to start wireproxy: $($_.Exception.Message)"
    exit 1
}

if (-not $p) { Write-Error 'wireproxy did not start'; exit 1 }

# A config can pass `wireproxy -n` validation yet die at runtime (bad key,
# unreachable endpoint, port already bound). Give it a moment, then confirm
# it is still alive so the launcher doesn't proceed with a dead tunnel.
Start-Sleep -Milliseconds 400
if ($p.HasExited) {
    Write-Error "wireproxy exited immediately (code $($p.ExitCode)) - check the .vpn config / endpoint"
    exit 1
}
# Record the PID for the launcher's session marker. Write it to a FILE, NOT stdout:
# wireproxy inherits this process's stdout handle (we don't redirect it), so a caller
# capturing the helper's stdout via `for /f` would block on EOF until wireproxy dies
# — i.e. the launcher would hang forever after starting the tunnel. A file sidesteps
# the inherited-pipe hang entirely.
if ($PidFile) {
    [System.IO.File]::WriteAllText($PidFile, [string]$p.Id, [System.Text.UTF8Encoding]::new($false))
}
exit 0
