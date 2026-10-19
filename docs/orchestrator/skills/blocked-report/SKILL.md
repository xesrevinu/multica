---
name: blocked-report
description: Stop cleanly when an issue cannot be finished in this run (missing access, unclear requirement, broken environment, repeated failure) and hand it to the lead with a precise reason. Use instead of guessing or looping.
license: MIT
compatibility: multica
metadata:
  source: multica orchestrator
---

# Blocked report

1. Classify: missing input (credentials, decision, spec), environment broken, product/platform limitation, or a defect outside this issue.
2. Push any useful partial work to `agent/KEY` (never `main`).
3. `multica issue metadata set KEY --key blocked_reason --value "<one line: what is missing, who can give it, what you tried>"`
4. `multica issue metadata set KEY --key phase --value blocked` (last).
5. One result comment with the details (commands, errors, partial sha). Stop.

The orchestrator moves the issue to the lead and notifies the human. Do not change status or assignee yourself. Lead: resolve, then `phase=unblocked` (or `phase=cancel_proposed`).

- **Workspace scope (hard rule)**: search, read and write only inside the run's working directory (git worktree). Use `rg <pattern> .` or `git grep`; never run recursive grep/find/rg over `$HOME`, `/`, `/Users` or parent directories. Issue context comes from `multica issue get`, comments, metadata (`blocked_reason`, `acceptance`) and `multica flow show`; if it is missing, report blocked instead of searching the disk.
