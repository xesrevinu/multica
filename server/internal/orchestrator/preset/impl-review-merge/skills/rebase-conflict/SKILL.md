---
name: rebase-conflict
description: Resolve a merge conflict between agent/<key> and main when the orchestrator sends the issue back with blocked_reason "conflict" or "precheck conflict". Owner runs only.
license: MIT
compatibility: multica
metadata:
  source: multica orchestrator (pstack babysit / shipping)
---

# Rebase conflict

1. `git fetch origin && git rebase origin/main` on your own branch (`agent/<key>`).
2. Resolve each conflict keeping both sides' intent; check callers of anything main moved or deleted (drift sweep).
3. Run the repo checks on the rebased head. Fix until green.
4. Publish your own branch only: `git push --force-with-lease=agent/<key>:<sha you last pushed> origin HEAD:refs/heads/agent/<key>`.
5. `handoff-metadata` with the new `head_sha`, then `phase=review_requested`. The old verdict is void; a new review follows.
6. If the conflict needs a decision you cannot make, use `blocked-report`.
