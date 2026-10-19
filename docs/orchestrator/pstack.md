# pstack in the orchestrator

All 55 pstack skills and 2 agents (MIT, Lauren Tan, cursor/plugins @ 6b78e9f4) are vendored verbatim in
`server/internal/orchestrator/preset/impl-review-merge/pstack/`. `manifest.yaml` maps each one to roles and an action;
`CONTRACT.md` (workspace scope, metadata handoff, no human mid-run, append-only history with merge-main, engine owns landing,
optional subagents) is appended to every exported SKILL.md. `multica team preset render` attaches the mapped skills per role;
`export` writes the adapted files (dropped skills are not exported). `TestPstackManifestCoversEverySkill` fails if an upstream
skill is added without a mapping, or an adapt/drop/operator entry has no reason.

Re-sync: copy upstream `skills/ agents/ LICENSE` over the vendored tree, run the test, map new skills.

Actions: **keep** = verbatim + contract; **adapt** = note overrides conflicting upstream text; **operator** = for humans, never attached; **drop** = not exported.

| skill | action | roles | why / adaptation |
|---|---|---|---|
| architect | keep | lead, owner |  |
| arena | adapt | owner | Candidates live in .orch-scratch/arena/<n> inside the worktree, not /tmp or new worktrees. Only the winner is committed to the issue branch. Without subagents, skip arena and say so. |
| automate-me | drop |  | Mines ~/.cursor transcripts and writes .cursor/skills via AskQuestion: violates workspace scope and no-human-mid-run. Lessons are encoded with `correct` instead. |
| benchmark-checklist | keep | owner, reviewer |  |
| blast-radius | keep | owner, reviewer |  |
| bro | operator |  | Conversational tone skill for a live human. |
| correct | adapt | lead | The 'agent instruction file' is the repo's AGENTS.md/.multica docs on a normal issue branch; propose the change as its own issue, never edit role instructions in place. |
| create-formal-verification-skill | adapt | owner | Only when the issue asks for it. Output goes into the repo (e.g. .multica/skills/verify-*), committed on the issue branch. |
| create-verification-skill | adapt | owner | Write the skill under the repo's .multica/skills/verify-<app>/ (not .cursor/skills) on the issue branch; it becomes the reviewer/acceptance evidence recipe. |
| figure-it-out | keep | lead |  |
| how | adapt | lead, owner, reviewer | Explainer subagents optional (contract 6). |
| in-your-own-words | adapt | lead | Restate the issue as the plan comment before splitting; no live confirmation, unclear premises go to blocked-report. |
| interrogate | adapt | reviewer | This is the multi-reviewer pass behind review-verdict. Reviewer subagents optional; map findings to review-verdict severities and record them in the verdict comment. |
| ketchup | adapt | acceptance | Produce the catch-up as an issue comment for the human accepter, from issue history and activity, not transcripts. |
| maintain-verification-skill | keep | owner, reviewer |  |
| make-bot-ui | drop |  | Grok Bot webhook UI plus Tailscale exposure: unrelated to multica runs and conflicts with the no-exposure rule. |
| no-comments | keep | owner, reviewer |  |
| poteto-help | operator |  | Help menu for a human reading pstack; reads ~/.cursor. |
| poteto-mode | adapt | owner, reviewer, lead | Playbooks babysit, shipping, autopilot-full, autopilot-stack, orchestrate, worktree-cleanup, session-pickup and pause-safely describe the engine's job (contract 5): read them for intent only. 'Rebase into small ordered commits' in feature/refactoring/opening-a-pr = commit small ordered steps (contract 4). Swarm screenshots go in .orch-scratch/, not /tmp. |
| principle-attack-the-premise | keep | lead |  |
| principle-boundary-discipline | keep | owner |  |
| principle-build-the-lever | keep | lead |  |
| principle-encode-lessons-in-structure | keep | lead |  |
| principle-exhaust-the-design-space | keep | lead |  |
| principle-experience-first | keep | owner, acceptance |  |
| principle-explain-the-number | keep | owner, reviewer |  |
| principle-fix-root-causes | keep | owner, reviewer |  |
| principle-foundational-thinking | keep | lead, owner |  |
| principle-guard-the-context-window | keep | lead, owner, reviewer |  |
| principle-laziness-protocol | keep | owner |  |
| principle-make-operations-idempotent | keep | owner |  |
| principle-migrate-callers-then-delete-legacy-apis | keep | lead, owner |  |
| principle-minimize-reader-load | keep | owner, reviewer |  |
| principle-model-the-domain | keep | owner |  |
| principle-never-block-on-the-human | adapt | lead, owner, reviewer | Irreversible or out-of-scope decisions still go to blocked-report (contract 3). |
| principle-outcome-oriented-execution | keep | lead |  |
| principle-prove-it-works | keep | owner, reviewer, acceptance |  |
| principle-redesign-from-first-principles | keep | lead |  |
| principle-separate-before-serializing-shared-state | keep | lead |  |
| principle-sequence-verifiable-units | keep | lead, owner |  |
| principle-subtract-before-you-add | keep | owner, reviewer |  |
| principle-test-behavior-not-implementation | keep | owner, reviewer |  |
| principle-type-system-discipline | keep | owner |  |
| recall | adapt | lead | Recall from issue comments, activity and git log via the multica CLI; never read ~/.cursor transcripts. |
| reflect | adapt | lead | Post-mortem over a finished issue's comments/activity; accepted items become new issues, not edits to rules files. |
| setup-pstack | drop |  | Writes ~/.cursor/rules/pstack-models.mdc (outside workspace). Model choice is the multica agent's runtime config. |
| sharpen | keep | lead |  |
| show-me-your-work | adapt | owner, reviewer | The work log is the handoff comment on the issue. |
| swarm | adapt | reviewer, acceptance | Verification workers optional (contract 6); evidence files in .orch-scratch/, summarized in the verdict comment. |
| tdd | keep | owner |  |
| teach | operator |  | Interactive tutoring for a live human. |
| technical-writing | keep | owner, lead |  |
| typescript-best-practices | keep | owner, reviewer |  |
| unslop | keep | owner, reviewer |  |
| why | adapt | lead, owner, reviewer | Source-digging subagents optional (contract 6); external sources (Datadog, Slack) only if the runtime already has access. |
| comment-sicko (agent) | adapt | reviewer | Use as a reviewer lens (comment hygiene) inside interrogate, not a separate agent. |
| poteto-agent (agent) | operator |  | Cursor subagent definition; multica roles replace it. |

Acceptance-role skills are for the human accepter / acceptance agent (the preset renders lead, owners, reviewer).
