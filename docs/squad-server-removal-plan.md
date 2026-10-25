# Squad server removal — migration plan

Squads are replaced by the **orchestrator** for team routing and assignment. The frontend is shedding squad UI; older servers should **soft-fail** new squad operations while clients keep decoding historical `assignee_type=squad` (and related wire values) for timelines, activity, and cached issue rows.

This document inventories **live server coupling** in the `agent-tui` worktree and proposes a phased server migration. It does **not** implement migrations or delete code.

**Related:** [orchestrator/README.md](./orchestrator/README.md), [SLIM2-PROGRESS.md](../SLIM2-PROGRESS.md) (partial entry-point removal).

---

## Current state (already in this tree)

Partial **Phase 0**:

| Area | Status |
|------|--------|
| HTTP `/api/squads*` + `POST /api/issues/{id}/squad-evaluated` | **Removed** from `server/cmd/server/router.go` (`07ff2a999`) |
| `multica squad` CLI | **Removed** (`server/cmd/multica/cmd_squad.go`) |
| CLI issue `--assignee` / `--to` | **Member + agent only** (`issueAssigneeKinds`, `server/cmd/multica/cmd_issue.go` ~3200–3202) |
| Frontend squad list API | **`squadListOptions` → `[]`** (`packages/core/workspace/queries.ts`) |
| Orchestrator | **No squad refs** (`server/internal/orchestrator/`) |

**Still live:** `squad` / `squad_member` tables, sqlc, CHECK constraints, squad-leader dispatch, daemon briefing, issue API still accepting `assignee_type=squad` when a row exists.

---

## Inventory

### HTTP (removed — historical contract)

From `server/cmd/server/router.go` at `29082f7cf`:

- `GET/POST /api/squads`, `GET/PUT/DELETE /api/squads/{id}`
- `GET/POST/DELETE /api/squads/{id}/members`, `PATCH .../members/role`
- `POST /api/issues/{id}/squad-evaluated` → `RecordSquadLeaderEvaluation`

Handlers were in `server/internal/handler/squad.go` (865 lines removed in `07ff2a999`).

### HTTP / handlers still using squads

| Surface | Files |
|---------|-------|
| Issue assign validation (`assignee_type=squad`) | `server/internal/handler/issue.go` (~4274–4298) |
| Issue list / visibility SQL | `server/internal/handler/issue.go`, `server/pkg/db/queries/issue.sql` (~33–57) |
| Comment @squad triggers | `server/internal/handler/comment.go` |
| Autopilot assignee `agent` \| `squad` | `server/internal/handler/autopilot.go` |
| Quick action assignee catalog | `server/internal/handler/quick_action.go` (~218, 318–342) |
| Daemon claim + leader briefing | `server/internal/handler/daemon.go`, `squad_briefing.go` |
| Project workflow `handler_type: squad` | `server/internal/issueworkflow/issueworkflow.go`, `handler/issue_workflow.go` |
| Workspace delete | `server/internal/handler/workspace.go`, `pkg/db/queries/workspace_delete.sql` |

Internal enqueue: `server/internal/handler/squad.go` (`enqueueSquadLeaderTask`, etc.) — **no route mount**.

### SQL

- **Tables:** `squad`, `squad_member` — `server/schema/sqlite.sql` ~1784–1810; migrations `084`–`088`.
- **Columns:** `issue.assignee_type` (squad), `autopilot.assignee_type`, `autopilot_run.squad_id`, `agent_task_queue.squad_id` + `is_leader_task`, `quick_action.assignee_type`.
- **Activity:** `activity_log.action = squad_leader_evaluated`; index `idx_activity_log_squad_no_action_task` (migration `089`).
- **Queries:** `server/pkg/db/queries/squad.sql` (`TransferSquadAssignees`, `TransferSquadAutopilotsToLeader`, …); wakeup join in `workspace_wakeup.sql` ~155–156.

### Execution (`assignee_type=squad`)

| Path | Files |
|------|-------|
| Assign / promote → leader task | `service/issue.go`, `service/issue_trigger.go`, `handler/squad.go` |
| Autopilot dispatch | `service/autopilot.go` (`resolveAutopilotLeader`, `EnqueueTaskForSquadLeader*`), `service/task.go` |
| Comments / admission | `handler/comment.go`, `handler/agent_access.go` |
| Wakeup conditions | `service/issue_wakeup_condition.go`, `issue_wakeup_system.go` |
| no_action dedup | `service/squad_no_action.go`, `service/task.go` |

### Autopilot `squad_id`

Config on `autopilot`; attribution on `autopilot_run.squad_id` — `service/autopilot.go` (`autopilotSquadAttribution` ~1484–1492).

### Daemon / prompts

- `server/internal/daemon/prompt.go` (~320–339): still references `multica squad list` and squad quick-create defaults.
- `squad_briefing.go`: leader protocol references `multica squad activity`.
- `daemon/execenv/runtime_config_sections.go`, `reply_instructions.go`: squad CLI help strings.

**Gap:** CLI `multica squad` removed; prompts still cite it.

### CLI

