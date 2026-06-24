# SessionStart memory push. Injects curated feedback memory bodies + a catalog of
# situational (project/reference) memories, so behavioral rules load WITHOUT relying
# on the model deciding to read them (kills "amnesia despite memory" pull-gap).
# MEMORY.md index already auto-loads; this adds the bodies that don't.
$ErrorActionPreference = 'SilentlyContinue'

# Force UTF-8 stdout: catalog descriptions (Cyrillic) + feedback bodies (arrows/dashes)
# are non-ASCII; default console encoding corrupts them and breaks the harness JSON parse.
[Console]::OutputEncoding = [System.Text.Encoding]::UTF8

# UTF-8 stdin (Cyrillic-safe), same pattern as detect-stuck.ps1
try {
  $sr = New-Object System.IO.StreamReader([Console]::OpenStandardInput(), [System.Text.Encoding]::UTF8)
  $raw = $sr.ReadToEnd(); $sr.Dispose()
} catch { $raw = [Console]::In.ReadToEnd() }
try { $in = $raw | ConvertFrom-Json } catch { $in = $null }

# Fire on real new contexts only; skip 'compact' (context already carried over).
$source = [string]$in.source
if ($source -eq 'compact') { exit 0 }

$ccbase = if ($env:CLAUDE_CONFIG_DIR) { $env:CLAUDE_CONFIG_DIR } else { Join-Path $env:USERPROFILE '.claude' }
$mem = Join-Path $ccbase 'memory'
if (-not (Test-Path $mem)) { exit 0 }

$sb = New-Object System.Text.StringBuilder
[void]$sb.AppendLine("CURATED MEMORY (pushed at session start - these are behavioral rules you ALREADY agreed to; apply them, do not re-derive):")
[void]$sb.AppendLine()

# Behavioral feedback -> full bodies (the 'why'/'how' not in CLAUDE.md)
Get-ChildItem -Path $mem -Filter 'feedback_*.md' | Sort-Object Name | ForEach-Object {
  [void]$sb.AppendLine("--- $($_.Name) ---")
  [void]$sb.AppendLine((Get-Content $_.FullName -Raw -Encoding UTF8))
  [void]$sb.AppendLine()
}

# Situational memory -> catalog only (read on demand when context matches)
[void]$sb.AppendLine("SITUATIONAL MEMORY (read the file when its topic comes up):")
Get-ChildItem -Path $mem -Filter '*.md' |
  Where-Object { $_.Name -notlike 'feedback_*' -and $_.Name -ne 'MEMORY.md' } |
  Sort-Object Name | ForEach-Object {
    $desc = (Select-String -Path $_.FullName -Pattern '^description:\s*(.+)$' | Select-Object -First 1).Matches.Groups[1].Value
    [void]$sb.AppendLine("- $($_.Name): $desc")
  }

$ctx = $sb.ToString()
([pscustomobject]@{ hookSpecificOutput = [pscustomobject]@{ hookEventName = 'SessionStart'; additionalContext = $ctx } } | ConvertTo-Json -Compress -Depth 6) | Write-Output
exit 0
