# Orchestrated projects

A project with a team (`multica team status`) is run by a state machine in the
server, the only writer of status, assignee and engine metadata. Agents work,
write hand-over metadata, and leave one result comment.

**Routing.** The lead plans epics into staged `backlog` children with area
labels. The first matching `routes` entry picks the owner pool; `experts`
(label-matched, e.g. `area:research`, `area:hard`) are checked first. A
`consult` expert answers and hands back (`expert_done` records it); an
`implement` expert owns the issue. `needs:<slot>` labels (e.g. `needs:cargo`)
queue the issue while that slot is full. No @mentions start work.

## What you may write

`multica issue metadata set <KEY> --key <k> --value <v>`. Quote shas as JSON
strings (`--value "\"$SHA\""`) and write the signal key (`phase`/`verdict`) last.

| You are | Write | Effect |
|---|---|---|
| owner | `multica issue status <KEY> in_progress` (from todo only) | shows you started |
| owner | push `agent/<KEY>`, then `head_sha=<sha>`, then `phase=review_requested` | pre-check (merge + CI), PR found/opened, reviewer assigned |
| reviewer | `reviewed_sha=<sha you ran>`, then `verdict=approve` or `verdict=changes` | approve@head + green CI → squash merge (or human acceptance); changes → owner |
| anyone | `blocked_reason=<what is missing, who can give it>`, then `phase=blocked` | lead gets it, human notified |
| lead | `phase=unblocked` or `phase=cancel_proposed` on a blocked issue | back to owner, or cancelled |

Engine-owned (refused with `orchestrator_owned`): `owner`, `expert_done`, `attempt_*`, `pr`, `pr_number`,
`merged_sha`, `acceptance`, `acceptance_required`, and the other bookkeeping keys.

## Gotchas

- Only the owner pushes `agent/<KEY>`. Rebase with `--force-with-lease` only.
  Never create an `agent/<KEY>` branch by hand for another issue.
- A verdict counts only for `reviewed_sha == head`; a new push needs a new review.
- The reviewer re-runs the gates on that sha and exercises the change. Green CI
  is not a verdict; the verdict goes in metadata, not status.
- End every run with a hand-over or a block. A run that ends with neither is
  re-run, and after the attempt limit the issue blocks.
- Never merge, close or move the issue; the orchestrator merges with
  `--match-head-commit` and deletes the branch. A PR merged by hand is noticed
  only when the issue reaches its merge step.
- CI red is re-run once; a second red returns to the owner with the checks in
  `blocked_reason`.
- `multica flow show <KEY>` explains the last decision. `multica flow …` and
  `multica team …` are for humans, not agents.
