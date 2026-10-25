---
name: handoff-metadata
description: Hand an issue to the Multica orchestrator by writing issue metadata in the right order. Use at the end of every owner, reviewer or lead run on an orchestrated project.
license: MIT
compatibility: multica
metadata:
  source: multica orchestrator
---

# Handoff metadata

The orchestrator owns every status and assignee change. You signal with metadata; the signal key goes LAST.

| Role | Write first | Write last (signal) |
|---|---|---|
| owner done | `head_sha=<pushed sha>` | `phase=review_requested` |
| reviewer done | `reviewed_sha=<sha you ran>` | `verdict=approve` or `verdict=changes` |
| anyone stuck | `blocked_reason=<what is missing, who can give it>` | `phase=blocked` |
| lead decision | (description edit, if any) | `phase=unblocked` or `phase=cancel_proposed` |

```sh
multica issue metadata set KEY --key head_sha --value "\"$(git rev-parse HEAD)\""
multica issue metadata set KEY --key phase --value review_requested
```

## Rules

- Quote shas as JSON strings so a numeric-looking sha stays a string.
- `head_sha` must equal the sha on `origin/agent/KEY` (`git rev-parse origin/agent/KEY` after the push).
- Never write `owner`, `attempt_*`, `pr`, `pr_number`, `merged_sha`, `acceptance*`, `review_started_sha`; never `issue status` except `in_progress`; never `--assignee`. The server refuses with `orchestrator_owned` — that is the contract, do not retry or route around it.
- Handoff is metadata-first: write the keys above in order; that is the signal to the orchestrator. A result comment is optional and never substitutes for metadata.
- For gbot / `mac-studio:gbot` experts: never post an issue comment with member/profile credentials (that posts as the human, e.g. member:Ray). If you comment at all, use only an agent/`mat_` token so authorship is agent:gbot. Prefer putting any summary in the task reply next to the `[gbot-task … status=…]` marker instead of an issue comment.
- `blocked_reason` tells you why the issue came back.

- **Workspace scope (hard rule)**: search, read and write only inside the run's working directory (git worktree). Use `rg <pattern> .` or `git grep`; never run recursive grep/find/rg over `$HOME`, `/`, `/Users` or parent directories. Issue context comes from `multica issue get`, comments, metadata (`blocked_reason`, `acceptance`) and `multica flow show`; if it is missing, report blocked instead of searching the disk.
