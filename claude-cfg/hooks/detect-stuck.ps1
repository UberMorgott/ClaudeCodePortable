# Stuck-loop detector. Two failure channels:
#  A) command-failure loop (exit!=0): PostToolUseFailure counts; PreToolUse delivers nudge; PostToolUse resets.
#  B) behavioral failure ("compiles but wrong"): UserPromptSubmit scans the user's words for
#     "still doesn't work"-type reports and injects the nudge — because such failures have exit 0
#     and are judged by the USER, never by a command exit code.
# Matcher Bash|PowerShell for the command events; UserPromptSubmit has no matcher.
$ErrorActionPreference = 'SilentlyContinue'

# Read stdin as UTF-8 explicitly (default console encoding mangles Cyrillic).
try {
  $sr = New-Object System.IO.StreamReader([Console]::OpenStandardInput(), [System.Text.Encoding]::UTF8)
  $raw = $sr.ReadToEnd(); $sr.Dispose()
} catch { $raw = [Console]::In.ReadToEnd() }
if (-not $raw) { exit 0 }
try { $in = $raw | ConvertFrom-Json } catch { exit 0 }

$sid = $in.session_id
if (-not $sid) { $sid = 'default' }
$evt = [string]$in.hook_event_name
$cmd = [string]$in.tool_input.command

$ccbase = if ($env:CLAUDE_CONFIG_DIR) { $env:CLAUDE_CONFIG_DIR } else { Join-Path $env:USERPROFILE '.claude' }
$dir = Join-Path $ccbase 'hook-state'
New-Item -ItemType Directory -Force -Path $dir | Out-Null
$stateFile = Join-Path $dir ("$sid.json")

$count = 0; $last = ''
if (Test-Path $stateFile) {
  try { $st = Get-Content $stateFile -Raw | ConvertFrom-Json; $count = [int]$st.count; $last = [string]$st.last_cmd } catch {}
}

$nudge = "STOP blind retrying. Per global rules (When coding / Anti-guessing): (1) Context7 the relevant library/API (resolve-library-id -> docs) instead of guessing; (2) invoke the systematic-debugging skill to find root cause; (3) re-check project docs and flag to user if stale. Do NOT attempt another change without doing this first."

switch ($evt) {
  'UserPromptSubmit' {
    $txt = [string]$in.prompt
    if (-not $txt) { $txt = $raw }  # fallback if field name differs
    # Strong signals: unambiguous failure reports (carry negation / explicit failure).
    $patStrong = 'не\s*работает|не\s*пашет|не\s*фурычит|всё?\s*ещё?\s*не|все\s*еще\s*не|по-?прежнему\s*не|не\s*получ|не\s*вышло|не\s*помог|не\s*запуск|опять\s*не|снова\s*не|doesn.?t\s*work|does\s*not\s*work|not\s*working|still\s*(not|broke|fail|doesn|the\s*same)|didn.?t\s*help|no\s*luck'
    # Ambiguous "same X" family: fire ONLY when a recurrence/failure context word is also present.
    # Kills false positives like "use the same error handling" / "сделай то же самое".
    $patSame = 'same\s*(error|issue|problem)|та\s*же\s*(ошибк|проблем|фигн)|то\s*же\s*самое'
    $patCtx  = 'still|again|опять|снова|по-?прежнему|всё?\s*ещё?|не\s*работа|не\s*помог|не\s*получ|не\s*вышло|broke|broken|doesn.?t|not\s*work|fail|crash|no\s*luck|didn.?t'
    $hit = ($txt -imatch $patStrong) -or (($txt -imatch $patSame) -and ($txt -imatch $patCtx))
    if ($hit) {
      $msg = "User reports it STILL does not work (behavioral failure, not a command error). $nudge"
      ([pscustomobject]@{ hookSpecificOutput = [pscustomobject]@{ hookEventName = 'UserPromptSubmit'; additionalContext = $msg } } | ConvertTo-Json -Compress -Depth 6) | Write-Output
    }
    exit 0
  }
  'PreToolUse' {
    if ($cmd -and $cmd -eq $last -and $count -ge 3) {
      $msg = "STUCK signal: '$cmd' just failed $count times in a row. $nudge"
      if ($count -ge 5) {
        ([pscustomobject]@{ hookSpecificOutput = [pscustomobject]@{ hookEventName = 'PreToolUse'; permissionDecision = 'deny'; permissionDecisionReason = $msg } } | ConvertTo-Json -Compress -Depth 6) | Write-Output
      } else {
        ([pscustomobject]@{ hookSpecificOutput = [pscustomobject]@{ hookEventName = 'PreToolUse'; additionalContext = $msg } } | ConvertTo-Json -Compress -Depth 6) | Write-Output
      }
    }
    exit 0
  }
  'PostToolUseFailure' {
    if (-not $cmd) { exit 0 }
    if ($cmd -eq $last) { $count++ } else { $count = 1; $last = $cmd }
    ([pscustomobject]@{ count = $count; last_cmd = $last } | ConvertTo-Json -Compress) | Set-Content -Path $stateFile -Encoding UTF8
    exit 0
  }
  default {
    # PostToolUse (success) -> reset command counter. Gate on the actual success event so an
    # unexpected event type can't silently clear a live failure streak.
    if ($evt -eq 'PostToolUse') {
      ([pscustomobject]@{ count = 0; last_cmd = '' } | ConvertTo-Json -Compress) | Set-Content -Path $stateFile -Encoding UTF8
    }
    exit 0
  }
}
