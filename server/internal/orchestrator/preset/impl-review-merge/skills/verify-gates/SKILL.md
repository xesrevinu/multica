---
name: verify-gates
description: Re-run the repository gates yourself on the exact sha under review, in a wt-prepared checkout, before giving a verdict. Use for reviewer runs.
license: MIT
compatibility: multica
metadata:
  source: multica orchestrator (pstack independent verifier)
---

# Verify gates

1. `git fetch origin` and pick the sha: `SHA=$(git rev-parse origin/agent/<key>)`.
2. Inspect first, in your own run directory (it is a wt worktree with
   dependencies, vendor links and the direnv toolchain already prepared):
   `git diff origin/main...$SHA`, `git show $SHA:<path>`, `git grep <pat> $SHA`.
   Never `git checkout`/`switch`/`reset` there: its HEAD must stay put.
3. To run gates, make the one extra checkout with worktrunk so the repo's
   setup hooks run (deps, vendor/hermes links, `direnv allow`):
   `wt switch --yes --no-cd --create rv/<key> --base $SHA`, then work in the
   path `wt` prints (`git worktree list | grep rv/<key>`). Never plain
   `git worktree add`, never copy node_modules/vendor by hand: if setup is
   missing, that is an infra finding, not something to rebuild.
4. Run only the checks the repo's checks skill lists for the paths changed
   (e.g. `holt-checks`), with the toolchain from the checkout's `.envrc`
   (`direnv exec . <cmd>` if it is not already in your env). Capture the exact
   command and the tail of its output.
5. Exercise the changed behavior once through its real surface when it has one
   (CLI call, small script, request). Compilation is not behavior.
6. `wt remove --yes --force -D rv/<key>`. Leave nothing behind.
7. Put commands and results in your verdict comment. A gate you could not run
   is a finding ("not verified: <why>"), not a pass.
