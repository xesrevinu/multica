---
name: verify-gates
description: Re-run the repository gates yourself on the exact sha under review, in the issue's own worktree, before giving a verdict. Use for reviewer runs.
license: MIT
compatibility: multica
metadata:
  source: multica orchestrator (pstack independent verifier)
---

# Verify gates1. `git fetch origin` and pick the sha: `SHA=$(git rev-parse origin/agent/<key>)`.
2. Your run directory IS the issue's worktree on `agent/<key>` (deps,
   toolchain, build cache prepared). Pin it first:
   `test "$(git rev-parse HEAD)" = "$SHA"`. If it differs, do not checkout,
   reset, pull or rebuild a tree: report "not verified: worktree HEAD
   <head> != $SHA" as an infra finding and stop.
3. Run every check right there. No `wt switch`, no `rv/<key>`, no `git worktree
   add`, `git archive`, copied/symlinked trees. Never commit, checkout, switch,
   reset, stash or edit tracked files in it.
.
4. Run only the checks the repo's checks skill lists for the paths changed
   (e.g. `holt-checks`), with the toolchain from the checkout's `.envrc`
   (`direnv exec . <cmd>` if it is not already in your env). Capture the exact
   command and the tail of its output.
5. Exercise the changed behavior once through its real surface when it has one
   (CLI call, small script, request). Compilation is not behavior.
6. Before finishing: `test "$(git rev-parse HEAD)" = "$SHA"` and
   `git status --porcelain` must be empty (ignored build output is fine).
   Remove only files you created; report any other change as a finding.
7. Put commands and results in your verdict comment. A gate you could not run
   is a finding ("not verified: <why>"), not a pass.
