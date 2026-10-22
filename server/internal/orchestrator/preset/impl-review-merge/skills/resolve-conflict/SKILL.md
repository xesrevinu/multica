---
name: resolve-conflict
description: Merge main into agent/<key> when the engine sets phase=resolving_conflict (handoff kind resolve_conflict). Reviewer runs only; the one time a reviewer writes to the branch.
license: MIT
compatibility: multica
metadata:
  source: multica orchestrator (T11 conflict resolution)
---

# Resolve a conflict with main

You resolve merge conflicts only: no refactors, no unrelated fixes, no
behaviour beyond what both sides already do. When in doubt, stop and hand back.

1. Your run directory is the issue worktree on `agent/<key>`. `git fetch origin`.
   Stop (step 6) unless `git status --porcelain` is empty and HEAD equals
   `origin/agent/<key>`.
2. `git merge --no-ff origin/{{MAIN}}`. Never rebase, never force-push.
3. Resolve each conflict keeping both sides' intent; take main's version where
   the PR did not mean to change the file; regenerate lockfiles and generated
   files instead of hand-merging. Check callers of anything main moved or
   deleted. Commit the merge listing the resolved files.
4. Run the repo gates on the merged head (skill `verify-gates` steps for the
   commands). Fix only breakage the merge caused.
5. `git push origin HEAD:refs/heads/agent/<key>` (fast-forward only). Then
   `handoff-metadata`: `conflict_result=resolved`, `conflict_notes` = one line
   per resolved file. Done: the engine re-checks and carries the approval when
   the PR's own change is unchanged, otherwise orders a review.
6. Cannot resolve (a product decision, the PR's intent unclear, gates broken by
   the merge you cannot fix mechanically): `git merge --abort`, push nothing,
   set `conflict_result=cannot_resolve` and `conflict_notes` = conflicting
   files, what each side wants, what you tried. The owner gets it back.