- Timeline action `squad_leader_evaluated`: `server/cmd/multica/cmd_issue_timeline.go` (~74).
- `resolveAssignee*` still supports `kinds.squad` when enabled (~3335–3490).
- Stale test: `server/internal/cli/errors_integration_test.go` (~155) calls `/api/squads`.

### Analytics / metrics / WS

- `EventSquadCreated` / `squad_created`: `internal/analytics/events.go`; Prometheus `multica_squad_created_total` in `internal/metrics/business_events.go`.
- Autopilot run props: `assignee_type`, `squad_id` — `events.go` ~362–744.
- Protocol: `squad:created|updated|deleted` — `pkg/protocol/events.go` (~147–149). Emission stopped with HTTP removal; constants remain.

### Listeners

- No subscriber/inbox for squad assignee: `cmd/server/squad_assignee_listeners_test.go`.
- Note: `cmd/server/subscriber_listeners.go` (~16).

---

## Phases

### Phase 0 — Soft-block new squad creates/assigns

**Done:** CRUD routes, `multica squad`, default CLI squad resolution, empty client squad list.

**Next:**

1. Reject issue/autopilot/quick_action **writes** with `assignee_type=squad` (410/422) in `validateAssigneePair` and autopilot save paths.
2. Optionally block new `@squad` dispatches in `comment.go` (keep parse for display).
3. Feature-flag rollout via `server/pkg/featureflag` on hosted fleets.

Keep read paths and historical JSON enums.

### Phase 1 — Stop squad for execution (orchestrator-only)

1. Disable `enqueueSquadLeaderTask` and service squad branches; autopilot `assignee_type=squad` dispatch off.
2. Remove daemon briefing injection (`daemon.go`) and squad copy in `daemon/prompt.go` / execenv.
3. Deprecate workflow `handler_type: squad`; quick_action catalog without squads.
4. Legacy rows may remain until Phase 2; optional read-only squad join for display.

### Phase 2 — Data migration

1. Per-workspace inventory: issues, autopilots, quick_actions, open tasks with `squad_id` / `is_leader_task`.
2. Issues: `TransferSquadAssignees` pattern → leader as `assignee_type=agent` (`squad.sql` ~116–119).
3. Autopilots: `TransferSquadAutopilotsToLeader` (~121–132) or archive.
4. Drain in-flight leader tasks; map squads to orchestrator teams where applicable (`docs/orchestrator/examples/team.yaml`).
5. Soft-archive all `squad` rows before Phase 3 hard drop.

New maintenance migration only — do not edit `084`–`089`, `096`, `127`.

### Phase 3 — Drop schema and code

New migration: drop `squad_member`, `squad`; narrow CHECK constraints; drop `agent_task_queue.squad_id`, `autopilot_run.squad_id` when safe; remove sqlc `squad.sql` and Go squad packages/tests; retire analytics counters (keep historical docs for `squad_leader_evaluated`).

---

## Clients — keep historical decode

| Value | Handling | Types / files |
|-------|----------|---------------|
| `assignee_type: squad` | Show name or **Unknown Squad** | `packages/core/types/issue.ts`, `workspace/hooks.ts` (~83, 164–167) |
| `mention://squad/...` | Label without live API | `issues/comment-trigger-outcomes.ts`, `types/comment.ts` |
| `squad_leader_evaluated` | Timeline | `types/events.ts`, activity payloads |
| Autopilot / quick_action squad fields | Decode only | `types/autopilot.ts`, `types/quick-action.ts` |
| WS `squad:*` | No-op on new servers | `types/events.ts`, `protocol/events.go` |

Keep `"squad"` branches in `buildActorNameResolver` even though `squadListOptions` returns `[]`.

---

## Highest-risk coupling

1. **In-flight leader tasks** + briefing (`agent_task_queue`, `daemon.go`, `squad_briefing.go`).
2. **Autopilot squad assignees** + `TransferSquadAutopilotsToLeader` (`service/autopilot.go`).
3. **Comment @squad coalescing** (shared leader) — `handler/comment.go`.
4. **Issue visibility SQL** squad membership — `pkg/db/queries/issue.sql`.
5. **Mixed-version agents** — prompts cite removed `multica squad` (`daemon/prompt.go`).
6. **CHECK constraint / migration order** — must reassign before dropping `'squad'` from enums.
7. **`squad_leader_evaluated` dedup index** — dropping before migration revives duplicate comments (`squad_no_action.go`, migration `089`).

---

## File index

| Category | Paths |
|----------|-------|
| Schema | `server/schema/sqlite.sql`, `server/migrations/084_squad.*.sql` … `127_task_squad_id.*.sql` |
| sqlc | `server/pkg/db/queries/squad.sql`, `issue.sql`, `workspace_wakeup.sql`, `activity.sql` |
| Handlers / services | `handler/issue.go`, `comment.go`, `autopilot.go`, `daemon.go`, `squad.go`, `squad_briefing.go`; `service/autopilot.go`, `task.go`, `squad_no_action.go` |
| CLI / daemon | `cmd/multica/cmd_issue.go`, `daemon/prompt.go` |
| Client | `packages/core/types/squad.ts`, `workspace/queries.ts`, `workspace/hooks.ts` |
| Progress | `SLIM2-PROGRESS.md` § (e) |
