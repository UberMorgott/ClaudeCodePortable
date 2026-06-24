---
name: Keep Superpowers skills enabled — net token savings on distance
description: Superpowers add per-session overhead but prevent rework; user values them, do not disable for "savings"
type: feedback
---
Do NOT propose disabling Superpowers / skill auto-loading as a token-saving measure. User has weighed it and considers them net-positive on distance.

**Why:** Skills produce overhead at session start, but enforce discipline (TDD, debugging method, brainstorming, verification-before-completion) that prevents wrong-direction work. Wrong-direction work costs FAR more tokens than the skill overhead, because it requires undo + redo cycles. User has empirically validated this trade-off.

**How to apply:**
- When listing token-saving options, do not include "disable Superpowers" / "skip skill loading"
- When invoking skills, follow them properly — don't half-apply to "save time", because that defeats the whole point
- This applies to Superpowers specifically; situational MCP servers are different and can be pruned when unused
