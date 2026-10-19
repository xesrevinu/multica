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
