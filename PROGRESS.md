# feat/orchestrator — PROGRESS

In-app orchestrator (design doc B, teamctl/docs/B-orchestrator-in-multica.md in the e2e worktree).
Reference implementation: /Users/kee/Workspace/ai-stack/multica-e2e/teamctl (kept as-is).

- [x] M0 engine: server/internal/orchestrator/{spec,engine}.go + tests (port of teamctl tick; adds acceptance gate T10, FLOW.md invariants from backup/20261019/workflow-transitions-wip)
- [x] M1 migration 583_orchestrator (team, transition log w/ idem_key, event queue, issue_state for backoff/reminders)
- [x] M1 store/actuator/runtime: bus SubscribeAll → durable event queue → debounced worker; 60s reconciler; backoff + dead-letter; kill switch (team pause, label orchestrator-pause, MULTICA_ORCHESTRATOR_DISABLED=1); in-process API calls as service member "Orchestrator" (orchestrator@system.multica.local); native inbox type orchestrator_alert
- [x] M1 guard: UpdateIssue/BatchUpdate/SetIssueMetadataKey refuse with code orchestrator_owned; PR auto-complete disabled for orchestrated projects (pr_merge_status=none semantics)
- [x] M1 API /api/orchestrator (teams apply/diff/pause/resume/detach, flow show/log/unblock/cancel/accept/reject/replay)
