---
name: rebase-conflict
description: Bring main into agent/<key> by merging (never rebasing) when the orchestrator sends the issue back with blocked_reason "conflict" or "precheck conflict". Owner runs only.
license: MIT
compatibility: multica
metadata:
  source: multica orchestrator (pstack babysit / shipping)
---

# Main moved: merge it in

Merge, never rebase. The daemon refuses a run whose result no longer contains the commit it started from, and the PR is squashed on merge anyway, so a merge commit costs nothing.

1. `git fetch origin && git merge origin/main` on the branch you are on (`agent/<key>`).
2. Resolve each conflict keeping both sides' intent; check callers of anything main moved or deleted (drift sweep). Commit the merge.
3. Run the repo checks on the merged head. Fix until green.
4. Plain push, no force: `git push origin HEAD:refs/heads/agent/<key>`.
5. `handoff-metadata` with the new `head_sha`, then `phase=review_requested`. If only main's side changed, the engine keeps the approval (same patch); otherwise a new review follows.
6. If the conflict needs a decision you cannot make, use `blocked-report`.
