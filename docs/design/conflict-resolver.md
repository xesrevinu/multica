# Conflict-resolver role (design, not implemented)

## Problem
Today a PR that conflicts with main is handled as ordinary rework:
`precheck conflict` / `mergeable=CONFLICTING` → `T3_rework` back to the owner,
spending `attempt_impl`. Two to three conflicts in a busy repo exhaust the
round cap and block the issue (OX-174: "impl attempts exhausted: precheck
conflict"). The owner then re-reads their whole task to do a mechanical merge.
The PR card's "Merge main into branch" (GitHub update-branch) cannot help: it
only works when there is *no* conflict, and it moves the branch on GitHub
behind the local worktree's back.

## Role
A dedicated agent, `conflict-resolver` (spec: `roles.conflict_resolver`;
empty = feature off, today's behaviour). Narrow, mechanical, cheap model.

## Trigger
Engine sees a conflict for an issue whose branch exists and has a PR/local
branch, in any of:
- T2 pre-check (owner handed off): `ProbeResult.Kind == "conflict"`.
- T10 / merge pre-check: `mergeable == CONFLICTING`.
- Acceptance waiting and main moved into a conflict.
Not triggered while an owner run is active (the owner is still editing) or
when the issue is blocked/cancelled.

## State machine
New phase `resolving_conflict`, new transitions:

| id | from | to | when |
|---|---|---|---|
| `T11_resolve_conflict` | in_review / todo(changes_requested from conflict) | in_review, assignee conflict-resolver, phase resolving_conflict | conflict detected, resolver configured, `attempt_conflict < limit` |
| `T11a_conflict_resolved` | resolving_conflict | back to where it came from | resolver run completed, branch head moved, merge base now contains main tip, no conflict markers, gates green → re-enter the previous step: re-review if the merge touched files in the PR diff beyond trivial context (patch-id changed), else carry the approval (`T3c_carry_verdict`, same patch-id rule as today) |
| `T11b_conflict_handback` | resolving_conflict | todo, assignee owner, phase changes_requested | resolver reports `cannot_resolve` (semantic conflict), or its run failed/ended without a result twice |
| (existing) `T6_block` | resolving_conflict | blocked | `attempt_conflict` exhausted AND owner hand-back already used |

`T11b` hands the owner a structured handoff (kind `conflict`): conflicting
files, both sides' commits touching each, the resolver's notes on what it
tried and why it stopped. The owner run is a normal rework (counts
`attempt_impl`), because a semantic conflict *is* real work.

## Attempts / retries
- New counter `attempt_conflict` (limit 2, spec `limits.conflict`); resolver
  runs never touch `attempt_impl`/`attempt_review`.
- Resets to 0 when the issue merges or the owner pushes new work.
- Provider quota/rate limits: existing `providerFailure` path (backoff, no
  attempt). Prepare/infra failures: `attempt_infra` as today.
- Engine re-probes after every resolver run; if main moved again and a new
  conflict appears, that is a new attempt.

## Where it works
The issue's existing worktree on `agent/<key>` (same reuse rule as reviewer:
issue-scoped branch record, see 2cf0ace4d). No new branch, no rv-/fork.

## What the resolver does (skill `resolve-conflict`)
1. `git fetch origin`; verify HEAD == recorded `head_sha` and the tree is
   clean, else stop (`cannot_resolve: branch moved`).
2. `git merge --no-ff origin/main` (merge, never rebase: squash-only policy
   and pushed history stay intact; no force push).
3. Resolve conflicts preferring: keep both behaviours; take main's version for
   files the PR doesn't intentionally change; never drop the PR's intent.
   Lockfiles/generated files: regenerate, don't hand-merge.
4. Run the project gates (`verify-gates` skill: build, typecheck, tests the
   PR touches).
5. Commit the merge (message lists resolved files), `git push` (fast-forward
   only). Write meta `conflict_resolution` = files + one-line rationale each.
6. If any conflict needs a product decision, or gates fail for reasons not
   caused by the merge, abort the merge (`git merge --abort`), push nothing,
   set `phase=cannot_resolve` with notes → engine runs `T11b`.

Prompt (system brief) essentials: "You resolve merge conflicts only. Do not
refactor, do not fix unrelated failures, do not change behaviour beyond what
both sides already do. When in doubt, stop and hand back with notes."

## UI
- PR card: replace "Update from main" on orchestrated issues with a single
  **"Resolve conflicts"** button (visible when CONFLICTING and the resolver is
  configured) → `POST /api/orchestrator/issues/{key}/resolve-conflict`, which
  only nudges the engine to run `T11` now (same guards). Behind-but-clean PRs
  need no button: the resolver path (or GitHub's update-branch for
  non-orchestrated repos) handles it.
- "Copy labels to PR" stays hidden for orchestrated issues (engine owns labels).
- Timeline: `T11*` humanized ("Resolving merge conflicts", "Conflicts
  resolved", "Handed back to owner: semantic conflict").

## Rollout
1. Engine transitions + counter behind `roles.conflict_resolver` (empty = off).
2. Skill + agent definition in presets; dry-run mode logs "would dispatch
   resolver" for a day.
3. Enable on Holt; watch `attempt_conflict` / hand-back rate.

## Open questions for Ray
- Re-review policy after a resolver merge: always re-review, or carry the
  approval when the PR's own patch-id is unchanged (proposed: carry)?
- Should a resolver run also handle "behind main, no conflict" (keep PRs
  fresh), or only real conflicts (proposed: only conflicts)?
- Model/runtime for the resolver (cheap model is fine for mechanical merges).
