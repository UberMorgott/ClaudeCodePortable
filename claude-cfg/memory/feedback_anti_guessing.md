---
name: feedback_anti_guessing
description: "When stuck/unsure about code or an API — Context7 first, then docs; never blind-iterate"
metadata: 
  node_type: memory
  type: feedback
---

- **Why:** guessing API signatures/fields from memory + retry loops waste hours; a docs lookup resolves in minutes. Especially recent libraries where memory lags behind the current release.
- **How to apply:** the instant you'd "guess" an API signature/field/behavior → STOP. Ladder: (1) Context7 exact lib/version → docs; (2) project docs / official docs / web; (3) still stuck → tell user. 2 failed attempts on the same thing = hard trigger, no blind retry. Stale project docs → propose a SEPARATE agent to fix them, don't silently work around.
- **Enforced by:** global CLAUDE.md (`## When coding` → Anti-guessing + Context7 + Stuck) AND mechanical hook `~/.claude/hooks/detect-stuck.ps1` with TWO channels: (A) command-failure loop — PostToolUseFailure counts, PreToolUse delivers nudge at 3 identical failures / deny at 5; (B) behavioral failure — UserPromptSubmit scans user msg for "не работает/still doesn't work" → nudge (catches compiles-but-wrong failures judged by the user, not by an exit code).
- Related: [[feedback_strict_delegation]]
