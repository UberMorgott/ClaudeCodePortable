---
name: Autonomy rules
description: Never ask user to run commands that Claude can run itself — work end-to-end
type: feedback
---

Do not ask user to execute commands that you can execute yourself.
If you can do it (run command, create file, check setting) — do it immediately.
Never say "Run command X" — run it yourself.
Work end-to-end, don't offload simple actions to the user.

**Why:** User gave this feedback repeatedly — wasted time on trivial delegations.
**How to apply:** Every time you're about to suggest a command — ask yourself if you can just run it.
