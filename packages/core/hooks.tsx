"use client";

import { useCurrentWorkspace } from "./paths/hooks";
import { getCurrentWsId } from "./platform/workspace-storage";

/**
 * Returns the current workspace UUID. Throws if none is selected.
 *
 * Web: URL slug + React Query workspace list (`useCurrentWorkspace`).
 * TUI/CLI token mode: `setCurrentWorkspace(null, workspaceId)` + ApiClient
 * `getWorkspaceId` — same id mirror via `getCurrentWsId()` so shared
 * mutation hooks (inbox, issues, …) work without a slug route.
 */
export function useWorkspaceId(): string {
  const ws = useCurrentWorkspace();
  if (ws) return ws.id;
  const id = getCurrentWsId();
  if (id) return id;
  throw new Error("useWorkspaceId: no workspace selected — ensure component renders inside a workspace route (or TUI bootSharedApi)");
}
