import { ApiClient, setApiInstance } from "@multica/core/api"
// Deep import — barrel `@multica/core/platform` pulls CoreProvider/i18n JSX
// which breaks under TUI's `@opentui/react` jsxImportSource.
import { setCurrentWorkspace } from "@multica/core/platform/workspace-storage"
import type { Connection } from "../config"

/** Mutable so TUI workspace switches update ApiClient + mutation hooks. */
let activeWorkspaceId: string | null = null

/**
 * Boot the shared Web ApiClient for the TUI.
 *
 * Same HTTP surface as Web (`@multica/core/api`), token + workspace-id mode
 * (CLI layout). Orch / inbox queryOptions + mutations in core then work without
 * a second Effect transport for those routes.
 *
 * `setCurrentWorkspace(null, id)` feeds `useWorkspaceId()` for mutation hooks
 * that still read workspace from core (no URL slug in the TUI).
 */
export function bootSharedApi(connection: Connection): ApiClient {
  activeWorkspaceId = connection.workspaceID
  const client = new ApiClient(connection.serverUrl, {
    identity: { platform: "multica-tui" },
    getToken: () => connection.token,
    getWorkspaceId: () => activeWorkspaceId,
  })
  client.setToken(connection.token)
  setCurrentWorkspace(null, connection.workspaceID)
  setApiInstance(client)
  return client
}

/** Keep shared ApiClient + `useWorkspaceId()` in sync when the TUI switches workspaces. */
export function setSharedWorkspaceId(workspaceId: string) {
  activeWorkspaceId = workspaceId
  setCurrentWorkspace(null, workspaceId)
}
