# pstack in the orchestrator

All 55 pstack skills and 2 agents (MIT, Lauren Tan, cursor/plugins @ 6b78e9f4) are vendored verbatim in
`server/internal/orchestrator/preset/impl-review-merge/pstack/`. `manifest.yaml` maps each one to roles and an action;
`CONTRACT.md` (workspace scope, metadata handoff, no human mid-run, append-only history with merge-main, engine owns landing,
optional subagents) is appended to every exported SKILL.md. `multica team preset render` attaches the mapped skills per role;
`export` writes the adapted files (dropped skills are not exported). `TestPstackManifestCoversEverySkill` fails if an upstream
skill is added without a mapping, or an adapt/drop/operator entry has no reason.

Re-sync: copy upstream `skills/ agents/ LICENSE` over the vendored tree, run the test, map new skills.

Actions: **keep** = verbatim + contract; **adapt** = note overrides conflicting upstream text; **operator** = for humans, never attached; **drop** = not exported.

## Per-role mounting and budgets (2026-10-19)

Only the `roles:` lists in `manifest.yaml` are mounted; every other keep/adapt skill is exported as an unattached
library (roles column = `library`). Each role has an explicit context budget (bytes of role file + team contract +
every mounted SKILL.md as exported, all loaded) and a max pstack skill count; `TestRoleContextBudget` enforces both.

| role | pstack skills | context bytes / budget | before (skills, ~bytes) |
|---|---|---|---|
| lead | 7 | 31.6k / 32k | 23, ~124k |
| owner | 8 | 45.8k / 46.5k | 33, ~187k |
| reviewer | 5 | 37.1k / 38.5k | 20, ~123k |
| acceptance | 2 | 10.4k / 11k | 4, ~20k |

The reviewer runs a multi-dimension review: correctness, security, tests and API design lenses in parallel; only
high-confidence findings with a severity (`important` / `nit` / `pre-existing`) are reported (reviewer.md, review-verdict).
Screenshot acceptance is a PocketJS-side convention and out of scope here.

| skill | action | mounted for | why / adaptation |
|---|---|---|---|
| architect | keep | library |  |
| arena | adapt | library | Candidates live in .orch-scratch/arena/<n> inside the worktree, not /tmp or new worktrees. Only the winner is committed to the issue branch. Without subagents, skip arena and say so. |
| automate-me | drop |  | Mines ~/.cursor transcripts and writes .cursor/skills via AskQuestion: violates workspace scope and no-human-mid-run. Lessons are encoded with `correct` instead. |
| benchmark-checklist | keep | library |  |
| blast-radius | keep | owner, reviewer |  |
| bro | operator |  | Conversational tone skill for a live human. |
| correct | adapt | library | The 'agent instruction file' is the repo's AGENTS.md/.multica docs on a normal issue branch; propose the change as its own issue, never edit role instructions in place. |
| create-formal-verification-skill | adapt | library | Only when the issue asks for it. Output goes into the repo (e.g. .multica/skills/verify-*), committed on the issue branch. |
| create-verification-skill | adapt | library | Write the skill under the repo's .multica/skills/verify-<app>/ (not .cursor/skills) on the issue branch; it becomes the reviewer/acceptance evidence recipe. |
| figure-it-out | keep | lead |  |
| how | adapt | library | Explainer subagents optional (contract 6). |
| in-your-own-words | adapt | lead | Restate the issue as the plan comment before splitting; no live confirmation, unclear premises go to blocked-report. |
| interrogate | adapt | reviewer | This is the multi-reviewer pass behind review-verdict. Reviewer subagents optional; map findings to review-verdict severities and record them in the verdict comment. |
| ketchup | adapt | library | Produce the catch-up as an issue comment for the human accepter, from issue history and activity, not transcripts. |
| maintain-verification-skill | keep | library |  |
| make-bot-ui | drop |  | Grok Bot webhook UI plus Tailscale exposure: unrelated to multica runs and conflicts with the no-exposure rule. |
| no-comments | keep | reviewer |  |
| poteto-help | operator |  | Help menu for a human reading pstack; reads ~/.cursor. |
| poteto-mode | adapt | library | Playbooks babysit, shipping, autopilot-full, autopilot-stack, orchestrate, worktree-cleanup, session-pickup and pause-safely describe the engine's job (contract 5): read them for intent only. 'Rebase into small ordered commits' in feature/refactoring/opening-a-pr = commit small ordered steps (contract 4). Swarm screenshots go in .orch-scratch/, not /tmp. |
| principle-attack-the-premise | keep | lead |  |
| principle-boundary-discipline | keep | owner |  |
| principle-build-the-lever | keep | library |  |
| principle-encode-lessons-in-structure | keep | library |  |
| principle-exhaust-the-design-space | keep | library |  |
| principle-experience-first | keep | acceptance |  |
| principle-explain-the-number | keep | library |  |
| principle-fix-root-causes | keep | owner |  |
| principle-foundational-thinking | keep | library |  |
| principle-guard-the-context-window | keep | lead |  |
| principle-laziness-protocol | keep | library |  |
| principle-make-operations-idempotent | keep | library |  |
| principle-migrate-callers-then-delete-legacy-apis | keep | library |  |
| principle-minimize-reader-load | keep | library |  |
| principle-model-the-domain | keep | library |  |
| principle-never-block-on-the-human | adapt | library | Irreversible or out-of-scope decisions still go to blocked-report (contract 3). |
| principle-outcome-oriented-execution | keep | lead |  |
| principle-prove-it-works | keep | owner, reviewer, acceptance |  |
| principle-redesign-from-first-principles | keep | library |  |
| principle-separate-before-serializing-shared-state | keep | library |  |
| principle-sequence-verifiable-units | keep | lead |  |
| principle-subtract-before-you-add | keep | owner |  |
| principle-test-behavior-not-implementation | keep | owner, reviewer |  |
| principle-type-system-discipline | keep | library |  |
| recall | adapt | library | Recall from issue comments, activity and git log via the multica CLI; never read ~/.cursor transcripts. |
| reflect | adapt | library | Post-mortem over a finished issue's comments/activity; accepted items become new issues, not edits to rules files. |
| setup-pstack | drop |  | Writes ~/.cursor/rules/pstack-models.mdc (outside workspace). Model choice is the multica agent's runtime config. |
| sharpen | keep | lead |  |
| show-me-your-work | adapt | library | The work log is the handoff comment on the issue. |
| swarm | adapt | library | Verification workers optional (contract 6); evidence files in .orch-scratch/, summarized in the verdict comment. |
| tdd | keep | owner |  |
| teach | operator |  | Interactive tutoring for a live human. |
| technical-writing | keep | library |  |
| typescript-best-practices | keep | owner |  |
| unslop | keep | library |  |
| why | adapt | library | Source-digging subagents optional (contract 6); external sources (Datadog, Slack) only if the runtime already has access. |
| comment-sicko (agent) | adapt | reviewer | Use as a reviewer lens (comment hygiene) inside interrogate, not a separate agent. |
| poteto-agent (agent) | operator |  | Cursor subagent definition; multica roles replace it. |

Acceptance-role skills are for the human accepter / acceptance agent (the preset renders lead, owners, reviewer).
