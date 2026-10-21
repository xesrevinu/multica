---
name: multica-platform
description: "Use for Multica platform actions the runtime brief does not fully cover: issue and PR contracts, orchestrated (team-run) projects, mentions, agents, autopilots, projects, runtimes, skill import. Not for the product code you are working on."
user-invocable: false
allowed-tools: Bash(multica *), Bash(git *), Bash(gh *)
---

# Operating Multica

The runtime brief owns the turn: which issue, when to comment, what to write.
This skill owns the platform contracts behind it. Read the rules below, then
open the reference(s) your task actually needs. Do not read them all.

## Routing

| Open | When the task is about |
|---|---|
| `references/orchestrated.md` | An issue in an orchestrated project: hand-over metadata, `orchestrator_owned` refusals, blocked, review verdicts |
| `references/issues.md` | Issues: PR linking, status side effects, sub-issues and stages, sibling runs, wakeups, charts vs files in comments |
| `references/mentions.md` | `mention://` links: which types start a run, which are inert |
| `references/agents.md` | Agent definitions: fields, secrets, MCP config, skills |
| `references/autopilots.md` | Autopilots: triggers, `create_issue` vs `run_only` |
| `references/projects.md` | Projects, resources (`github_repo`, `local_directory`, worktrees), project workflows |
| `references/runtimes.md` | Runtimes, daemons, `repo checkout` |
| `references/skill-import.md` | Importing a skill from a URL or archive |

A task that crosses domains needs each domain it touches: creating an agent,
assigning it an issue and writing a mention needs three references.

## Rules

1. **Orchestrated project? Never write status or assignee.** If the brief or a
   refusal says the project is orchestrated, the orchestrator moves the issue.
   You hand over with metadata (`references/orchestrated.md`). An
   `orchestrator_owned` error is the contract working, not a permission bug:
   do not retry it, do not look for another route.
2. **Read before you write.** `list` / `get` with `--output json` first;
   `multica <command> --help` beats guessing flags.
3. **A name is not an id.** Mentions, assignment and every `--*-id` flag take a
   real UUID from the matching `list --output json`. Never invent one.
4. **`--output json` writes to stdout; warnings go to stderr.** Do not `2>&1`
   into a parser — a write that succeeded will look failed and invite a retry.
5. **Writes are real.** They change shared state or start paid runs. Never run
   one to see what happens; propose what the user did not ask for.
6. **Status keys identify workflow states; categories describe lifecycle only.**
   Custom statuses do not inherit built-in automation behavior.
7. **Comment reads stay bounded.** Scan with `--roots-only --summary --compact`,
   then expand with `--thread <thread-id> --tail 30`. When the turn message
   hands you a `--since` delta read, that read is the bounded scan.

## When behavior looks wrong

Classify first: expected, configuration, product limitation, or bug. Say what
the platform does; if it is correct but bad for the user, say so and propose a
scoped change. Do not quietly change routing, briefing or triggers.
