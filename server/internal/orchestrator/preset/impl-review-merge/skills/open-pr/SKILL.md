---
name: open-pr
description: Commit, push branch agent/<key> and open (or update) exactly one ready GitHub pull request with a reviewer-ready description. Use for owner runs when the project repo is on GitHub.
license: MIT
compatibility: multica
metadata:
  source: multica orchestrator (pstack opening-a-pr)
---

# Open PR

## Steps

1. Stay on the branch the run started on. Only you write `agent/<key>`.
2. Commits: small and ordered, each landable. Subject `type(scope): subject` (feat, fix, docs, refactor, test, chore, perf), imperative, no period. The body says why, not what the diff shows. Amend a fix into the commit it belongs to.
3. Before pushing, run the repo checks on the committed head (`npm test && npm run lint` here). Do not push red.
4. Push: `git push origin HEAD:refs/heads/agent/<key>`. After a rebase of your own branch: `git push --force-with-lease=agent/<key>:<sha you last pushed> origin HEAD:refs/heads/agent/<key>`. Never push `main`, never force anyone else's branch.
5. One PR per issue, opened ready (no `--draft`): `gh pr list --head agent/<key> --state open --json number,url` first; if none:
   `gh pr create --head agent/<key> --base main --title "type(scope): subject (KEY)" --body-file /tmp/pr-KEY.md`
6. Body = a one-minute briefing for a reviewer who has the diff. `##` sections in this order, drop empty ones:
   - `## Why` — problem and approach, 1–3 sentences.
   - `## What changed` — 1–3 bullets; name a symbol or path only when it carries the change.
   - `## Scope` — what it covers and deliberately leaves out.
   - `## Blast Radius` — who or what it touches and why that is safe.
   - `## Verification` — the commands you ran and their result.
7. Hand off with `handoff-metadata`. Put the PR URL in your result comment.

## Never

- `gh pr merge`, approving or closing PRs, deleting branches, "Closes KEY" in the body. The orchestrator merges with `--match-head-commit` after an independent review and green CI, and babysits CI (one re-run of a red head).
