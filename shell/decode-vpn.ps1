# Decodes an Amnezia "vpn://..." share file into a WireGuard-format .conf
# that wireproxy-awg can read. Run by Start.bat at every launch, so you can
# swap servers just by replacing the .vpn file.
#
#   pwsh decode-vpn.ps1 -In <path\to\amnezia_config.vpn> -Out <path\to\awg.conf>

param(
    [Parameter(Mandatory=$true)][string]$In,
    [Parameter(Mandatory=$true)][string]$Out
)
$ErrorActionPreference = 'Stop'

$s = (Get-Content -Raw $In).Trim() -replace '^vpn://',''
# base64url -> standard base64
$s = $s.Replace('-','+').Replace('_','/')
switch ($s.Length % 4) { 2 { $s += '==' } 3 { $s += '=' } 1 { $s += '===' } }
$bytes = [Convert]::FromBase64String($s)

# Qt qCompress(): first 4 bytes = big-endian uncompressed size, rest = zlib stream
$payload = $bytes[4..($bytes.Length - 1)]
$ms  = New-Object System.IO.MemoryStream(, $payload)
$z   = New-Object System.IO.Compression.ZLibStream($ms, [System.IO.Compression.CompressionMode]::Decompress)
$dec = New-Object System.IO.MemoryStream
$z.CopyTo($dec); $z.Dispose()
$obj = ([System.Text.Encoding]::UTF8.GetString($dec.ToArray())) | ConvertFrom-Json

# find the container that carries an AmneziaWG config
$container = $obj.containers | Where-Object { $_.awg } | Select-Object -First 1
if (-not $container) { throw "No AmneziaWG (awg) container in this .vpn file" }

$cfgText = ($container.awg.last_config | ConvertFrom-Json).config
if (-not $cfgText) { throw "No .config text inside last_config" }

# Amnezia leaves DNS placeholders that its app substitutes at connect time.
# wireproxy treats a leading '$' as an env-var ref, so replace them with the
# real DNS from the share file (fallback to Cloudflare).
$dns1 = if ($obj.dns1) { $obj.dns1 } else { '1.1.1.1' }
$dns2 = if ($obj.dns2) { $obj.dns2 } else { '1.0.0.1' }
$cfgText = $cfgText.Replace('$PRIMARY_DNS', $dns1).Replace('$SECONDARY_DNS', $dns2)

# write UTF-8 without BOM
[System.IO.File]::WriteAllText($Out, $cfgText, (New-Object System.Text.UTF8Encoding($false)))
Write-Host "decoded -> $Out"
