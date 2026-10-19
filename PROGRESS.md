# feat/orchestrator — PROGRESS

In-app orchestrator (design doc B, teamctl/docs/B-orchestrator-in-multica.md in the e2e worktree).
Reference implementation: /Users/kee/Workspace/ai-stack/multica-e2e/teamctl (kept as-is).

- [x] M0 engine: server/internal/orchestrator/{spec,engine}.go + tests (port of teamctl tick; adds acceptance gate T10, FLOW.md invariants from backup/20261019/workflow-transitions-wip)
- [x] M1 migration 583_orchestrator (team, transition log w/ idem_key, event queue, issue_state for backoff/reminders)
- [x] M1 store/actuator/runtime: bus SubscribeAll → durable event queue → debounced worker; 60s reconciler; backoff + dead-letter; kill switch (team pause, label orchestrator-pause, MULTICA_ORCHESTRATOR_DISABLED=1); in-process API calls as service member "Orchestrator" (orchestrator@system.multica.local); native inbox type orchestrator_alert
- [x] M1 guard: UpdateIssue/BatchUpdate/SetIssueMetadataKey refuse with code orchestrator_owned; PR auto-complete disabled for orchestrated projects (pr_merge_status=none semantics)
- [x] M1 API /api/orchestrator (teams apply/diff/pause/resume/detach, flow show/log/unblock/cancel/accept/reject/replay)

## Scenarios run on the in-app engine, teamctl serve stopped (2026-10-19)
- S1 ATT-37 pass; PR-S1 ATT-38 pass (PR #9); S2 ATT-39 pass; PR-S2 ATT-41 pass (changes→rework→merged, PR #10)
- GA ATT-40 pass: T10→reject(reason)→T3 rework→T5b stuck cancel→re-review→T10→`flow accept`→done
- OOB ATT-43 pass: T9 reverted in ~430ms; F1 ATT-45 pass; F2 ATT-46 pass (dead-letter); S5 ATT-47 pass (disk wait, then done)
- S6+S3 ATT-48..51 pass: stage-2 child started only after both stage-1 children were done; ATT-50 conflict precheck → T3 rework → merged
- S4 pass (on its own): ATT-40 run >20m → T5b_cancel_stuck → T5 rerun
- Incident: the ATT-40 owner ran `rg` over /Users/kee looking for the rejection reason, which was only in metadata.acceptance. Killed only that rg (a daemon→pi child), paused the team, cancelled the run. Fix: reject also writes blocked_reason; WORKSPACE SCOPE rule added to instructions, skills, the platform reference and the runbook. The rerun was clean.
- Not done yet: PR-S3..S6, M4 UI, patch-id verdict carry-over, leader election, rebuilding the e2e daemon/CLI from orch

## Step: idle detection, M4 UI, e2e daemon rebuild, PR-S3/S4/S6 (2026-10-19 16:00–16:30)
- 54ea8f512 no-progress detection: `limits.idle_min` (default 8, negative disables). A running run with no task_message for that long gets T5b (counts as an infra attempt).
- 0336312ae web: issue Flow card (state rail with current node, phase, attempt counters, failures, why blocked, paused, waiting, next decision, last 10 transitions, Accept button) and project team section (spec summary plus pause/resume). views typecheck clean; vitest projects+issues 1443/1443 pass.
- e2e daemon rebuilt from feat/orchestrator: the old binary is kept as multica-e2e/server/bin/multica.bak-pre-orch, and the daemon was restarted with the same env/profile (pid 57230).
- PR-S3 ATT-52/53: pass. PR #11 merged first; #12 GitHub mergeable=CONFLICTING → T3 rework → re-review → merged (de563cc).
- PR-S6 ATT-54..57: pass. Stage-1 PRs merged in parallel, stage 2 started (16:10:48) only after both were done, then merged.
- PR-S4 ATT-58: pass, on the new daemon. With idle_min=1 the agent's `sleep 150` was cancelled 3 times ("no tool output for 1m"), then T6 block to lead after infra 2/2, orchestrator_alert inbox. idle_min restored to 8, `flow unblock` → the 150s sleep was tolerated → PR merged.
- PR-S5 ATT-59: in progress (intentional first-attempt CI flake → expect exactly one re-run).
- PR-S5 ATT-59: pass. The first CI attempt was red (intentional flake); the engine re-ran it once (run 39223273946 attempt 2 = success; the agents' task_messages have no rerun) → merged PR #17. The re-run is now logged (commit "log CI re-runs"). Note: the "one re-run per head" memory is in-process, so a server restart could allow one more.
- Scenario suite on the in-app engine: S1–S6, PR-S1–S6, GA, OOB, F1, F2 all pass.

## Remaining
- Multi-replica: no leader election yet (single-replica assumption, documented in design.md).
- patch-id verdict carry-over (rebase-only reworks still re-review).
- UI: transitions are listed in the Flow card, not interleaved in the comment timeline; reject from the UI is not implemented (CLI needs --reason); the frontend has no special rendering for the orchestrator_alert inbox type.
- The CI re-run memory should be persisted (orchestrator_issue_state).
- PocketJS .multica changes deferred, as instructed.
