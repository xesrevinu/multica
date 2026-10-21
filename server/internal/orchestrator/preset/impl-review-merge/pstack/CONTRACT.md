# pstack inside a Multica orchestrated run

Upstream pstack (MIT, Lauren Tan; cursor/plugins @ 6b78e9f4, 2026-10-19) is vendored
verbatim under `skills/` and `agents/`. On export every SKILL.md gets this contract and
its per-skill note (from `manifest.yaml`) appended. Where they disagree with the
upstream text, **this contract wins**.

1. **Workspace scope.** Read and write only inside your run's working directory (the
   issue worktree) and the Multica CLI. Never write `~/.cursor`, `~/.claude`, `/tmp`
   outside your run dir, other worktrees, or the profile home. Scratch goes in
   `.orch-scratch/` inside the worktree (git-ignored, never committed).
2. **Handoff = issue metadata.** State lives in issue metadata and comments, not in
   chat transcripts or local stores. Write `phase` last, per `handoff-metadata`.
   "Decision trail", "resume note", "inbox", "status page" all mean: an issue comment.
3. **No human mid-run.** There is no live operator. Where a skill says AskQuestion /
   "ask the user" / "confirm with the operator", decide yourself if reversible;
   otherwise use `blocked-report` (phase=blocked with a concrete question) and stop.
4. **History is append-only.** Never rebase, amend pushed commits, or force-push.
   Conflicts: `git merge origin/<main>`. "Rebase into small ordered commits" means
   *commit in small ordered steps as you go*. Patch-id carry handles re-review.
5. **The engine owns topology and landing.** No PR merges, base retargets, stacks,
   worktree creation/removal or CI retriggers by agents; the orchestrator does these
   from verdicts and gates. Owners push their own branch only.
6. **Subagents / models are optional.** `Task`, `pstack-models.mdc`, bugbot,
   automations and cloud agents are Cursor features. If your runtime has subagents,
   run the parallel roles inside your run dir; otherwise run them sequentially
   yourself and say so in the handoff comment. Never block on a missing model rule.
