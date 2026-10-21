---
name: multica-platform
description: "Use for Multica platform actions the runtime brief does not cover: orchestrated (team-run) projects, issue and PR contracts, wakeups, mentions, agents, autopilots, projects, runtimes, skill import. Not for the product code you are working on."
user-invocable: false
allowed-tools: Bash(multica *), Bash(git *), Bash(gh *)
---

# Operating Multica

The runtime brief owns the turn. `multica <command> --help` documents every
flag; this skill keeps only what `--help` will not tell you. Open a reference
only when the task needs it.

| Open | When |
|---|---|
| `references/orchestrated.md` | The issue's project has a team (`multica team status`): hand-over, review, experts, blocked |
| `references/issues.md` | Issues outside a team: PR linking, status side effects, sub-issues and stages, wakeups, charts |
| `references/workspace.md` | Mentions, agents, autopilots, projects, runtimes, skill import |

## Rules

1. **Orchestrated project: never write status, assignee or engine metadata.**
   The orchestrator assigns, routes review, merges (squash) and closes. You
   hand over with metadata. `orchestrator_owned` is the contract, not a
   permission bug: do not retry it or look for another route.
2. **A name is not an id.** Every `--*-id` flag and mention takes a UUID from
   `list --output json`. Never invent one.
3. **Parse stdout only.** `--output json` writes stdout, warnings go to stderr;
   `2>&1` into a parser makes a successful write look failed.
4. **Writes are real.** They change shared state or start paid runs. Read
   (`get`/`list`) before you write; never write to see what happens.
5. **Bounded comment reads.** `multica issue comment list <id> --roots-only
   --summary --compact`, then `--thread <id> --tail 30`.
6. **Stay in your checkout.** Context comes from `multica issue get`, comments,
   metadata and `multica flow show <key>`, not from searching the disk.

When behavior looks wrong, say whether it is expected, configuration, a
product limit or a bug, and propose a scoped change. Do not quietly change
routing, triggers or team specs.
