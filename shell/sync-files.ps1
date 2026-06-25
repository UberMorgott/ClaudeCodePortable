<#
  sync-files.ps1 — copy repo files (scripts + claude-cfg) from a freshly
  downloaded source tree onto the stick, but ONLY the files that actually
  differ (missing in dest, or SHA256 mismatch). Replaces the old blanket
  `xcopy /y` / `copy /y` in bootstrap.cmd that clobbered everything every run.

  Non-destructive: never deletes files in $Dest that aren't in $Src, so any
  user-local extras under shell\ / claude-cfg\ are preserved.

  $Items are relative paths under $Src; each may be a directory (synced
  recursively) or a single file. -LiteralPath is used throughout so paths
  with spaces work.

  NOTE on $Items binding: pwsh's `-File` mode does NOT collect multiple
  space-separated values after a named `-Items` switch into the array — only
  the first would bind and the rest would error. $Items is therefore declared
  ValueFromRemainingArguments and the caller (bootstrap.cmd) passes the item
  names as bare trailing tokens (no `-Items` switch), e.g.:
      pwsh -File sync-files.ps1 -Src ... -Dest ... shell claude-cfg Start.bat
#>
[CmdletBinding()]
param(
    [Parameter(Mandatory)][string]$Src,
    [Parameter(Mandatory)][string]$Dest,
    [Parameter(Mandatory, ValueFromRemainingArguments)][string[]]$Items
)

$ErrorActionPreference = 'Stop'

# Normalize the source root to an absolute path so we can compute reliable
# relative paths for each enumerated file (preserving subdir structure).
$srcRoot = (Resolve-Path -LiteralPath $Src).Path

$updated = 0
$unchanged = 0

foreach ($item in $Items) {
    $itemPath = Join-Path -Path $srcRoot -ChildPath $item

    if (-not (Test-Path -LiteralPath $itemPath)) {
        Write-Warning "[sync] source item not found, skipping: $item"
        continue
    }

    # Collect the concrete source files for this item.
    if (Test-Path -LiteralPath $itemPath -PathType Container) {
        $files = Get-ChildItem -LiteralPath $itemPath -Recurse -File
    } else {
        $files = @(Get-Item -LiteralPath $itemPath)
    }

    foreach ($file in $files) {
        $full = $file.FullName

        # Relative path of this file under the source root, preserving subdirs.
        # GetRelativePath normalizes 8.3 short vs long path forms: %TEMP% often
        # resolves to ...\SUPPOR~1 while Get-ChildItem yields ...\SUPPORT-5, so a
        # plain prefix Substring sees "no common root" and silently flattens the
        # whole tree into $Dest (shell\update.ps1 -> update.ps1). That broke the
        # installer whenever it ran from a short-named path (any %TEMP% download).
        $rel = [System.IO.Path]::GetRelativePath($srcRoot, $full)
        if ([System.IO.Path]::IsPathRooted($rel) -or $rel.StartsWith('..')) {
            # No common root -> refuse to flatten silently; fail loud instead.
            Write-Error "[sync] cannot compute relative path for '$full' under '$srcRoot'"
            exit 1
        }

        $destPath = Join-Path -Path $Dest -ChildPath $rel

        $needCopy = $false
        if (-not (Test-Path -LiteralPath $destPath -PathType Leaf)) {
            $needCopy = $true
        } else {
            $srcHash = (Get-FileHash -LiteralPath $full -Algorithm SHA256).Hash
            $dstHash = (Get-FileHash -LiteralPath $destPath -Algorithm SHA256).Hash
            if ($srcHash -ne $dstHash) { $needCopy = $true }
        }

        if ($needCopy) {
            try {
                $parent = [System.IO.Path]::GetDirectoryName($destPath)
                if ($parent -and -not (Test-Path -LiteralPath $parent)) {
                    New-Item -ItemType Directory -Path $parent -Force | Out-Null
                }
                Copy-Item -LiteralPath $full -Destination $destPath -Force
            } catch {
                Write-Error "[sync] failed to copy '$rel': $($_.Exception.Message)"
                exit 1
            }
            $updated++
            Write-Host "[sync] + $rel"
        } else {
            $unchanged++
        }
    }
}

Write-Host "[sync] $updated updated, $unchanged unchanged"
exit 0
