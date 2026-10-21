# SLIM2 progress

Worktree: `chore/slim-2` only. No push. Disk was ~92GB free when this note was written.

## (a) Agent providers — done

Kept: `pi`, `omp`, `codex`, `claude`, `opencode`, `yielded`, `grok`, `gbot`, `dsh`.

- `9c3d54520` `chore: drop unsupported agent providers` — 207 files, +319 / −63566
- `b60b42f9a` `docs: describe only the remaining agent runtimes` — 18 files, +134 / −468

Shared ACP client lives in `server/pkg/agent/acp_client.go` (Grok still uses those types). Historical `runtime_profile` checks were not edited. Classifier comments that still name removed CLIs were left; they are not a second backend. `gbot` has no local-skills path.

## (b) Composio — done

Removed the SDK (`server/pkg/composio`), integration service, HTTP routes, settings Apps tab, agent MCP-apps tab, client methods, feature flag, docs env rows, and landing copy.

Left in place (no migration):

- `user_composio_connection` (`server/migrations/127_user_composio_connection.*.sql`)
- `agent.composio_toolkit_allowlist` (`server/migrations/129_agent_composio_allowlist_and_task_originator.*.sql`)
- sqlc queries that still select that column (`server/pkg/db/queries/agent.sql` and generated `agent.sql.go` / `runtime.sql.go`)

The HTTP API no longer reads or writes the allowlist. `buildRuntimeMCPOverlay` stays as a no-op so enqueue call sites compile. Generic MCP fixtures that use the server name `composio` were left.

`pnpm typecheck` passed. `go build ./...` and `go vet ./...` passed. `go test` passed for `./internal/daemon`, `./internal/daemon/execenv`, `./internal/orchestrator`, and handler config/overlay tests. Broader `./internal/handler`, `./internal/service`, and `./cmd/server` tests fail on this machine because the local database is behind the code (`icon`, `triage_state`, `comment_thread_id`, `created_by_id`, `issue_wakeup`). Those failures are schema drift, not Composio.

## (c) WeCom / DingTalk — not started

Do not edit existing migrations. If dropping tables needs a new migration, skip it and note it here.

## (d) Stale refs — not started

Leave `LICENSE` untouched. Checked 2026-10-21: references to Composio / WeCom / DingTalk / desktop / mobile / plugin SDK remain in ~70 files (channel engine, daemon execenv, handler, generated sqlc, landing i18n, examples). Not small; left for a separate pass after (c).

## (e) Squads — done (herdr/remove-squads)

`server/internal/orchestrator` does not depend on squads.

Removed entry points: `/api/squads*` and `/api/issues/{id}/squad-evaluated` routes and handlers, `multica squad`, squad assignee resolution in the CLI (`--assignee` / `--to` now match members and agents only), squads pages and sidebar / shortcut / search entries, create-squad modal, squad client methods, the `?squad=` join on agent create, the squads docs pages, tutorial sections and CLI rows, and the multica-platform `references/squads.md`.

Kept (no migration): squad tables, sqlc queries, issue `assignee_type = 'squad'`, and the internal squad-leader dispatch in issue/comment/autopilot services. With no way to create or list squads it is unreachable for new data; existing squad-assigned issues still dispatch to their leader. The squad directory query resolves empty, so pickers show no squads and existing squad assignees render as "Unknown Squad".

## Skipped migrations

No new migration was added. Historical `runtime_profile` checks, Composio tables/columns, and WeCom/DingTalk/squad tables stay in place.
