---
name: Always use Opus, never downgrade to Sonnet/Haiku
description: User insists on Opus for all work; refuses Sonnet/Haiku even for "trivial" tasks
type: feedback
---
Always default to Opus (currently Opus 4.7). Do NOT propose Sonnet 4.6 or Haiku 4.5 as cost-saving substitutes — even for "simple" or "mechanical" tasks like rename, format, grep, simple read.

**Why:** User has tested and found that Sonnet and Haiku make mistakes even in simplest tasks, which forces re-do work and wastes more tokens overall than Opus would have used. Net cost of "cheaper" models is higher because of rework.

**How to apply:**
- Never suggest "switch to Sonnet/Haiku for X to save tokens" as a cost-optimization recommendation
- Subagents dispatched for the user's work should also default to Opus unless task is genuinely pure-mechanical (e.g. Write tool with exact pre-provided content)
- Token-saving recommendations must focus on OTHER vectors: MCP pruning, version rollback, hooks, .claudeignore, /clear discipline, MCP server hygiene — never model downgrade
- If user explicitly asks "what if I switched to Sonnet" — answer factually but flag the rework risk
