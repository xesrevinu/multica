# @multica/tui

Standalone Multica workspace TUI. Originally developed inside OpenCode as
`@opencode-ai/multica-tui`; now lives in this monorepo as an independent app
powered by `@opentui/core` + `@opentui/react` (no `@opencode/*` runtime deps).

## Run

```bash
# from repo root
pnpm --filter @multica/tui start
# or
pnpm start:tui

# watch mode
pnpm --filter @multica/tui dev
```

Connection settings come from the Multica CLI config (`~/.multica/config.json`),
or overrides:

```bash
MULTICA_SERVER_URL=… MULTICA_WORKSPACE_ID=… MULTICA_TOKEN=… pnpm start:tui
```

Useful flags:

```bash
bun run apps/tui/src/bin.ts --help
bun run apps/tui/src/bin.ts --json overview
bun run apps/tui/src/bin.ts --pick-issue --output /tmp/issue.json
```

## Layout

Three chrome rows, one list, one hint line — nothing stacks:

```
multica · ws <name> · proj <project>        <- context (p / ctrl+w)
1:overview 2:inbox 3:issues 4:space         <- tabs (1-4, h/l)
f <active filters>                          <- filters (f / click a chip)
  <list or detail>                          <- the rest of the screen
<time> ● live  ● n working      f · / · ?   <- hints live here, at the bottom
```

- `f` opens the filter menu (attention, status, assignee scope, machine …);
  clicking a chip cycles it. Nothing else is printed, so the list keeps the
  screen.
- Surfaces without filters show a count instead (`33 skills`), so switching
  space modes never shifts the list.
- `/` search, `r` refresh and the surface actions reuse that same row.
- `?` opens a two-column key map that fits without scrolling.

## Stack

- OpenTUI React renderer (`createCliRenderer` + `createRoot`)
- Thin local replacements for the OpenCode TUI shell (theme / dialog / keymap /
  config / toast) under `src/tui/context` and `src/tui/ui`
- Bun for start / test / binary compile



## Shared transport & state (with Web)

Both clients hit the same Multica API and present the same product surfaces.
The long-term shape is **one transport + one server-state layer**:

1. **HTTP**: `@multica/core/api` `ApiClient` (TUI boots it in `src/api/shared.ts`
   with bearer token + `X-Workspace-ID`; Web uses cookie/slug mode).
2. **Server state**: TanStack React Query — core exports `*Options` /
   `use*Mutation` (already true for orchestrator). TUI mounts
   `QueryClientProvider` in `tui/run.tsx`.
3. **Domain**: zod parsers / flow rules stay in `@multica/core/orchestrator/*`
   (and later other domains). UI chrome stays per surface (OpenTUI vs DOM).

**Migrated slices (shared ApiClient + React Query):**
- **Orch flow panel** — `orchTimelineOptions` / `orchPRCardOptions` /
  `useOrchFlowAction` / `useOrchPROp`
- **Inbox** — `inboxListOptions` / `archivedInboxListOptions` + mark/archive
  mutations via `tui/use-inbox.ts` (`useSharedInbox`)

Remaining TUI screens still use the Effect OpenAPI client + local `Store`;
migrate surface-by-surface. Workspace switches call `setSharedWorkspaceId` so
`X-Workspace-ID` and `useWorkspaceId()` stay aligned.

**Import constraint:** TUI `tsconfig` sets `jsxImportSource` to `@opentui/react`.
Do **not** import the `@multica/core/platform` barrel (it pulls Web
`CoreProvider` / i18n JSX). Prefer deep exports such as
`@multica/core/platform/workspace-storage`, `@multica/core/inbox/queries`, and
`@multica/core/inbox/mutations`.

## Shared domain with Web

Business logic that is not UI-specific lives in `@multica/core` and is imported
by both apps:

| Concern | Package export |
|---|---|
| Orch zod schemas | `@multica/core/orchestrator/schemas` |
| Lenient parsers | `@multica/core/orchestrator/parse` |
| Attention tree / filters | `@multica/core/orchestrator/tree` |
| Flow card mode + actions | `@multica/core/orchestrator/flow` |
| PR op availability | `@multica/core/orchestrator/flow` (`availablePROps`) |

The TUI transport is the Effect OpenAPI client (`bun run generate` from
`openapi.yaml`). After HTTP decode, orch payloads still pass through the shared
zod parsers so Web and TUI cannot drift on attention/flow semantics.

## Verification (tuistory)

`script/e2e.ts` drives the real TUI inside a PTY with
[tuistory](https://npm.im/tuistory) and asserts the layout contract above:

```bash
npm i -g tuistory
pnpm --filter @multica/tui test:e2e   # add --keep to hijack the session
```

It checks the 3-row chrome on every surface, the filter menu + chips, search in
the same row, detail panes scrolling inside the list area, hints on the last
row, and the help dialog fitting on screen. Screenshots land in `.e2e/`.
Needs a live server (`~/.multica/config.json` or `MULTICA_*` env); it exits
cleanly when none is configured. The run uses profile `e2e`, so your real
`tui-state.json` is untouched.

## Navigation

Flat top tabs (no you/work/ops nesting):

1. **overview** — workspace pulse
2. **inbox** — notifications
3. **issues** — single task list (absorbs the old orch attention buckets via
   `o` / attention chips). Enter opens issue detail (= chat / timeline / flow)
4. **space** — projects (progress list only), autopilots, agents, skills,
   agent DMs (`chats`), usage, settings

Projects no longer open a detail pane: Enter jumps to **issues** filtered to
that project.

## Orchestrator

Attention buckets (`needs_you` / `running` / `queued` / `settled`) are filters
on the **issues** tab, backed by `/api/orchestrator/issues`. Issue detail still
shows the flow card (accept / reject / unblock / cancel / replay) when the
issue is orchestrated.

`listWorkingAgents` soft-fails on 404 so older servers still boot the TUI.
