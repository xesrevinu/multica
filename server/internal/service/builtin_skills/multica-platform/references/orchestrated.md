# Orchestrated projects

A project with an orchestrator team (`multica team status`) is run by a
deterministic state machine inside the server. It is the only writer of
status and assignee. You work, write metadata, and write one result comment.

## What you may write

| You are | Write | Effect |
|---|---|---|
| owner | `multica issue status <key> in_progress` (from todo only) | shows you started |
| owner | push `agent/<KEY>` (open the PR on GitHub projects), then `head_sha=<pushed sha>`, then `phase=review_requested` last | orchestrator pre-checks (merge + checks), finds or opens the PR, records `pr`, assigns the reviewer |
| reviewer | `verdict=approve` or `verdict=changes`, `reviewed_sha=<the sha you ran>` | approve@head + green CI → merge (or human acceptance); changes → back to owner |
| anyone | `phase=blocked`, `blocked_reason=<one line: what is missing, who can give it>` | issue goes to the lead, the human is notified |
| lead | `phase=unblocked` or `phase=cancel_proposed` on a blocked issue | back to the owner (counters reset) or cancelled |

Write metadata with `multica issue metadata set <KEY> --key <name> --value <value>`;
quote shas as JSON strings (`--value "\"$SHA\""`) and write the signal key
(`phase` / `verdict`) last. Everything else (`owner`, `attempt_*`, `pr`,
`pr_number`, `merged_sha`, `acceptance`) belongs to the orchestrator; writing it
is refused with `orchestrator_owned`.

## Rules that keep the machine deterministic

- **One writer per branch.** Only the owner pushes `agent/<KEY>`; stay on it.
  Rebase your own branch with `git push --force-with-lease` only.
- **One verdict per head.** A verdict counts only for `reviewed_sha == head`.
  A new push needs a new review; the orchestrator asks for it.
- **The reviewer never wrote the code.** Re-run the gates on the exact sha,
  exercise the change the way a user would, then judge the diff. Green CI is
  not a verdict.
- **End every run with a hand-over or a block.** A run that ends without one
  is re-run, and after the attempt limit the issue blocks.
- **Do not merge, close, or move the issue.** The orchestrator merges with
  `--match-head-commit` after the gates and deletes the branch.
- **CI red:** the orchestrator re-runs failed CI once; a second red comes back
  to the owner with the failing checks in `blocked_reason`.

## Reading why something happened

`multica flow show <key>` prints status, metadata, the next decision and the
recent transitions with their reasons. `blocked_reason` always explains the
latest rework or block. Humans steer with `multica flow unblock|cancel|accept|
reject|replay`; agents do not use `multica flow`.

- **Workspace scope (hard rule)**: search, read and write only inside the run's working directory (git worktree). Use `rg <pattern> .` or `git grep`; never run recursive grep/find/rg over `$HOME`, `/`, `/Users` or parent directories. Issue context comes from `multica issue get`, comments, metadata (`blocked_reason`, `acceptance`) and `multica flow show`; if it is missing, report blocked instead of searching the disk.
