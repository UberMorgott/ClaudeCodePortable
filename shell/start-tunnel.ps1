<#
  start-tunnel.ps1 — launch the wireproxy AmneziaWG userspace proxy fully hidden.

  Why a helper instead of `start /MIN`:
    `start "" /MIN wireproxy.exe` still creates a console window that lives in the
    taskbar. We want zero visible window. ProcessStartInfo with CreateNoWindow=$true
    spawns the proxy with no console at all, and because the process is NOT placed in
    a kill-on-close job object it keeps running after this pwsh (and the launcher)
    exit — exactly the "survives" behaviour Start.bat relied on.

  ArgumentList is used so paths containing spaces are quoted correctly by .NET.
#>
[CmdletBinding()]
param(
    [Parameter(Mandatory)][string]$Exe,
    [Parameter(Mandatory)][string]$Config
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
# NOTE: ProcessStartInfo.WindowStyle is ignored when UseShellExecute=$false;
# CreateNoWindow is what actually suppresses the console, so we don't set it.

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
exit 0
