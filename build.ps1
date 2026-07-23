# Local Windows equivalent of `make release` (make isn't on the dev box).
# Strip + UPX-compress ccp.exe into ./dist and emit a sha256sum-format checksums.txt.
param([string]$Version = 'dev')
$ErrorActionPreference = 'Stop'

$ldflags = "-s -w -X github.com/UberMorgott/ClaudeCodePortable/internal/buildinfo.Version=$Version"
New-Item -ItemType Directory -Force dist | Out-Null

$env:GOOS = 'windows'; $env:GOARCH = 'amd64'
Write-Host "building dist/ccp.exe (version $Version)"
go build -trimpath -ldflags $ldflags -o dist/ccp.exe ./cmd/ccp
Remove-Item Env:GOOS, Env:GOARCH

# Best-effort locally: unlike CI's `make release`, ship uncompressed if upx is absent.
if (Get-Command upx -ErrorAction SilentlyContinue) {
    upx --best --lzma dist/ccp.exe
} else {
    Write-Warning 'upx not on PATH — shipping uncompressed (install upx to shrink the artifact)'
}

# sha256sum format go-selfupdate's ChecksumValidator parses: lowercase hex, two spaces, filename.
$hash = (Get-FileHash -Algorithm SHA256 -LiteralPath 'dist/ccp.exe').Hash.ToLower()
Set-Content -Path 'dist/checksums.txt' -Value "$hash  ccp.exe" -Encoding ascii
Write-Host "done -> ./dist"
