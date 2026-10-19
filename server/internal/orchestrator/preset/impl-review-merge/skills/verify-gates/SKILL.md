---
name: verify-gates
description: Re-run the repository gates yourself on the exact sha under review, in a scratch worktree, before giving a verdict. Use for reviewer runs.
license: MIT
compatibility: multica
metadata:
  source: multica orchestrator (pstack independent verifier)
---

# Verify gates

1. `git fetch origin` and pick the sha: `git rev-parse origin/agent/<key>`.
2. Scratch checkout inside your working directory, never in the owner's:
   `git worktree add --detach .rv-KEY <sha>`.
3. Install only what the checks need, then run the project's check command
   (the one in the team contract) in `.rv-KEY`. Capture the exact command and
   the tail of its output.
4. Exercise the changed behavior once through its real surface when it has one
   (CLI call, small script, request). Compilation is not behavior.
5. `git worktree remove --force .rv-KEY`. Leave nothing behind.
6. Put commands and results in your verdict comment. A gate you could not run
   is a finding ("not verified: <why>"), not a pass.
