# Division of responsibilities and completeness checklist

| Step | Lead | Owner | Reviewer | Orchestrator | Human |
|---|---|---|---|---|---|
| Split epic, stages, deps, acceptance labels | R | | | | A |
| Assign owner, start run, attempt counters | | | | R | |
| Implement + tests + checks green on head | | R | | | |
| Push branch, open PR (GitHub) | | R | | ensures PR | |
| Independent review on exact sha | | | R | | |
| Rework routing (findings, conflict, red CI, reject) | | | | R | |
| Prechecks: conflict, CI, required checks | | | | R | |
| Human acceptance | | | | waits | R |
| Merge (squash, match-head-commit), delete branch | | | | R | |
| Blocked diagnosis | R | | | notifies | A |
| Cancel | proposes | | | | R |

R = does it, A = decides when it matters.

## Before a team goes live
- [ ] Roles in the spec exist as agents; reviewer is not an owner.
- [ ] Each agent has team.md + its role file as instructions (rendered by
      `multica team preset render`).
- [ ] Skills attached: owner = open-pr, handoff-metadata, rebase-conflict,
      blocked-report; reviewer = verify-gates, review-verdict, handoff-metadata,
      blocked-report;
      lead = plan-split, blocked-report.
- [ ] Project resource: local_directory worktree on the hub checkout; hub
      set in the spec; `.config/wt.toml` hooks install deps without writing
      tracked or untracked files into the tree (no lockfile churn).
- [ ] check_cmd runs green on main in a fresh worktree.
- [ ] Limits sized: impl/review/infra, stuck_min, idle_min, idle_min_by_backend (preset gbot: -1), queued_min, unblocks, min_free_gb (unset = default, 0/negative = off).
- [ ] Human set; inbox notifications reach them.
- [ ] Dry run: one trivial issue end to end, `multica flow log` reviewed.

## Per issue (lead writes, reviewer checks)
- [ ] Outcome and acceptance criteria are observable.
- [ ] Out-of-scope stated.
- [ ] Check command named.
- [ ] Files or area named when parallel issues exist (disjoint writers).
- [ ] Acceptance label when a human must look.
