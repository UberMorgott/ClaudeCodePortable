# Decodes an Amnezia "vpn://..." share file into a WireGuard-format .conf
# that wireproxy-awg can read. Run by Start.bat at every launch, so you can
# swap servers just by replacing the .vpn file.
#
#   pwsh decode-vpn.ps1 -In <path\to\amnezia_config.vpn> -Out <path\to\awg.conf> `
#                       [-ProxyOut <path\to\proxy.generated.conf>] [-BindAddress 127.0.0.1:25345]
#
# -ProxyOut: also emit the wireproxy config here, UTF-8 WITHOUT BOM. This MUST be
# generated from pwsh (not a cmd `echo`): cmd writes the redirected file in the
# console OEM codepage, so a Cyrillic/non-ASCII char in the path (e.g. ...\тест\)
# is stored as OEM bytes. wireproxy reads its config as UTF-8, decodes those bytes
# as mojibake, and fails with "cannot find path ...????...". Writing UTF-8 here
# keeps the path byte-correct. cmd `echo` also mangles `&`/`^`/`%` in the path.

param(
    [Parameter(Mandatory=$true)][string]$In,
    [Parameter(Mandatory=$true)][string]$Out,
    [string]$ProxyOut,
    [string]$BindAddress = '127.0.0.1:25345'
)
$ErrorActionPreference = 'Stop'

$s = (Get-Content -Raw $In).Trim() -replace '^vpn://',''
# base64url -> standard base64
$s = $s.Replace('-','+').Replace('_','/')
switch ($s.Length % 4) { 2 { $s += '==' } 3 { $s += '=' } 1 { throw "corrupt base64 in .vpn (length % 4 == 1)" } }
$bytes = [Convert]::FromBase64String($s)

# Qt qCompress(): first 4 bytes = big-endian uncompressed size, rest = zlib stream
if ($bytes.Length -le 4) { throw "vpn payload too short / corrupt .vpn file" }
$payload = [byte[]]($bytes[4..($bytes.Length - 1)])
$ms  = [System.IO.MemoryStream]::new($payload)
$z   = [System.IO.Compression.ZLibStream]::new($ms, [System.IO.Compression.CompressionMode]::Decompress)
$dec = [System.IO.MemoryStream]::new()
try {
    $z.CopyTo($dec)
    $obj = ([System.Text.Encoding]::UTF8.GetString($dec.ToArray())) | ConvertFrom-Json
} finally {
    $z.Dispose(); $ms.Dispose(); $dec.Dispose()
}

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
$utf8 = [System.Text.UTF8Encoding]::new($false)
[System.IO.File]::WriteAllText($Out, $cfgText, $utf8)
Write-Host "decoded -> $Out"

# Optionally emit the wireproxy config too (UTF-8 no BOM, byte-correct path).
# WGConfig points at the awg .conf with forward slashes (wireproxy convention).
if ($ProxyOut) {
    $wgPath  = ([System.IO.Path]::GetFullPath($Out)).Replace('\', '/')
    $proxyTxt = "WGConfig = $wgPath`n`n[http]`nBindAddress = $BindAddress`n"
    [System.IO.File]::WriteAllText($ProxyOut, $proxyTxt, $utf8)
    Write-Host "proxy cfg -> $ProxyOut"
}
